package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/memory"
)

// Signature locks: Search / SearchMulti / SearchWithDiagnostics must keep their
// exact signatures (parity is a hard gate; 051-jev-relevance-filter adds
// SearchFiltered alongside them, it does not reshape them).
var (
	_ func(*memory.Retriever, context.Context, string, int) ([]memory.Result, error)                                                                = (*memory.Retriever).Search
	_ func(*memory.Retriever, context.Context, []string, int) ([]memory.Result, error)                                                              = (*memory.Retriever).SearchMulti
	_ func(*memory.Retriever, context.Context, string, int) ([]memory.Result, memory.SearchDiagnostics, error)                                      = (*memory.Retriever).SearchWithDiagnostics
	_ func(*memory.Retriever, context.Context, string, int, int, filter.RelevanceFilter, filter.Policy) ([]memory.Result, filter.FilterMeta, error) = (*memory.Retriever).SearchFiltered
)

// stubFilter is an offline RelevanceFilter: probabilities are returned verbatim
// (the contract requires index alignment) unless fail is set, which makes the
// filter fail the way a Jev shard failure does.
type stubFilter struct {
	probs   []float64
	fail    error
	backend string
	calls   int
	seen    []filter.Candidate
}

func (s *stubFilter) Filter(_ context.Context, _ string, cands []filter.Candidate) ([]float64, filter.FilterMeta, error) {
	s.calls++
	s.seen = append([]filter.Candidate(nil), cands...)
	meta := filter.FilterMeta{Backend: s.backend, Theta: filter.DefaultPolicy().Theta}
	if s.fail != nil {
		meta.Degraded = true
		return nil, meta, s.fail
	}
	return s.probs, meta, nil
}

// seedFilteredCorpus writes n entries that all match the query token "alpha",
// each with a distinct trigger and content, so keyword retrieval returns the
// whole corpus and the fused order is deterministic across runs.
func seedFilteredCorpus(t *testing.T, n int) (*memory.EntryStore, *memory.VectorStore) {
	t.Helper()
	ctx := context.Background()
	es, vs := newStores(t)
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("alpha note %02d %s", i, strings.Repeat("x", i+1))
		entry := &memory.Entry{
			Name:      fmt.Sprintf("sf-%02d", i),
			Trigger:   fmt.Sprintf("trig-%02d", i),
			Content:   content,
			CharCount: len([]rune(content)),
		}
		if err := es.Upsert(ctx, entry); err != nil {
			t.Fatalf("upsert sf-%02d: %v", i, err)
		}
	}
	return es, vs
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

func names(results []memory.Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Name
	}
	return out
}

// T08(b): the degraded path must show exactly what a plain Search(ctx, query,
// show) shows, so the degraded arm stays comparable with arm C@8 (SC-005).
func TestSearchFiltered_DegradedSetMatchesPlainSearch(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 12)
	r := memory.NewRetriever(es, vs, nil)

	plain, err := r.Search(ctx, "alpha", 4)
	if err != nil {
		t.Fatalf("plain search: %v", err)
	}
	if len(plain) != 4 {
		t.Fatalf("plain search returned %d results, want 4", len(plain))
	}

	stub := &stubFilter{backend: filter.BackendJev, fail: errors.New("shard failure")}
	got, meta, err := r.SearchFiltered(ctx, "alpha", 12, 4, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("filter failure must not propagate; got error %v", err)
	}
	if !meta.Degraded {
		t.Fatalf("meta.Degraded = false, want true on the degraded path")
	}
	if meta.Backend != filter.BackendJev {
		t.Fatalf("meta.Backend = %q, want %q (no fake backend rewriting)", meta.Backend, filter.BackendJev)
	}
	if got, want := mustJSON(t, got), mustJSON(t, plain); got != want {
		t.Fatalf("degraded set mismatch:\n got %s\nwant %s", got, want)
	}
	if stub.calls != 1 {
		t.Fatalf("filter calls = %d, want 1 (no retry)", stub.calls)
	}
}

// T08(c): with no filter configured the composition is exactly the plain
// truncation, byte for byte.
func TestSearchFiltered_NilFilterIsPlainSearch(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 12)
	r := memory.NewRetriever(es, vs, nil)

	for _, tc := range []struct{ pool, show int }{
		{12, 4},
		{4, 4},
		{0, 4}, // pool <= 0 is treated as show
		{9, 4},
	} {
		plain, err := r.Search(ctx, "alpha", tc.show)
		if err != nil {
			t.Fatalf("plain search: %v", err)
		}
		got, meta, err := r.SearchFiltered(ctx, "alpha", tc.pool, tc.show, nil, filter.Policy{})
		if err != nil {
			t.Fatalf("pool=%d show=%d: %v", tc.pool, tc.show, err)
		}
		if got, want := mustJSON(t, got), mustJSON(t, plain); got != want {
			t.Fatalf("pool=%d show=%d: none filter differs from plain search:\n got %s\nwant %s", tc.pool, tc.show, got, want)
		}
		if meta.Backend != filter.BackendNone || meta.Degraded {
			t.Fatalf("pool=%d show=%d: meta = %+v, want backend none and not degraded", tc.pool, tc.show, meta)
		}
	}
}

// T07b: the wide pool is what the filter sees, mapped field for field.
func TestSearchFiltered_FilterSeesWidePoolMappedToCandidates(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 6)
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "sf-pinned", Trigger: "alpha", Content: "alpha pinned fact", Pinned: true, CharCount: 17,
	}); err != nil {
		t.Fatalf("upsert pinned: %v", err)
	}
	r := memory.NewRetriever(es, vs, nil)

	wide, err := r.Search(ctx, "alpha", 7)
	if err != nil {
		t.Fatalf("wide search: %v", err)
	}
	probs := make([]float64, len(wide))
	for i := range probs {
		probs[i] = 0.9
	}
	stub := &stubFilter{probs: probs, backend: filter.BackendJev}
	if _, _, err := r.SearchFiltered(ctx, "alpha", 7, 3, stub, filter.DefaultPolicy()); err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	if len(stub.seen) != len(wide) {
		t.Fatalf("filter saw %d candidates, want the %d-wide pool", len(stub.seen), len(wide))
	}
	for i, cand := range stub.seen {
		if cand.ID != wide[i].ID || cand.Name != wide[i].Name || cand.Text != wide[i].Content ||
			cand.Trigger != wide[i].Trigger || cand.Pinned != wide[i].Pinned || cand.Score != wide[i].Score {
			t.Fatalf("candidate %d = %+v, want mapping of %+v", i, cand, wide[i])
		}
	}
	pinnedSeen := false
	for _, cand := range stub.seen {
		if cand.Name == "sf-pinned" {
			pinnedSeen = cand.Pinned && cand.Trigger == "alpha"
		}
	}
	if !pinnedSeen {
		t.Fatalf("pinned entry not mapped with Pinned/Trigger: %+v", stub.seen)
	}
}

// T07b: only entries above Theta survive, probability-descending, and the
// composition owns Kept/Dropped.
func TestSearchFiltered_KeepsOnlyAboveThetaAndDropsTheRest(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 12)
	r := memory.NewRetriever(es, vs, nil)

	wide, err := r.Search(ctx, "alpha", 12)
	if err != nil {
		t.Fatalf("wide search: %v", err)
	}
	probs := make([]float64, len(wide))
	for i := range probs {
		probs[i] = 0.1
	}
	// One strong hit at the tail and one medium hit in the middle: the output
	// must follow probability, not the fused order.
	probs[len(wide)-1] = 0.95
	probs[1] = 0.6
	stub := &stubFilter{probs: probs, backend: filter.BackendJev}

	got, meta, err := r.SearchFiltered(ctx, "alpha", 12, 4, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	want := []string{wide[len(wide)-1].Name, wide[1].Name}
	if gotNames := names(got); strings.Join(gotNames, ",") != strings.Join(want, ",") {
		t.Fatalf("filtered names = %v, want %v", gotNames, want)
	}
	if meta.Kept != 2 || meta.Dropped != len(wide)-2 {
		t.Fatalf("meta Kept/Dropped = %d/%d, want 2/%d", meta.Kept, meta.Dropped, len(wide)-2)
	}
	if meta.Backend != filter.BackendJev {
		t.Fatalf("meta.Backend = %q, want %q", meta.Backend, filter.BackendJev)
	}
}

// Output order follows the filter's probabilities; Score stays the RRF score and
// is therefore non-monotonic (consumers must not re-sort by it).
func TestSearchFiltered_OrderFollowsProbabilityNotScore(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 12)
	r := memory.NewRetriever(es, vs, nil)

	wide, err := r.Search(ctx, "alpha", 12)
	if err != nil {
		t.Fatalf("wide search: %v", err)
	}
	probs := make([]float64, len(wide))
	for i := range probs {
		probs[i] = 0.5 + float64(i)/float64(10*len(wide)) // increasing: reverses the fused order
	}
	stub := &stubFilter{probs: probs, backend: filter.BackendJev}

	got, _, err := r.SearchFiltered(ctx, "alpha", 12, 12, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	if len(got) != len(wide) {
		t.Fatalf("got %d results, want %d", len(got), len(wide))
	}
	for i := range got {
		if got[i].Name != wide[len(wide)-1-i].Name {
			t.Fatalf("position %d = %q, want %q (probability-descending)", i, got[i].Name, wide[len(wide)-1-i].Name)
		}
	}
	if got[0].Score >= got[len(got)-1].Score {
		t.Fatalf("expected non-monotonic RRF scores with probability ordering; got %v", []float64{got[0].Score, got[len(got)-1].Score})
	}
}

// Honest empty: nothing above Theta and nothing above RelaxTheta yields an empty
// short list, never a stuffed top-k.
func TestSearchFiltered_HonestEmpty(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 12)
	r := memory.NewRetriever(es, vs, nil)

	wide, err := r.Search(ctx, "alpha", 12)
	if err != nil {
		t.Fatalf("wide search: %v", err)
	}
	probs := make([]float64, len(wide))
	for i := range probs {
		probs[i] = 0.1
	}
	stub := &stubFilter{probs: probs, backend: filter.BackendJev}
	got, meta, err := r.SearchFiltered(ctx, "alpha", 12, 4, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected honest empty, got %v", names(got))
	}
	if meta.Kept != 0 || meta.Dropped != len(wide) {
		t.Fatalf("meta Kept/Dropped = %d/%d, want 0/%d", meta.Kept, meta.Dropped, len(wide))
	}
}

// Pinned entries whose trigger appears in the query bypass Theta, and the
// composition must pass the query (not just the probabilities) to Select.
func TestSearchFiltered_PinnedTriggerBypassUsesQuery(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 6)
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "sf-pinned", Trigger: "Alpha", Content: "alpha pinned fact", Pinned: true, CharCount: 17,
	}); err != nil {
		t.Fatalf("upsert pinned: %v", err)
	}
	r := memory.NewRetriever(es, vs, nil)

	wide, err := r.Search(ctx, "alpha", 7)
	if err != nil {
		t.Fatalf("wide search: %v", err)
	}
	probs := make([]float64, len(wide))
	for i, hit := range wide {
		if hit.Name == "sf-pinned" {
			probs[i] = 0 // below both thresholds, yet pinned with a matching trigger
			continue
		}
		probs[i] = 0.9
	}
	stub := &stubFilter{probs: probs, backend: filter.BackendJev}
	got, _, err := r.SearchFiltered(ctx, "alpha", 7, 6, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	if len(got) == 0 || got[0].Name != "sf-pinned" {
		t.Fatalf("pinned entry must be injected first, got %v", names(got))
	}
	if !got[0].Pinned {
		t.Fatalf("pinned result lost its Pinned flag: %+v", got[0])
	}
}

// T07a: the stored pinned flag reaches Result.Pinned on every retrieval path.
func TestSearch_ResultCarriesPinnedFlag(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 3)
	if err := es.Upsert(ctx, &memory.Entry{
		Name: "sf-pinned", Trigger: "alpha", Content: "alpha pinned fact", Pinned: true, CharCount: 17,
	}); err != nil {
		t.Fatalf("upsert pinned: %v", err)
	}
	r := memory.NewRetriever(es, vs, nil)

	got, err := r.Search(ctx, "alpha", 8)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	seen := map[string]bool{}
	for _, hit := range got {
		seen[hit.Name] = hit.Pinned
	}
	if pinned, ok := seen["sf-pinned"]; !ok || !pinned {
		t.Fatalf("sf-pinned Pinned = %v (present %v), want true", pinned, ok)
	}
	if seen["sf-00"] {
		t.Fatalf("non-pinned entry sf-00 reported Pinned=true")
	}

	multi, err := r.SearchMulti(ctx, []string{"alpha"}, 8)
	if err != nil {
		t.Fatalf("search multi: %v", err)
	}
	for _, hit := range multi {
		if hit.Name == "sf-pinned" && !hit.Pinned {
			t.Fatalf("SearchMulti lost Pinned on %q", hit.Name)
		}
	}
}

// Parity lock: the three pre-existing search entry points stay mutually
// consistent (Search == SearchWithDiagnostics == single-subquery SearchMulti).
func TestSearchTrio_MutualParity(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 12)
	r := memory.NewRetriever(es, vs, nil)

	for _, query := range []string{"alpha", "alpha note 03", "nothing-matches-this-at-all"} {
		plain, err := r.Search(ctx, query, 5)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		withDiag, diag, err := r.SearchWithDiagnostics(ctx, query, 5)
		if err != nil {
			t.Fatalf("search with diagnostics %q: %v", query, err)
		}
		if diag.SweepUsed {
			t.Fatalf("unexpected cluster sweep for %q", query)
		}
		multi, err := r.SearchMulti(ctx, []string{query}, 5)
		if err != nil {
			t.Fatalf("search multi %q: %v", query, err)
		}
		if mustJSON(t, plain) != mustJSON(t, withDiag) || mustJSON(t, plain) != mustJSON(t, multi) {
			t.Fatalf("parity broken for %q:\n plain %s\n diag  %s\n multi %s",
				query, mustJSON(t, plain), mustJSON(t, withDiag), mustJSON(t, multi))
		}
	}
}

// SearchFiltered must not reshape the unfiltered path's scores: the filtered
// hits carry the same RRF scores the wide pool produced.
func TestSearchFiltered_ScoresCarryOverFromWidePool(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 5)
	r := memory.NewRetriever(es, vs, nil)

	wide, err := r.Search(ctx, "alpha", 5)
	if err != nil {
		t.Fatalf("wide search: %v", err)
	}
	probs := make([]float64, len(wide))
	for i := range probs {
		probs[i] = 0.9
	}
	stub := &stubFilter{probs: probs, backend: filter.BackendJev}
	got, _, err := r.SearchFiltered(ctx, "alpha", 5, 5, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	scores := map[string]float64{}
	for _, hit := range wide {
		scores[hit.Name] = hit.Score
	}
	for _, hit := range got {
		if hit.Score != scores[hit.Name] {
			t.Fatalf("score for %q = %v, want the RRF pool score %v", hit.Name, hit.Score, scores[hit.Name])
		}
	}
}

// A filter that violates the index-alignment contract is a failure, not a
// silently mis-scored pool: the call degrades like any other filter error.
func TestSearchFiltered_ShortProbabilityArrayDegrades(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 6)
	r := memory.NewRetriever(es, vs, nil)

	stub := &stubFilter{probs: []float64{0.9, 0.9}, backend: filter.BackendJev}
	got, meta, err := r.SearchFiltered(ctx, "alpha", 6, 3, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("misaligned filter must degrade without propagating: %v", err)
	}
	if !meta.Degraded {
		t.Fatalf("meta.Degraded = false, want true for a misaligned probability array")
	}
	plain, err := r.Search(ctx, "alpha", 3)
	if err != nil {
		t.Fatalf("plain search: %v", err)
	}
	if mustJSON(t, got) != mustJSON(t, plain) {
		t.Fatalf("misaligned filter must fall back to the fused top-show list")
	}
}

// Degenerate caller input (pool narrower than show) must still yield
// non-negative telemetry and the fused top-show fallback.
func TestSearchFiltered_PoolNarrowerThanShowStaysSane(t *testing.T) {
	ctx := context.Background()
	es, vs := seedFilteredCorpus(t, 12)
	r := memory.NewRetriever(es, vs, nil)

	stub := &stubFilter{backend: filter.BackendJev, fail: errors.New("shard failure")}
	got, meta, err := r.SearchFiltered(ctx, "alpha", 3, 8, stub, filter.DefaultPolicy())
	if err != nil {
		t.Fatalf("search filtered: %v", err)
	}
	if meta.Dropped < 0 {
		t.Fatalf("meta.Dropped = %d, want non-negative", meta.Dropped)
	}
	if len(got) > 8 {
		t.Fatalf("got %d results, want at most show=8", len(got))
	}
	if !meta.Degraded {
		t.Fatalf("meta.Degraded = false, want true on the degraded path")
	}
}
