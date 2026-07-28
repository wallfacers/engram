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
	"github.com/wallfacers/engram/memory/curation"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/store"
)

// RegistryConfig supplies dependencies shared by every namespace handle.
type RegistryConfig struct {
	DataDir           string
	EmbClient         embedding.Client
	LLMCaller         pipeline.ModelCaller
	MaxOpenNamespaces int
	CurationEnabled   bool
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
	curator   *curation.Worker

	curatorCancel context.CancelFunc

	// refs counts in-flight Acquire holders. Guarded by Registry.mu. A handle
	// with refs > 0 is in use and MUST NOT be evicted/closed underneath its
	// callers; eviction skips it and tolerates a transient over-budget state.
	refs int
}

func (h *NamespaceHandle) close() error {
	if h == nil {
		return nil
	}
	if h.curatorCancel != nil {
		h.curatorCancel()
	}
	if h.curator != nil {
		h.curator.Wait()
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
	curationEnabled   bool
	ctx               context.Context
	cancel            context.CancelFunc

	mu        sync.Mutex
	handles   map[string]*NamespaceHandle
	closing   map[string]chan struct{}
	lru       *list.List // front is most recently used; values are namespace strings
	closed    bool
	closeDone chan struct{}
	closeErr  error
}

type detachedHandle struct {
	namespace string
	handle    *NamespaceHandle
	done      chan struct{}
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
	if config.CurationEnabled && config.LLMCaller == nil {
		return nil, errors.New("curation requires an LLM caller")
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
	registryCtx, cancel := context.WithCancel(ctx)
	return &Registry{
		dataDir:           dataDir,
		embClient:         config.EmbClient,
		llmCaller:         config.LLMCaller,
		maxOpenNamespaces: config.MaxOpenNamespaces,
		curationEnabled:   config.CurationEnabled,
		ctx:               registryCtx,
		cancel:            cancel,
		handles:           make(map[string]*NamespaceHandle),
		closing:           make(map[string]chan struct{}),
		lru:               list.New(),
		closeDone:         make(chan struct{}),
	}, nil
}

// Acquire returns the cached handle for namespace, opening and assembling it on
// first access, and pins it for the duration of the caller's use. The returned
// release function MUST be called (typically via defer) exactly when the caller
// is done touching the handle; it drops the pin so the handle may later be
// evicted. While pinned, the handle's store is never closed underneath the
// caller, which is what makes concurrent tool calls safe against LRU eviction.
func (r *Registry) Acquire(ctx context.Context, namespace string) (*NamespaceHandle, func(), error) {
	if r == nil {
		return nil, nil, errors.New("nil registry")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ns, err := normalizeNamespace(namespace)
	if err != nil {
		return nil, nil, err
	}
	path, err := namespaceDatabasePath(r.dataDir, ns)
	if err != nil {
		return nil, nil, err
	}

	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return nil, nil, errors.New("registry is closed")
		}
		if done := r.closing[ns]; done != nil {
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-done:
				continue
			}
		}
		if handle := r.handles[ns]; handle != nil {
			r.touchLocked(ns)
			handle.refs++
			release := r.releaseFunc(handle)
			r.mu.Unlock()
			return handle, release, nil
		}
		break
	}

	st, err := store.Open(ctx, store.Options{DSN: path})
	if err != nil {
		r.mu.Unlock()
		return nil, nil, fmt.Errorf("open namespace %q: %w", ns, err)
	}
	entries := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	embedder := memory.NewEmbedder(entries, vectors, r.embClient, memory.DefaultEmbedBuffer)
	retriever := memory.NewRetriever(entries, vectors, r.embClient)
	var curator *curation.Worker
	var curatorCtx context.Context
	var curatorCancel context.CancelFunc
	var onWrite func()
	if r.curationEnabled {
		var cancel context.CancelFunc
		curatorCtx, cancel = context.WithCancel(r.ctx)
		curatorCancel = cancel
		curator = curation.NewWorker(
			entries,
			st.DB(),
			curation.ModelCaller(r.llmCaller),
			curation.DefaultConfig(),
			nil,
		)
		onWrite = curator.Notify
	}
	pipe := pipeline.New(pipeline.Config{
		Entries:  entries,
		Embedder: embedder,
		Call:     r.llmCaller,
		Budgets:  memory.DefaultBudgets(),
		OnWrite:  onWrite,
	})
	handle := &NamespaceHandle{
		store:         st,
		entries:       entries,
		vectors:       vectors,
		embedder:      embedder,
		retriever:     retriever,
		pipe:          pipe,
		curator:       curator,
		curatorCancel: curatorCancel,
		refs:          1,
	}
	if curator != nil {
		curator.Start(curatorCtx)
	}
	r.handles[ns] = handle
	r.lru.PushFront(ns)
	victims := r.detachEvictionsLocked()
	release := r.releaseFunc(handle)
	r.mu.Unlock()
	r.closeDetached(victims)
	return handle, release, nil
}

// releaseFunc builds the idempotent pin-release closure for handle. It is safe
// to call more than once (subsequent calls are no-ops) and after the registry
// is closed.
func (r *Registry) releaseFunc(handle *NamespaceHandle) func() {
	released := false
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if released {
			return
		}
		released = true
		if handle.refs > 0 {
			handle.refs--
		}
	}
}

func (r *Registry) touchLocked(namespace string) {
	for element := r.lru.Front(); element != nil; element = element.Next() {
		if element.Value == namespace {
			r.lru.MoveToFront(element)
			return
		}
	}
}

// detachEvictionsLocked removes idle LRU victims from the live cache and marks
// their namespaces closing. Slow worker/embedder shutdown happens after mu is
// released, so unrelated namespaces continue serving. An Acquire for the same
// namespace waits on the closing marker and cannot create a duplicate worker.
func (r *Registry) detachEvictionsLocked() []detachedHandle {
	var victims []detachedHandle
	for len(r.handles) > r.maxOpenNamespaces {
		victim := r.oldestIdleLocked()
		if victim == nil {
			return victims // all over-budget handles are in use; tolerate soft overflow
		}
		namespace := victim.Value.(string)
		handle := r.handles[namespace]
		delete(r.handles, namespace)
		r.lru.Remove(victim)
		done := make(chan struct{})
		r.closing[namespace] = done
		victims = append(victims, detachedHandle{namespace: namespace, handle: handle, done: done})
	}
	return victims
}

func (r *Registry) closeDetached(victims []detachedHandle) {
	for _, victim := range victims {
		_ = victim.handle.close()
		r.mu.Lock()
		if r.closing[victim.namespace] == victim.done {
			delete(r.closing, victim.namespace)
			close(victim.done)
		}
		r.mu.Unlock()
	}
}

// oldestIdleLocked returns the least-recently-used LRU element whose handle has
// no active references, or nil if every open handle is currently pinned. Callers
// must hold mu.
func (r *Registry) oldestIdleLocked() *list.Element {
	for element := r.lru.Back(); element != nil; element = element.Prev() {
		if handle := r.handles[element.Value.(string)]; handle != nil && handle.refs == 0 {
			return element
		}
	}
	return nil
}

// Close closes every opened namespace and prevents future Get calls.
func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		done := r.closeDone
		r.mu.Unlock()
		<-done
		r.mu.Lock()
		err := r.closeErr
		r.mu.Unlock()
		return err
	}
	r.closed = true
	if r.cancel != nil {
		r.cancel()
	}
	handles := make(map[string]*NamespaceHandle, len(r.handles))
	for namespace, handle := range r.handles {
		handles[namespace] = handle
		delete(r.handles, namespace)
	}
	closing := make([]chan struct{}, 0, len(r.closing))
	for _, done := range r.closing {
		closing = append(closing, done)
	}
	r.lru.Init()
	r.mu.Unlock()

	var closeErr error
	for namespace, handle := range handles {
		if err := handle.close(); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("close namespace %q: %w", namespace, err)
		}
	}
	for _, done := range closing {
		<-done
	}
	r.mu.Lock()
	r.closeErr = closeErr
	close(r.closeDone)
	r.mu.Unlock()
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
