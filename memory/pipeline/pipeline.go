// Package pipeline implements the ADD-only fact extraction path
// (memory-hybrid-retrieval-locomo). It distills a batch of conversation
// messages into new memory entries using exactly one LLM call per batch: every
// extracted fact becomes a NEW entry (it never updates or deletes an existing
// one), entities are indexed for the entity retrieval signal, and event dates
// are stamped for time-aware retrieval. Redundancy from accumulation is left to
// the existing curation engine.
//
// The pipeline is fail-safe: a model/parse error, or an individual invalid fact,
// is a WARN and a skip — never a session-affecting error.
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/wallfacers/engram/internal/idgen"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/prompt"
	"github.com/wallfacers/engram/store"
)

// ModelCaller performs one text-in/text-out model call (same shape as the
// curation judge caller). The runtime wires a real provider; tests inject a mock.
type ModelCaller func(ctx context.Context, system, user string) (string, error)

// Message is one conversation turn fed to the pipeline.
type Message struct {
	Role             string // "user" or "assistant"
	Text             string
	ExternalSourceID string
	Ordinal          int
	OccurredAt       *time.Time
}

// RedundancySuppressor decides whether an incoming extracted fact projection is
// a semantic duplicate of an existing fact and should be suppressed before a
// new projection is created (024 write-time redundancy suppression, US1). A
// nil suppressor disables suppression (the default; behavior is byte-identical
// to the pre-024 path). The engine provides an offline default implementation
// (memory/curation.Suppressor); adapters may inject their own.
type RedundancySuppressor interface {
	// ShouldSuppress reports whether incoming is redundant with existing and
	// its projection should be suppressed. It MUST be conservative: returning
	// true suppresses a projection, so false is the safe default on doubt.
	ShouldSuppress(ctx context.Context, existing, incoming *memory.Entry) bool
}

// SuppressionStats are the audit counters for write-time redundancy
// suppression (spec FR-005 / SC-001). They are incremented by the pipeline
// whenever a suppressor is configured.
type SuppressionStats struct {
	// Decisions is the number of incoming facts that were compared against
	// at least one existing candidate (i.e. entered the suppression path).
	Decisions int
	// Suppressed is the number of projections suppressed as redundant.
	Suppressed int
	// SuspectedMisSuppressions is the number of suppressed candidates that
	// also carry independent evidence beyond the matching existing entry — a
	// runtime proxy for "similar but not equivalent" false suppressions
	// (spec FR-005 / research.md Decision 4).
	SuspectedMisSuppressions int
}

// Pipeline extracts and stores facts. A nil call makes it inert (Ingest is a
// no-op), mirroring the curation worker's inert mode.
type Pipeline struct {
	entries    *memory.EntryStore
	ledger     *memory.LedgerStore
	embedder   *memory.Embedder // may be nil (embedding disabled)
	call       ModelCaller
	budgets    memory.Budgets
	onWrite    func() // curation pressure trigger; optional
	suppressor RedundancySuppressor
	stats      SuppressionStats
}

// Config bundles the pipeline's dependencies.
type Config struct {
	Entries  *memory.EntryStore
	Ledger   *memory.LedgerStore
	Embedder *memory.Embedder
	Call     ModelCaller
	Budgets  memory.Budgets
	OnWrite  func()
	// Suppressor, when non-nil, enables write-time redundancy suppression
	// (024 US1). It is the injection point for the engine's offline Jaccard
	// suppressor (memory/curation.Suppressor) or an adapter-provided one.
	// Default nil = suppression disabled, byte-identical legacy behavior.
	Suppressor RedundancySuppressor
}

// New builds a Pipeline. Returns nil when Entries or Call is nil (inert).
func New(cfg Config) *Pipeline {
	if cfg.Entries == nil || cfg.Call == nil {
		return nil
	}
	if cfg.Ledger == nil {
		cfg.Ledger = cfg.Entries.Ledger()
	}
	return &Pipeline{
		entries:    cfg.Entries,
		ledger:     cfg.Ledger,
		embedder:   cfg.Embedder,
		call:       cfg.Call,
		budgets:    cfg.Budgets,
		onWrite:    cfg.OnWrite,
		suppressor: cfg.Suppressor,
	}
}

type extractedFact struct {
	Fact       string   `json:"fact"`
	SourceIDs  []string `json:"source_ids"`
	Entities   []string `json:"entities"`
	EventDate  string   `json:"event_date"`
	EventStart string   `json:"event_start"`
	EventEnd   string   `json:"event_end"`
	Aliases    []string `json:"aliases"`
	Category   string   `json:"category"`
	Durability string   `json:"durability"`
}

type extractionResult struct {
	Facts []extractedFact `json:"facts"`
}

// IngestResult makes the two persistence phases observable: Evidence is
// committed before extraction, while Entries contains only newly created fact
// projections. Duplicate facts can still gain source lineage without being
// counted as new Entries.
type IngestResult struct {
	Evidence []memory.Evidence
	Entries  []*memory.Entry
	Degraded []string
	// Suppression reports the write-time redundancy audit counts for this
	// ingest when a suppressor is configured (spec FR-005 / SC-001). A zero
	// value means suppression was disabled or made no decision.
	Suppression SuppressionStats
}

type preparedMessage struct {
	message Message
	ordinal int
}

// Ingest runs one extraction pass over messages dated sessionDate. It returns the
// number of entries written. A nil pipeline, a trivial batch, or any failure
// yields (0, nil) — the caller never needs to handle extraction errors.
func (p *Pipeline) Ingest(ctx context.Context, sessionDate time.Time, sourceSessionID string, messages []Message) (int, error) {
	result, err := p.IngestDetailed(ctx, sessionDate, sourceSessionID, messages)
	return len(result.Entries), err
}

// SuppressionStats returns the cumulative write-time redundancy audit counters
// since this pipeline was constructed (spec FR-005). A nil pipeline returns a
// zero value. The counts are not concurrency-safe against concurrent Ingest
// calls; callers driving a single pipeline from one goroutine (the MCP worker
// and the eval harness both do) read them safely at the end of a pass.
func (p *Pipeline) SuppressionStats() SuppressionStats {
	if p == nil {
		return SuppressionStats{}
	}
	return p.stats
}

// IngestDetailed commits every substantive raw message as Evidence before one
// extraction call. An extraction model/parse failure is an honest degraded
// result: raw Evidence remains available even when no fact projection is made.
func (p *Pipeline) IngestDetailed(ctx context.Context, sessionDate time.Time, sourceSessionID string, messages []Message) (IngestResult, error) {
	if p == nil {
		return IngestResult{}, nil
	}
	if p.ledger == nil {
		return IngestResult{}, fmt.Errorf("memory: pipeline has no evidence ledger")
	}

	prepared := make([]preparedMessage, 0, len(messages))
	for index, m := range messages {
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		ordinal := m.Ordinal
		if ordinal == 0 && index > 0 {
			ordinal = index
		}
		prepared = append(prepared, preparedMessage{message: m, ordinal: ordinal})
	}
	if len(prepared) == 0 {
		return IngestResult{}, nil // trivial batch: no Evidence or LLM call
	}
	if sourceSessionID == "" {
		sourceSessionID = legacyIngestSessionID(prepared)
	}

	recordedAt := time.Now().UTC()
	inputs := make([]memory.EvidenceInput, 0, len(prepared))
	for _, item := range prepared {
		inputs = append(inputs, memory.EvidenceInput{
			ExternalSourceID: item.message.ExternalSourceID,
			SourceType:       memory.EvidenceMessage,
			SourceSessionID:  sourceSessionID,
			Speaker:          item.message.Role,
			Ordinal:          item.ordinal,
			Content:          item.message.Text,
			OccurredAt:       item.message.OccurredAt,
			RecordedAt:       recordedAt,
		})
	}
	evidence, err := p.ledger.AppendBatch(ctx, inputs)
	if err != nil {
		return IngestResult{}, fmt.Errorf("memory: persist ingest evidence: %w", err)
	}
	result := IngestResult{Evidence: evidence}

	promptMsgs := make([]prompt.MemoryExtractionMessage, 0, len(prepared))
	batchSources := make(map[string]memory.Evidence, len(evidence))
	for index, item := range prepared {
		source := evidence[index]
		batchSources[source.ID] = source
		promptMsgs = append(promptMsgs, prompt.MemoryExtractionMessage{
			Role:     item.message.Role,
			Text:     item.message.Text,
			SourceID: source.ID,
		})
	}
	dateStr := ""
	if !sessionDate.IsZero() {
		dateStr = sessionDate.UTC().Format("2006-01-02")
	}
	user := prompt.BuildMemoryExtractionUserPrompt(dateStr, promptMsgs)

	raw, err := p.call(ctx, prompt.MemoryExtractionSystemPrompt, user)
	if err != nil {
		slog.Warn("memory: extraction model call failed", "err", err)
		result.Degraded = append(result.Degraded, "extraction_model_failure")
		return result, nil
	}
	facts, err := parseFacts(raw)
	if err != nil {
		slog.Warn("memory: extraction parse failed", "err", err)
		result.Degraded = append(result.Degraded, "extraction_parse_failure")
		return result, nil
	}

	for _, f := range facts.Facts {
		refs, ok := factSourceRefs(f.SourceIDs, batchSources)
		if !ok {
			slog.Warn("memory: extracted fact rejected", "reason", "invalid_source_ids")
			continue
		}
		entry, created := p.storeFact(ctx, sessionDate, sourceSessionID, f, refs)
		if created {
			result.Entries = append(result.Entries, entry)
		}
	}
	if len(result.Entries) > 0 && p.onWrite != nil {
		p.onWrite() // one curation pressure signal per batch
	}
	result.Suppression = p.stats
	return result, nil
}

func legacyIngestSessionID(messages []preparedMessage) string {
	var payload strings.Builder
	for _, item := range messages {
		fmt.Fprintf(&payload, "%d\x00%s\x00%s\n", item.ordinal, item.message.Role, item.message.Text)
	}
	digest := sha256.Sum256([]byte(payload.String()))
	return fmt.Sprintf("legacy-ingest:%x", digest[:])
}

// storeFact validates and persists one extracted fact. It returns the entry and
// whether this call created a new fact projection rather than only unioning
// source lineage onto an existing ADD-only fact.
func (p *Pipeline) storeFact(ctx context.Context, sessionDate time.Time, sourceSessionID string, f extractedFact, refs []memory.EvidenceRef) (*memory.Entry, bool) {
	content := strings.TrimSpace(f.Fact)
	if content == "" {
		return nil, false
	}
	if err := p.budgets.CheckEntryContent(content); err != nil {
		slog.Warn("memory: extracted fact rejected", "reason", "content_too_large", "err", err)
		return nil, false
	}
	existing, err := p.entries.GetByContent(ctx, content)
	if err == nil {
		existingRefs, err := p.entries.SourceRefs(ctx, existing.ID)
		if err != nil {
			slog.Warn("memory: duplicate fact source lookup failed", "err", err)
			return nil, false
		}
		if err := p.entries.UpsertWithSources(ctx, existing, unionEvidenceRefs(existingRefs, refs)); err != nil {
			slog.Warn("memory: duplicate fact source union failed", "err", err)
			return nil, false
		}
		return existing, false
	}
	if !errors.Is(err, store.ErrNotFound) {
		slog.Warn("memory: extracted fact dedup check failed", "err", err)
		return nil, false
	}
	// 024 write-time redundancy suppression: before creating a new projection,
	// check whether the incoming fact is a semantic duplicate of an existing
	// one (US1). The FTS candidate pre-filter bounds the exact Jaccard step to
	// a small candidate set (spec FR-001 / research.md Decision 1).
	if p.suppressor != nil {
		incoming := &memory.Entry{
			Name:      entryName(content),
			Content:   content,
			EventDate: parseEventDate(f.EventDate, sessionDate),
		}
		candidates, candErr := p.entries.SimilarEntries(ctx, content, 8)
		if candErr != nil {
			slog.Warn("memory: suppression candidate lookup failed", "err", candErr)
		}
		for _, candidate := range candidates {
			// No self-exclusion guard is needed here: candidate names are unique
			// ULID-suffixed slugs (entryName) that never equal incoming.Name, and
			// a byte-identical fact is already handled by the GetByContent exact
			// match above — the suppression path only ever sees near-duplicates.
			p.stats.Decisions++
			if p.suppressor.ShouldSuppress(ctx, candidate, incoming) {
				p.stats.Suppressed++
				// Suspected mis-suppression: the suppressed candidate carries its
				// own independent evidence lineage (beyond the exact-duplicate
				// union path), so "similar but not equivalent" may have been lost
				// (spec FR-005 / research.md Decision 4).
				if refs, err := p.entries.SourceRefs(ctx, candidate.ID); err == nil && len(refs) > 1 {
					p.stats.SuspectedMisSuppressions++
				}
				slog.Debug("memory: extracted fact suppressed as redundant", "incoming", content, "existing", candidate.Content)
				return nil, false
			}
		}
	}
	trigger := deriveTrigger(content, p.budgets.TriggerChars)
	if err := p.budgets.CheckTrigger(trigger); err != nil {
		trigger = "" // a bad derived trigger is non-fatal; store without one
	}

	entry := &memory.Entry{
		Name:            entryName(content),
		Trigger:         trigger,
		Content:         content,
		Durability:      normalizeDurability(f.Durability),
		Category:        strings.TrimSpace(f.Category),
		CharCount:       memory.CharCount(content),
		SourceSessionID: sourceSessionID,
		FactSource:      "extraction",
		EventDate:       parseEventDate(f.EventDate, sessionDate),
	}
	entry.EventStart, entry.EventEnd = parseEventRange(f.EventStart, f.EventEnd, f.EventDate, sessionDate)
	if err := p.entries.UpsertWithSources(ctx, entry, refs); err != nil {
		slog.Warn("memory: extracted fact upsert failed", "name", entry.Name, "err", err)
		return nil, false
	}
	if len(f.Entities) > 0 {
		if err := p.entries.PutEntities(ctx, entry.Name, f.Entities); err != nil {
			slog.Warn("memory: extracted entities index failed", "name", entry.Name, "err", err)
		}
		pairs := entityPairs(f.Entities)
		if err := p.entries.UpsertEdges(ctx, pairs); err != nil {
			slog.Warn("memory: extracted entity edges failed", "name", entry.Name, "err", err)
		}
	}
	if len(f.Aliases) > 0 {
		if err := p.entries.PutAliases(ctx, entry.Name, f.Aliases); err != nil {
			slog.Warn("memory: extracted event aliases index failed", "name", entry.Name, "err", err)
		}
	}
	p.embedder.Enqueue(entry.Name) // nil-safe
	for _, alias := range f.Aliases {
		if strings.TrimSpace(alias) != "" {
			p.embedder.Enqueue(memory.AliasShadowName(entry.Name)) // nil-safe
			break
		}
	}
	return entry, true
}

func factSourceRefs(sourceIDs []string, batch map[string]memory.Evidence) ([]memory.EvidenceRef, bool) {
	if len(sourceIDs) == 0 {
		return nil, false
	}
	refs := make([]memory.EvidenceRef, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			return nil, false
		}
		if _, exists := batch[sourceID]; !exists {
			return nil, false
		}
		if _, exists := seen[sourceID]; exists {
			continue
		}
		seen[sourceID] = struct{}{}
		refs = append(refs, memory.EvidenceRef{EvidenceID: sourceID, SourceOrder: len(refs), FullSource: true})
	}
	return refs, len(refs) > 0
}

func unionEvidenceRefs(existing, incoming []memory.EvidenceRef) []memory.EvidenceRef {
	combined := make([]memory.EvidenceRef, 0, len(existing)+len(incoming))
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, refs := range [][]memory.EvidenceRef{existing, incoming} {
		for _, ref := range refs {
			if _, exists := seen[ref.EvidenceID]; exists {
				continue
			}
			seen[ref.EvidenceID] = struct{}{}
			ref.SourceOrder = len(combined)
			combined = append(combined, ref)
		}
	}
	return combined
}

func entityPairs(entities []string) []memory.EntityEdge {
	seen := make(map[string]struct{}, len(entities))
	for _, raw := range entities {
		if norm := memory.EntityNorm(raw); norm != "" {
			seen[norm] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(seen))
	for entity := range seen {
		ordered = append(ordered, entity)
	}
	sort.Strings(ordered)
	pairs := make([]memory.EntityEdge, 0, len(ordered)*(len(ordered)-1)/2)
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			pairs = append(pairs, memory.EntityEdge{A: ordered[i], B: ordered[j], Kind: "co", Weight: 1})
		}
	}
	return pairs
}

// hasSubstance reports whether the batch has any non-empty user/assistant text.
func hasSubstance(messages []Message) bool {
	for _, m := range messages {
		if strings.TrimSpace(m.Text) != "" {
			return true
		}
	}
	return false
}

func parseFacts(raw string) (*extractionResult, error) {
	js := extractJSON(raw)
	if js == "" {
		return nil, fmt.Errorf("no JSON object in extraction output")
	}
	var r extractionResult
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		return nil, fmt.Errorf("unmarshal extraction JSON: %w", err)
	}
	return &r, nil
}

func extractJSON(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < 0 || end < start {
		return ""
	}
	return s[start : end+1]
}

func normalizeDurability(d string) string {
	switch strings.TrimSpace(strings.ToLower(d)) {
	case "evergreen":
		return "evergreen"
	default:
		return "volatile"
	}
}

// parseEventDate resolves an ISO date string; on failure returns nil. sessionDate
// is reserved for future relative-date resolution (the model already resolves
// relatives against the session date it is given).
func parseEventDate(s string, _ time.Time) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func parseEventRange(startText, endText, dateText string, sessionDate time.Time) (*time.Time, *time.Time) {
	start := parseEventDate(startText, sessionDate)
	end := parseEventDate(endText, sessionDate)
	if start == nil && end == nil {
		point := parseEventDate(dateText, sessionDate)
		if point != nil {
			return point, point
		}
		return nil, nil
	}
	if start == nil {
		start = end
	}
	if end == nil {
		end = start
	}
	if start != nil && end != nil && end.Before(*start) {
		return nil, nil
	}
	return start, end
}

// deriveTrigger produces a short recall cue from the fact by truncating to the
// trigger budget on a rune boundary.
func deriveTrigger(fact string, maxRunes int) string {
	fact = oneLine(fact)
	if maxRunes <= 0 {
		maxRunes = 120
	}
	r := []rune(fact)
	if len(r) <= maxRunes {
		return fact
	}
	return strings.TrimSpace(string(r[:maxRunes-1]))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// entryName builds a unique slug: a truncated kebab-case slug of the fact plus a
// ULID suffix, so ADD-only never collides on an existing name.
func entryName(fact string) string {
	slug := slugify(fact, 40)
	suffix := strings.ToLower(idgen.NewULID())
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	if slug == "" {
		return "fact-" + suffix
	}
	return slug + "-" + suffix
}

// slugify lowercases, keeps ASCII alphanumerics and CJK runes, and joins runs
// with single hyphens, capped at maxRunes.
func slugify(s string, maxRunes int) string {
	var b strings.Builder
	prevHyphen := false
	count := 0
	for _, r := range strings.ToLower(s) {
		if count >= maxRunes {
			break
		}
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || isCJKRune(r):
			b.WriteRune(r)
			prevHyphen = false
			count++
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
				count++
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3040 && r <= 0x30FF) || // Hiragana + Katakana
		(r >= 0xAC00 && r <= 0xD7A3) // Hangul syllables
}
