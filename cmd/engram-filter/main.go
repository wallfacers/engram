// Command engram-filter is the host-side (B) path of the Jev relevance filter:
// it reads a candidate pool as JSON on stdin and writes the surviving shortlist
// as JSON on stdout. The pool is the caller's own retrieval result, so this tool
// never queries the store and needs no MCP surface.
//
// It reuses the engine's filter package, which is what makes the two paths
// interchangeable: the same threshold policy, the same two-stage relaxation, the
// same pinned/trigger bypass, and the same telemetry as memory_search with
// filter="jev".
//
// Input:  {"query": "...", "candidates": [{"id","name","text","trigger","pinned","score"}]}
// Output: {"query": "...", "indices": [...], "probs": [...]?, "meta": {...}}
//
// Jev is optional and configured through the environment
// (ENGRAM_JEV_BASE_URL/ENGRAM_JEV_MODEL/ENGRAM_JEV_API_KEY, plus the optional
// ENGRAM_JEV_THETA and ENGRAM_JEV_RELAX). An unconfigured or failed filter
// degrades to the caller's fused order truncated to --show, with
// meta.degraded=true — never an error, and never a partial shortlist.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/wallfacers/engram/filter"
	"github.com/wallfacers/engram/filter/jev"
)

const defaultShow = 8

// candidateInput mirrors filter.Candidate, which is the neutral carrier shared
// with the memory engine.
type candidateInput = filter.Candidate

type filterInput struct {
	Query      string           `json:"query"`
	Candidates []candidateInput `json:"candidates"`
}

type filterOutput struct {
	Query   string           `json:"query"`
	Indices []int            `json:"indices"`
	Probs   []float64        `json:"probs,omitempty"`
	Notes   []string         `json:"notes,omitempty"`
	Meta    filterMetaOutput `json:"meta"`
}

type filterMetaOutput struct {
	Backend   string  `json:"backend"`
	Theta     float64 `json:"theta"`
	Kept      int     `json:"kept"`
	Dropped   int     `json:"dropped"`
	Degraded  bool    `json:"degraded"`
	LatencyMs int     `json:"latency_ms"`
	CostUSD   float64 `json:"cost_usd"`
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) error {
	defaults, err := environmentDefaults(getenv)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("engram-filter", flag.ContinueOnError)
	fs.SetOutput(stderr)
	show := fs.Int("show", defaultShow, "maximum number of entries to show")
	theta := fs.Float64("theta", defaults.theta, "relevance threshold in (0,1)")
	relax := fs.Bool("relax", defaults.relax, "keep the two-stage relaxation fallback")
	withProbs := fs.Bool("probs", false, "include the per-entry probabilities in the output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *show < 1 {
		return errors.New("show must be at least 1")
	}
	if *theta <= 0 || *theta >= 1 {
		return errors.New("theta must be in (0,1)")
	}

	policy := filter.DefaultPolicy()
	policy.Theta = *theta
	policy.RelaxDisabled = !*relax
	if policy.RelaxTheta > policy.Theta {
		policy.RelaxTheta = policy.Theta
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read candidate pool: %w", err)
	}
	var in filterInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("decode candidate pool: %w", err)
	}

	shortlist, err := selectShortlist(context.Background(), buildFilter(getenv, policy), in, policy, *show, *withProbs)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(shortlist); err != nil {
		return fmt.Errorf("write shortlist: %w", err)
	}
	return nil
}

// buildFilter constructs the optional Jev client. It returns an untyped nil when
// Jev is unconfigured, which selectShortlist reports as an honest degradation.
func buildFilter(getenv func(string) string, policy filter.Policy) filter.RelevanceFilter {
	client, err := jev.New(jev.Config{
		BaseURL: strings.TrimSpace(getenv("ENGRAM_JEV_BASE_URL")),
		Model:   strings.TrimSpace(getenv("ENGRAM_JEV_MODEL")),
		APIKey:  getenv("ENGRAM_JEV_API_KEY"),
		Policy:  policy,
	})
	if err != nil || client == nil {
		return nil
	}
	return client
}

type envDefaults struct {
	theta float64
	relax bool
}

// environmentDefaults lets the CLI honor the same Jev knobs as the server, while
// flags stay the authoritative interface for one-off runs.
func environmentDefaults(getenv func(string) string) (envDefaults, error) {
	policy := filter.DefaultPolicy()
	values := envDefaults{theta: policy.Theta, relax: true}
	if raw := strings.TrimSpace(getenv("ENGRAM_JEV_THETA")); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || value <= 0 || value >= 1 {
			return envDefaults{}, fmt.Errorf("parse ENGRAM_JEV_THETA: want a number in (0,1), got %q", raw)
		}
		values.theta = value
	}
	if raw := strings.TrimSpace(getenv("ENGRAM_JEV_RELAX")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return envDefaults{}, fmt.Errorf("parse ENGRAM_JEV_RELAX: want a boolean, got %q", raw)
		}
		values.relax = value
	}
	return values, nil
}

// selectShortlist scores the pool and applies the shared selection policy. A
// missing or failing filter degrades to the caller's fused order truncated to
// show: no partial adoption, and a shortlist that is exactly reproducible.
func selectShortlist(ctx context.Context, flt filter.RelevanceFilter, in filterInput, policy filter.Policy, show int, wantProbs bool) (filterOutput, error) {
	degraded := false
	if filter.IsNilRelevanceFilter(flt) {
		flt = filter.None{}
		degraded = true
	}
	probs, meta, err := flt.Filter(ctx, in.Query, in.Candidates)
	if err != nil || len(probs) != len(in.Candidates) {
		probs = uniformProbabilities(len(in.Candidates))
		meta = filter.FilterMeta{Backend: filter.BackendNone}
		degraded = true
	}
	notes := append([]string(nil), meta.Notes...)
	if meta.Backend == "" {
		meta.Backend = filter.BackendNone
	}
	meta.Degraded = meta.Degraded || degraded
	if meta.Theta == 0 {
		meta.Theta = policy.Theta
	}

	kept := filter.SelectForQuery(in.Query, probs, in.Candidates, policy)
	if truncated, overridden := truncateShortlist(in.Query, in.Candidates, kept, show); overridden {
		// Notes is engine-side telemetry in the MCP contract; this CLI is its own
		// surface, so the override is reported to the caller under its own key.
		notes = append(notes, "pinned override: every pinned entry is shown even though they exceed --show")
		kept = truncated
	}
	meta.Kept = len(kept)
	meta.Dropped = max(0, len(in.Candidates)-len(kept))

	out := filterOutput{
		Query:   in.Query,
		Indices: make([]int, 0, len(kept)),
		Notes:   notes,
		Meta: filterMetaOutput{
			Backend:   meta.Backend,
			Theta:     meta.Theta,
			Kept:      meta.Kept,
			Dropped:   meta.Dropped,
			Degraded:  meta.Degraded,
			LatencyMs: meta.LatencyMs,
			CostUSD:   meta.CostUSD,
		},
	}
	out.Indices = append(out.Indices, kept...)
	if wantProbs {
		out.Probs = make([]float64, len(kept))
		for i, idx := range kept {
			out.Probs[i] = probs[idx]
		}
	}
	return out, nil
}

func uniformProbabilities(n int) []float64 {
	probs := make([]float64, n)
	for i := range probs {
		probs[i] = 1
	}
	return probs
}

// truncateShortlist applies the caller's --show budget without ever cutting the
// pinned prefix: relevance-filter.md §3.4 shows every pinned entry whose trigger
// matched the query even when they exceed KShowMax, so the B path must not undo
// that with its own display cap. Only the non-pinned remainder is truncated, and
// overridden reports that the budget had to give way.
func truncateShortlist(query string, cands []filter.Candidate, kept []int, show int) ([]int, bool) {
	if len(kept) <= show {
		return kept, false
	}
	pinned := 0
	for pinned < len(kept) && kept[pinned] < len(cands) && filter.PinnedMatch(query, cands[kept[pinned]]) {
		pinned++
	}
	limit := max(show, pinned)
	if limit >= len(kept) {
		return kept, false
	}
	return kept[:limit], true
}
