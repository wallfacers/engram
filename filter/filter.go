// Package filter holds the retrieval-side relevance filter contract for engram.
//
// A RelevanceFilter scores retrieved candidates against a query and returns a
// calibrated probability per candidate; Select/SelectForQuery then apply the
// keep/drop policy as a pure function. Splitting scoring (a model call) from
// selection (a threshold policy) keeps the policy unit-testable offline and lets
// adapters re-tune theta without touching the client.
//
// The package depends on nothing else in engram: candidates are carried by the
// neutral Candidate type so memory/ can depend on filter/ without a cycle
// (same shape as embedding.Reranker). A nil RelevanceFilter means "no filtering"
// and callers fall back to their fused order.
package filter

import (
	"context"
	"reflect"
	"sort"
	"strings"
)

// Backend identifiers reported in FilterMeta.Backend.
const (
	BackendNone = "none"
	BackendJev  = "jev"
)

// Candidate is one retrieval hit offered to a RelevanceFilter. Trigger, Pinned
// and Score come from the stored entry (memory.Entry) and the fused retrieval
// order; ID/Name/Text are what the Jev pointer protocol sends to the model.
type Candidate struct {
	ID      string
	Name    string
	Text    string
	Trigger string
	Pinned  bool
	Score   float64
}

// FilterMeta is the per-call telemetry attached to a filtered search. Kept
// counts the entries the filter's own policy would show (pinned injections
// included, which is why they never land in Dropped); Dropped is the rest.
// InputTokens/OutputTokens carry the backend's reported usage through to callers
// that account for the filter segment (evaluation cost reporting); they stay 0
// when the backend reported no usage, because invented numbers are worse than
// an explicit unknown. Notes carries honest, non-numeric caveats (unknown cost,
// pathological pinned override, negative cache). Notes stays engine-side: the
// memory_search wire contract does not serialize it.
type FilterMeta struct {
	Backend      string
	Theta        float64
	Kept         int
	Dropped      int
	LatencyMs    int
	CostUSD      float64
	InputTokens  int
	OutputTokens int
	Degraded     bool
	Notes        []string
}

// Policy is the pure-data keep/drop policy. The zero value normalizes to
// DefaultPolicy via WithDefaults; start from DefaultPolicy() and override single
// fields (a bare Policy{RelaxDisabled: true} means Theta 0, i.e. keep everything).
type Policy struct {
	Theta      float64
	RelaxTheta float64
	RelaxMax   int
	KShowMax   int
	// RelaxDisabled is the ENGRAM_JEV_RELAX=0 kill-switch: when true the relax
	// stage is skipped entirely and "nothing above Theta" yields an honest empty
	// result. The environment variable is read by adapters, never here — the
	// engine stays host-free.
	RelaxDisabled bool
}

// DefaultPolicy is the shipped policy: keep p >= 0.5 (max 12), otherwise relax
// once to p >= 0.35 (max 3), otherwise return empty.
func DefaultPolicy() Policy {
	return Policy{Theta: 0.5, RelaxTheta: 0.35, RelaxMax: 3, KShowMax: 12}
}

// WithDefaults maps the zero-value policy to DefaultPolicy so an unset Policy{}
// behaves like the documented default. Any partially set policy is used as
// given, where KShowMax <= 0 means "no cap".
func (p Policy) WithDefaults() Policy {
	if p == (Policy{}) {
		return DefaultPolicy()
	}
	return p
}

// RelevanceFilter scores candidates against a query. Implementations return one
// calibrated probability per candidate, aligned by index, or an error plus
// Degraded=true — never a partial list, because a half-scored pool produces an
// unreproducible short list.
type RelevanceFilter interface {
	Filter(ctx context.Context, query string, cands []Candidate) ([]float64, FilterMeta, error)
}

// None is the no-op filter: every candidate scores 1.0 so the threshold stage is
// a no-op and callers keep their fused order. The composition layer truncates
// the unfiltered path to `show`, which is what makes None equivalent to a plain
// truncation while still flowing through the same Select code path.
type None struct{}

// Filter implements RelevanceFilter for the no-op filter.
func (None) Filter(_ context.Context, _ string, cands []Candidate) ([]float64, FilterMeta, error) {
	probs := make([]float64, len(cands))
	for i := range probs {
		probs[i] = 1
	}
	return probs, FilterMeta{Backend: BackendNone, Kept: len(cands)}, nil
}

// IsNilRelevanceFilter reports whether flt carries no filter at all. A concrete
// nil (for example a *jev.Client whose constructor reported "unconfigured")
// must collapse to "no filter" at every adapter boundary: otherwise the
// interface would be non-nil and calling Filter would dereference a nil
// receiver (typed-nil discipline, AGENTS.md).
func IsNilRelevanceFilter(flt RelevanceFilter) bool {
	if flt == nil {
		return true
	}
	value := reflect.ValueOf(flt)
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

// Select applies pol without query context, so no trigger bypass can match
// (a trigger is only "found in" a query). Callers that have the query must use
// SelectForQuery.
func Select(probs []float64, cands []Candidate, pol Policy) []int {
	return SelectForQuery("", probs, cands, pol)
}

// SelectForQuery applies pol to probs/cands and returns the kept indices:
// pinned entries whose Trigger appears in the query first, then the rest by
// probability descending. probs must be aligned with cands; entries beyond
// len(probs) are treated as probability 0 but can still be pinned-injected.
func SelectForQuery(query string, probs []float64, cands []Candidate, pol Policy) []int {
	if len(cands) == 0 {
		return nil
	}
	pol = pol.WithDefaults()
	probAt := func(i int) float64 {
		if i < len(probs) {
			return probs[i]
		}
		return 0
	}
	pinned, pinnedSet := pinnedInjections(query, cands, probAt)
	kept := thresholdKeep(probs, cands, pinnedSet, pol.Theta)
	if len(kept) == 0 && !pol.RelaxDisabled {
		kept = thresholdKeep(probs, cands, pinnedSet, pol.RelaxTheta)
		if pol.RelaxMax > 0 && len(kept) > pol.RelaxMax {
			kept = kept[:pol.RelaxMax]
		}
	}
	if pol.KShowMax > 0 {
		room := pol.KShowMax - len(pinned)
		if room < 0 {
			// Pathological: more pinned hits than the cap. All pinned entries
			// still show (the user asked for them) and callers may note the
			// override in telemetry.
			room = 0
		}
		if len(kept) > room {
			kept = kept[:room]
		}
	}
	out := make([]int, 0, len(pinned)+len(kept))
	out = append(out, pinned...)
	out = append(out, kept...)
	return out
}

// thresholdKeep returns indices whose probability reaches theta, probability
// descending (ties keep input order), skipping pinned entries that are injected
// separately.
func thresholdKeep(probs []float64, cands []Candidate, pinned map[int]bool, theta float64) []int {
	type hit struct {
		idx int
		p   float64
	}
	hits := make([]hit, 0, len(cands))
	for i := range cands {
		if pinned[i] {
			continue
		}
		p := 0.0
		if i < len(probs) {
			p = probs[i]
		}
		if p >= theta {
			hits = append(hits, hit{idx: i, p: p})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].p != hits[b].p {
			return hits[a].p > hits[b].p
		}
		return hits[a].idx < hits[b].idx
	})
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

// PinnedMatch reports whether a candidate is a pinned entry whose non-empty
// Trigger appears in the query (case-insensitive). It is the single spelling of
// the trigger-bypass rule: SelectForQuery uses it to build the pinned prefix, and
// adapters use it to keep that prefix intact when they truncate a shortlist to
// their own show budget (the pathological more-pinned-than-KShowMax case still
// shows every pinned entry).
func PinnedMatch(query string, c Candidate) bool {
	if !c.Pinned || c.Trigger == "" || query == "" {
		return false
	}
	return strings.Contains(strings.ToLower(query), strings.ToLower(c.Trigger))
}

// pinnedInjections returns pinned candidates whose non-empty Trigger appears in
// the query (case-insensitive), probability descending, plus a set for
// exclusion from the threshold stage.
func pinnedInjections(query string, cands []Candidate, probAt func(int) float64) ([]int, map[int]bool) {
	set := map[int]bool{}
	if query == "" {
		return nil, set
	}
	type hit struct {
		idx int
		p   float64
	}
	var hits []hit
	for i, c := range cands {
		if !PinnedMatch(query, c) {
			continue
		}
		hits = append(hits, hit{idx: i, p: probAt(i)})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].p != hits[b].p {
			return hits[a].p > hits[b].p
		}
		return hits[a].idx < hits[b].idx
	})
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
		set[h.idx] = true
	}
	return out, set
}
