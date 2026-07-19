package mcpserver

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wallfacers/engram/memory"
)

// getForTest acquires ns and immediately releases the pin, returning the handle.
// It models a discrete access and is safe in single-threaded tests where no
// concurrent eviction runs; tests that need to hold a pin call Acquire directly.
func getForTest(t *testing.T, registry *Registry, ctx context.Context, ns string) *NamespaceHandle {
	t.Helper()
	handle, release, err := registry.Acquire(ctx, ns)
	if err != nil {
		t.Fatal(err)
	}
	release()
	return handle
}

func TestRegistryLazilyOpensAndReusesNamespace(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()

	if got := registry.handleCount(); got != 0 {
		t.Fatalf("new registry has %d handles, want 0", got)
	}

	first := getForTest(t, registry, ctx, "default")
	if first.entries == nil || first.retriever == nil {
		t.Fatalf("lazily opened handle is incomplete: entries=%v retriever=%v", first.entries, first.retriever)
	}
	if got := registry.handleCount(); got != 1 {
		t.Fatalf("after first Get has %d handles, want 1", got)
	}

	second := getForTest(t, registry, ctx, "")
	if first != second {
		t.Fatal("repeated default namespace Get did not reuse the handle")
	}
	if got := registry.handleCount(); got != 1 {
		t.Fatalf("after repeated Get has %d handles, want 1", got)
	}
}

func TestRegistryCloseClosesAllHandles(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	getForTest(t, registry, ctx, "default")
	getForTest(t, registry, ctx, "other")

	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	if got := registry.handleCount(); got != 0 {
		t.Fatalf("closed registry has %d handles, want 0", got)
	}
}

func TestRegistryEvictsLeastRecentlyUsedNamespaceAndPreservesData(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir(), MaxOpenNamespaces: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	first := getForTest(t, registry, ctx, "first")
	if err := first.entries.Upsert(ctx, &memory.Entry{Name: "first-memory", Content: "persisted first namespace", CharCount: 26}); err != nil {
		t.Fatal(err)
	}
	getForTest(t, registry, ctx, "second")
	second := getForTest(t, registry, ctx, "second")
	if err := second.entries.Upsert(ctx, &memory.Entry{Name: "second-memory", Content: "persisted second namespace", CharCount: 27}); err != nil {
		t.Fatal(err)
	}
	getForTest(t, registry, ctx, "first")
	getForTest(t, registry, ctx, "third")
	if got := registry.handleCount(); got != 2 {
		t.Fatalf("registry has %d open handles after eviction, want 2", got)
	}
	if _, ok := registry.handles["second"]; ok {
		t.Fatal("least recently used second namespace was not evicted")
	}

	reopened := getForTest(t, registry, ctx, "second")
	entries, err := reopened.entries.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "second-memory" {
		t.Fatalf("evicted second namespace lost persisted data: %#v", entries)
	}
	firstAgain := getForTest(t, registry, ctx, "first")
	entries, err = firstAgain.entries.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "first-memory" {
		t.Fatalf("evicted first namespace lost persisted data: %#v", entries)
	}
}

// TestRegistryDoesNotEvictInUseHandle proves that a handle pinned by an
// in-flight Acquire is never closed by LRU eviction, even under heavy pressure
// from concurrent Acquire calls on other namespaces. Run under -race, an old
// evictLocked that unconditionally closed the LRU tail would trip here.
func TestRegistryDoesNotEvictInUseHandle(t *testing.T) {
	ctx := context.Background()
	registry, err := NewRegistry(ctx, RegistryConfig{DataDir: t.TempDir(), MaxOpenNamespaces: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })

	held, release, err := registry.Acquire(ctx, "held")
	if err != nil {
		t.Fatal(err)
	}
	if err := held.entries.Upsert(ctx, &memory.Entry{Name: "keep", Content: "held-namespace data", CharCount: 19}); err != nil {
		t.Fatal(err)
	}

	var readErr atomic.Value
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// If eviction closed the pinned store underneath us, this List
			// errors (and races on the closed *sql.DB under -race).
			if _, err := held.entries.List(ctx); err != nil {
				readErr.Store(err)
				return
			}
		}
	}()

	// With MaxOpenNamespaces=1, every distinct namespace is over budget and the
	// LRU tail includes the pinned "held"; a correct registry evicts only idle
	// handles and tolerates transient over-budget instead.
	for i := 0; i < 200; i++ {
		other, releaseOther, err := registry.Acquire(ctx, fmt.Sprintf("ns-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := other.entries.List(ctx); err != nil {
			t.Fatal(err)
		}
		releaseOther()
	}

	close(stop)
	wg.Wait()
	if v := readErr.Load(); v != nil {
		t.Fatalf("pinned handle was evicted/closed while in use: %v", v)
	}

	// Releasing the pin lets the bound be enforced again on the next open.
	release()
	getForTest(t, registry, ctx, "reclaim")
	if got := registry.handleCount(); got > 1 {
		t.Fatalf("after releasing pin, handleCount = %d, want <= 1 (bound)", got)
	}
}
