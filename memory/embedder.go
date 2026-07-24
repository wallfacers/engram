package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wallfacers/engram/embedding"
)

// DefaultEmbedBuffer bounds the write-behind embedder's queue.
const DefaultEmbedBuffer = 256

const aliasShadowSuffix = "#alias"
const queryShadowSuffix = "#query"

func aliasShadowName(factName string) string {
	return factName + aliasShadowSuffix
}

// AliasShadowName returns the vector-row name reserved for a fact's aliases.
func AliasShadowName(factName string) string {
	return aliasShadowName(factName)
}

func queryShadowName(factName string) string {
	return factName + queryShadowSuffix
}

// QueryShadowName returns the vector-row name reserved for a fact's doc2query
// pseudo-queries (feature 012).
func QueryShadowName(factName string) string {
	return queryShadowName(factName)
}

// resolveAliasShadow reports whether name is a #alias shadow and its source fact.
func resolveAliasShadow(name string) (source string, isShadow bool) {
	if !strings.HasSuffix(name, aliasShadowSuffix) {
		return name, false
	}
	return strings.TrimSuffix(name, aliasShadowSuffix), true
}

// resolveQueryShadow reports whether name is a #query shadow and its source fact.
func resolveQueryShadow(name string) (source string, isShadow bool) {
	if !strings.HasSuffix(name, queryShadowSuffix) {
		return name, false
	}
	return strings.TrimSuffix(name, queryShadowSuffix), true
}

// resolveShadow reports whether name is any shadow vector (alias or query) and
// its source fact. The retriever's max-pool merge is content-agnostic and folds
// every recognized shadow back onto its source, so adding a new shadow kind only
// requires teaching this resolver its suffix.
func resolveShadow(name string) (source string, isShadow bool) {
	if source, isShadow := resolveQueryShadow(name); isShadow {
		return source, true
	}
	return resolveAliasShadow(name)
}

// Embedder is the write-behind path that keeps memory_embeddings populated
// without ever blocking an entry write (design D3 + D8 usage-logger pattern). A
// single background goroutine drains a queue of entry names, fetches each
// entry's content, embeds it, and upserts the vector. Enqueue is non-blocking;
// a full queue drops the request (the startup/backfill sweep will catch it).
//
// A nil *Embedder is inert: Enqueue/Backfill/Close are all no-ops, which is the
// state when embedding is unconfigured.
type Embedder struct {
	entries *EntryStore
	vectors *VectorStore
	client  embedding.Client
	ch      chan string

	wg        sync.WaitGroup
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
}

// NewEmbedder starts the drain goroutine. When client is nil it returns nil (an
// inert embedder), so callers can unconditionally use the result.
func NewEmbedder(entries *EntryStore, vectors *VectorStore, client embedding.Client, buf int) *Embedder {
	if client == nil || entries == nil || vectors == nil {
		return nil
	}
	if buf <= 0 {
		buf = DefaultEmbedBuffer
	}
	e := &Embedder{
		entries: entries,
		vectors: vectors,
		client:  client,
		ch:      make(chan string, buf),
	}
	e.wg.Add(1)
	go e.drain()
	return e
}

func (e *Embedder) drain() {
	defer e.wg.Done()
	for name := range e.ch {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		if err := e.embedOne(ctx, name); err != nil {
			slog.Warn("memory: write-behind embed failed", "name", name, "err", err)
		}
		cancel()
	}
}

// embedOne fetches, embeds, and stores the vector for one entry. A deleted entry
// (ErrNotFound) is a silent skip.
func (e *Embedder) embedOne(ctx context.Context, name string) error {
	if source, isShadow := resolveQueryShadow(name); isShadow {
		if _, err := e.entries.GetByName(ctx, source); err != nil {
			return nil //nolint:nilerr // source gone before we embedded its queries
		}
		queries, err := e.entries.FactQueries(ctx, source)
		if err != nil {
			return err
		}
		text := queryEmbedText(queries)
		if text == "" {
			return nil
		}
		vecs, err := e.client.Embed(ctx, []string{text})
		if err != nil {
			return err
		}
		if len(vecs) != 1 {
			return nil
		}
		return e.vectors.Put(ctx, name, e.client.Model(), vecs[0], time.Now())
	}
	if source, isShadow := resolveAliasShadow(name); isShadow {
		entry, err := e.entries.GetByName(ctx, source)
		if err != nil {
			return nil //nolint:nilerr // source gone before we embedded its aliases
		}
		aliases, err := e.aliases(ctx, source)
		if err != nil {
			return err
		}
		text := aliasEmbedText(entry.Content, aliases)
		if text == "" {
			return nil
		}
		vecs, err := e.client.Embed(ctx, []string{text})
		if err != nil {
			return err
		}
		if len(vecs) != 1 {
			return nil
		}
		return e.vectors.Put(ctx, name, e.client.Model(), vecs[0], time.Now())
	}

	entry, err := e.entries.GetByName(ctx, name)
	if err != nil {
		return nil //nolint:nilerr // entry gone before we embedded it: nothing to do
	}
	vecs, err := e.client.Embed(ctx, []string{embedText(entry)})
	if err != nil {
		return err
	}
	if len(vecs) != 1 {
		return nil
	}
	return e.vectors.Put(ctx, name, e.client.Model(), vecs[0], time.Now())
}

func (e *Embedder) aliases(ctx context.Context, entryName string) ([]string, error) {
	rows, err := e.entries.db.QueryContext(ctx,
		`SELECT alias FROM memory_event_aliases WHERE entry_name = ? ORDER BY alias`, entryName)
	if err != nil {
		return nil, fmt.Errorf("memory: aliases for embedding %q: %w", entryName, err)
	}
	defer rows.Close() //nolint:errcheck

	var aliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, fmt.Errorf("memory: scan alias for embedding %q: %w", entryName, err)
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read aliases for embedding %q: %w", entryName, err)
	}
	return aliases, nil
}

func aliasEmbedText(content string, aliases []string) string {
	lowerContent := strings.ToLower(content)
	filtered := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if !strings.Contains(lowerContent, strings.ToLower(alias)) {
			filtered = append(filtered, alias)
		}
	}
	return strings.Join(filtered, "\n")
}

// queryEmbedText is the text embedded for a fact's #query shadow: the pseudo-
// queries joined verbatim. Unlike aliasEmbedText it does NOT drop content-word
// overlap — a doc2query question is meant to share the fact's entity words; that
// overlap is the query↔statement bridge we want, not noise to filter.
func queryEmbedText(queries []string) string {
	filtered := make([]string, 0, len(queries))
	for _, q := range queries {
		if strings.TrimSpace(q) != "" {
			filtered = append(filtered, q)
		}
	}
	return strings.Join(filtered, "\n")
}

// embedText is the text handed to the embedding model for an entry. Trigger +
// content captures both the recall cue and the fact body.
func embedText(e *Entry) string {
	if e.Trigger == "" {
		return e.Content
	}
	return e.Trigger + "\n" + e.Content
}

// Enqueue schedules an entry for (re-)embedding. Non-blocking; a full queue
// drops the request. A nil embedder no-ops.
func (e *Embedder) Enqueue(name string) {
	if e == nil || name == "" {
		return
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return
	}
	select {
	case e.ch <- name:
	default:
		slog.Debug("memory: embed enqueue dropped", "name", name)
	}
}

// Backfill enqueues every entry whose vector is missing or stale for the current
// model. Intended to run once at startup and opportunistically after writes.
// Bounded by the queue: names beyond the queue capacity are dropped and picked
// up on the next Backfill. A nil embedder no-ops.
func (e *Embedder) Backfill(ctx context.Context) error {
	if e == nil {
		return nil
	}
	names, err := e.vectors.NamesMissingModel(ctx, e.client.Model())
	if err != nil {
		return err
	}
	aliasShadows, err := e.AliasShadowNames(ctx)
	if err != nil {
		return err
	}
	queryShadows, err := e.QueryShadowNames(ctx)
	if err != nil {
		return err
	}
	shadowNames := append(aliasShadows, queryShadows...)
	if len(shadowNames) > 0 {
		stored, err := e.vectors.LoadAllForModel(ctx, e.client.Model())
		if err != nil {
			return err
		}
		for _, shadowName := range shadowNames {
			if _, ok := stored[shadowName]; !ok {
				names = append(names, shadowName)
			}
		}
	}
	for _, name := range names {
		e.Enqueue(name)
	}
	if len(names) > 0 {
		slog.Info("memory: embedding backfill enqueued", "count", len(names), "model", e.client.Model())
	}
	return nil
}

// AliasShadowNames returns every alias-shadow vector row implied by the alias
// index, in deterministic source-name order.
func (e *Embedder) AliasShadowNames(ctx context.Context) ([]string, error) {
	if e == nil {
		return nil, nil
	}
	rows, err := e.entries.db.QueryContext(ctx,
		`SELECT DISTINCT entry_name FROM memory_event_aliases ORDER BY entry_name`)
	if err != nil {
		return nil, fmt.Errorf("memory: list alias shadow names: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var names []string
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, fmt.Errorf("memory: scan alias shadow name: %w", err)
		}
		names = append(names, aliasShadowName(source))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: read alias shadow names: %w", err)
	}
	return names, nil
}

// QueryShadowNames returns every #query shadow vector row implied by the
// doc2query pseudo-query index (feature 012), in deterministic source-name order.
func (e *Embedder) QueryShadowNames(ctx context.Context) ([]string, error) {
	if e == nil {
		return nil, nil
	}
	sources, err := e.entries.FactQueryEntryNames(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, queryShadowName(source))
	}
	return names, nil
}

// Close stops the drain goroutine after the queue is drained. Safe to call
// multiple times and on a nil embedder.
func (e *Embedder) Close() {
	if e == nil {
		return
	}
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		close(e.ch)
		e.mu.Unlock()
	})
	e.wg.Wait()
}
