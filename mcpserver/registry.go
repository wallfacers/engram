package mcpserver

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/store"
)

// RegistryConfig supplies dependencies shared by every namespace handle.
type RegistryConfig struct {
	DataDir           string
	EmbClient         embedding.Client
	LLMCaller         pipeline.ModelCaller
	MaxOpenNamespaces int
}

// NamespaceHandle owns one independent engine store and its assembled public
// engine accessors.
type NamespaceHandle struct {
	store     *store.Store
	entries   *memory.EntryStore
	vectors   *memory.VectorStore
	embedder  *memory.Embedder
	retriever *memory.Retriever
	pipe      *pipeline.Pipeline
}

func (h *NamespaceHandle) close() error {
	if h == nil {
		return nil
	}
	if h.embedder != nil {
		h.embedder.Close()
	}
	if h.store == nil {
		return nil
	}
	return h.store.Close()
}

// Registry lazily maps validated namespaces to isolated engine stores.
type Registry struct {
	dataDir           string
	embClient         embedding.Client
	llmCaller         pipeline.ModelCaller
	maxOpenNamespaces int

	mu      sync.Mutex
	handles map[string]*NamespaceHandle
	lru     *list.List // front is most recently used; values are namespace strings
	closed  bool
}

// NewRegistry creates a registry and validates that its data directory can be
// created and used. Handles themselves are opened only on first Get.
func NewRegistry(ctx context.Context, config RegistryConfig) (*Registry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.DataDir == "" {
		return nil, errors.New("data directory is required")
	}
	if config.MaxOpenNamespaces < 0 {
		return nil, errors.New("max open namespaces must not be negative")
	}
	if config.MaxOpenNamespaces == 0 {
		config.MaxOpenNamespaces = defaultMaxOpenNamespaces
	}
	dataDir, err := filepath.Abs(filepath.Clean(config.DataDir))
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	info, err := os.Stat(dataDir)
	if err != nil {
		return nil, fmt.Errorf("stat data directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("data directory %q is not a directory", dataDir)
	}
	if _, err := namespaceDatabasePath(dataDir, defaultNamespace); err != nil {
		return nil, err
	}
	return &Registry{
		dataDir:           dataDir,
		embClient:         config.EmbClient,
		llmCaller:         config.LLMCaller,
		maxOpenNamespaces: config.MaxOpenNamespaces,
		handles:           make(map[string]*NamespaceHandle),
		lru:               list.New(),
	}, nil
}

// Get returns the cached handle for namespace, opening and assembling it on
// first access.
func (r *Registry) Get(ctx context.Context, namespace string) (*NamespaceHandle, error) {
	if r == nil {
		return nil, errors.New("nil registry")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ns, err := normalizeNamespace(namespace)
	if err != nil {
		return nil, err
	}
	path, err := namespaceDatabasePath(r.dataDir, ns)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("registry is closed")
	}
	if handle := r.handles[ns]; handle != nil {
		r.touchLocked(ns)
		return handle, nil
	}

	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		return nil, fmt.Errorf("open namespace %q: %w", ns, err)
	}
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	embedder := memory.NewEmbedder(entries, vectors, r.embClient, memory.DefaultEmbedBuffer)
	retriever := memory.NewRetriever(entries, vectors, r.embClient)
	pipe := pipeline.New(pipeline.Config{
		Entries:  entries,
		Embedder: embedder,
		Call:     r.llmCaller,
		Budgets:  memory.DefaultBudgets(),
	})
	handle := &NamespaceHandle{
		store:     st,
		entries:   entries,
		vectors:   vectors,
		embedder:  embedder,
		retriever: retriever,
		pipe:      pipe,
	}
	r.handles[ns] = handle
	r.lru.PushFront(ns)
	r.evictLocked()
	return handle, nil
}

func (r *Registry) touchLocked(namespace string) {
	for element := r.lru.Front(); element != nil; element = element.Next() {
		if element.Value == namespace {
			r.lru.MoveToFront(element)
			return
		}
	}
}

// evictLocked keeps the number of opened namespace stores within the configured
// bound. It runs while mu is held, so a namespace cannot be evicted twice or
// replaced concurrently. Store.Close is intentionally performed before the
// next Get can observe the new cache state.
func (r *Registry) evictLocked() {
	for len(r.handles) > r.maxOpenNamespaces {
		element := r.lru.Back()
		if element == nil {
			return
		}
		namespace := element.Value.(string)
		handle := r.handles[namespace]
		delete(r.handles, namespace)
		r.lru.Remove(element)
		_ = handle.close()
	}
}

// Close closes every opened namespace and prevents future Get calls.
func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var closeErr error
	for namespace, handle := range r.handles {
		if err := handle.close(); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("close namespace %q: %w", namespace, err)
		}
		delete(r.handles, namespace)
	}
	r.lru.Init()
	return closeErr
}

func (r *Registry) handleCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.handles)
}

func (r *Registry) hasLLMCaller() bool {
	return r != nil && r.llmCaller != nil
}
