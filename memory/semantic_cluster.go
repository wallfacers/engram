package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/wallfacers/engram/embedding"
)

// Semantic cluster signal attribution. Clusters record the signal that
// triggered the merge so mis-clustering is observable (FR-006 / research.md R3).
const (
	ClusterSignalEntity    = "entity"    // shared normalized entity token
	ClusterSignalKeyword   = "keyword"   // shared keyword Jaccard >= threshold
	ClusterSignalEmbedding = "embedding" // embedding cosine overlay (optional)
)

// ErrEpisodeClustererRequired is returned by RebuildAll when the clusterer is
// nil (contracts/engine-api.md §3).
var ErrEpisodeClustererRequired = errors.New("memory: episode clusterer is required")

// ClusterOptions configures a SemanticClusterer. Zero value is usable: the
// zero MinKeywordJaccard resolves to the default, and zero MaxEvidencePerEpisode
// resolves to the default cap.
type ClusterOptions struct {
	// MinKeywordJaccard is the shared-keyword Jaccard threshold for the offline
	// keyword signal. Zero resolves to the default 0.25 (research.md R3). This is
	// deliberately looser than 024's write_dedup 0.7: clustering is a grouping
	// action, not a suppression action.
	MinKeywordJaccard float64
	// MaxEvidencePerEpisode bounds each cluster. Zero resolves to the default 8
	// (research.md R4, aligned with 024 SiblingFacts maxSiblings).
	MaxEvidencePerEpisode int
	// EmbedThresh is the cosine threshold for the optional embedding overlay.
	// Zero resolves to the default 0.9. Only used by NewHybridClusterer.
	EmbedThresh float64
}

func (o ClusterOptions) minKeywordJaccard() float64 {
	if o.MinKeywordJaccard <= 0 {
		return 0.25
	}
	return o.MinKeywordJaccard
}

func (o ClusterOptions) maxEvidencePerEpisode() int {
	if o.MaxEvidencePerEpisode <= 0 {
		return 8
	}
	return o.MaxEvidencePerEpisode
}

func (o ClusterOptions) embedThresh() float64 {
	if o.EmbedThresh <= 0 {
		return 0.9
	}
	return o.EmbedThresh
}

// EpisodeCluster is one cross-session semantic cluster of Evidence. EvidenceIDs
// are deterministically ordered (ordinal asc, then recorded_at, then id);
// Signal records which trigger fired (audit, FR-006).
type EpisodeCluster struct {
	EvidenceIDs []string
	Signal      string
}

// SemanticClusterer groups cross-session Evidence into semantic clusters.
// Implementations MUST be deterministic and bounded; a nil embedding client
// must fall back to the pure offline path (constitution I/V).
type SemanticClusterer interface {
	Cluster(ctx context.Context, evidence []Evidence) ([]EpisodeCluster, error)
}

// ClusterStats reports the audit counters from a clustering pass (FR-006).
type ClusterStats struct {
	Decisions      int // pairwise similarity decisions evaluated
	Clusters       int // clusters produced
	SuspectedMist  int // clusters near the bound / weak edge (audit signal)
	TotalEvidence  int // input evidence considered
}

// offlineClusterer implements SemanticClusterer with deterministic offline
// signals only: shared normalized entity token (exact) OR shared keyword
// Jaccard >= threshold. No embedding endpoint is required (SC-005).
type offlineClusterer struct {
	opts      ClusterOptions
	lastStats ClusterStats
}

// NewOfflineClusterer creates the pure-offline SemanticClusterer. It never
// touches an embedding endpoint; shared entity/keyword signals drive clustering.
func NewOfflineClusterer(opts ClusterOptions) SemanticClusterer {
	return &offlineClusterer{opts: opts}
}

// hybridClusterer wraps the offline clusterer and overlays an optional
// embedding-cosine signal. When the embedder is nil (unconfigured sidecar), it
// degrades to the pure offline path (constitution I/V). The overlay only adds
// evidence that the offline pass left unclustered: evidence whose cosine against
// a cluster's seed exceeds embedThresh joins that cluster. This keeps the
// offline result byte-identical when no embedder is configured.
type hybridClusterer struct {
	opts     ClusterOptions
	offline  SemanticClusterer
	embedder embedding.Client // nil-safe: concrete-nil collapses to untyped nil at the boundary
}

// NewHybridClusterer creates the clusterer with an optional embedding overlay.
// Pass a nil client (or a concrete-nil *embedding.Client) for the pure offline
// path.
func NewHybridClusterer(opts ClusterOptions, client embedding.Client) SemanticClusterer {
	// Collapse a concrete-nil *embedding.Client to an untyped-nil interface so
	// the nil check below works (typed-nil discipline, CLAUDE.md).
	if client == nil {
		return &offlineClusterer{opts: opts}
	}
	return &hybridClusterer{
		opts:     opts,
		offline:  NewOfflineClusterer(opts),
		embedder: client,
	}
}

// Cluster implements SemanticClusterer: offline pass first, then embedding
// overlay on the unclustered remainder.
func (h *hybridClusterer) Cluster(ctx context.Context, evidence []Evidence) ([]EpisodeCluster, error) {
	if h.offline == nil {
		return nil, fmt.Errorf("memory: nil offline clusterer")
	}
	clusters, err := h.offline.Cluster(ctx, evidence)
	if err != nil {
		return nil, err
	}
	if h.embedder == nil || len(clusters) == 0 || len(evidence) == 0 {
		return clusters, nil
	}
	return h.overlayEmbedding(ctx, evidence, clusters)
}

// Stats forwards the offline pass audit counters (FR-006). Embedding-overlay
// additions do not change the offline decision/suspect counters; they only add
// clusters' membership, so the offline counts remain the auditable baseline.
func (h *hybridClusterer) Stats() ClusterStats {
	if off, ok := h.offline.(interface{ Stats() ClusterStats }); ok {
		return off.Stats()
	}
	return ClusterStats{}
}

// overlayEmbedding assigns evidence that no offline cluster claimed to the
// cluster whose seed has the highest cosine above embedThresh. Only evidence
// with a cosine >= threshold joins; the rest stay unclustered. Evidence already
// claimed by an offline cluster is never moved (determinism preserved).
func (h *hybridClusterer) overlayEmbedding(ctx context.Context, evidence []Evidence, clusters []EpisodeCluster) ([]EpisodeCluster, error) {
	claimed := make(map[string]bool)
	for _, c := range clusters {
		for _, id := range c.EvidenceIDs {
			claimed[id] = true
		}
	}

	// Seed text of each cluster: concatenate member content for a stable vector.
	type seed struct {
		text string
		ids  []string
	}
	seeds := make([]seed, len(clusters))
	var unclaimed []Evidence
	for i, c := range clusters {
		var sb strings.Builder
		for _, id := range c.EvidenceIDs {
			for _, ev := range evidence {
				if ev.ID == id {
					sb.WriteString(ev.Content)
					sb.WriteString("\n")
					break
				}
			}
		}
		seeds[i] = seed{text: sb.String(), ids: c.EvidenceIDs}
	}
	for _, ev := range evidence {
		if !claimed[ev.ID] {
			unclaimed = append(unclaimed, ev)
		}
	}
	if len(unclaimed) == 0 {
		return clusters, nil
	}

	seedTexts := make([]string, len(seeds))
	for i := range seeds {
		seedTexts[i] = seeds[i].text
	}
	unclaimedTexts := make([]string, len(unclaimed))
	for i := range unclaimed {
		unclaimedTexts[i] = unclaimed[i].Content
	}

	// Embed in two calls: seeds and unclaimed. Batch size is bounded by callers.
	seedVecs, err := h.embedder.Embed(ctx, seedTexts)
	if err != nil {
		// Embedding failure degrades gracefully: offline clusters stand.
		return clusters, nil
	}
	unclaimedVecs, err := h.embedder.Embed(ctx, unclaimedTexts)
	if err != nil {
		return clusters, nil
	}
	if len(seedVecs) != len(seeds) || len(unclaimedVecs) != len(unclaimed) {
		return clusters, nil
	}

	thresh := h.opts.embedThresh()
	for u, vec := range unclaimedVecs {
		bestIdx, bestSim := -1, thresh
		for s, svec := range seedVecs {
			sim := cosineSim(vec, svec)
			if sim >= bestSim {
				bestSim = sim
				bestIdx = s
			}
		}
		if bestIdx < 0 {
			continue
		}
		if len(seeds[bestIdx].ids) >= h.opts.maxEvidencePerEpisode() {
			continue // bound respected
		}
		clusters[bestIdx].EvidenceIDs = append(clusters[bestIdx].EvidenceIDs, unclaimed[u].ID)
		if clusters[bestIdx].Signal == "" {
			clusters[bestIdx].Signal = ClusterSignalEmbedding
		}
		seeds[bestIdx].ids = append(seeds[bestIdx].ids, unclaimed[u].ID)
	}
	return clusters, nil
}

func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// tokenizeSignificant extracts the significant tokens of an evidence content
// for offline similarity: normalized word-like runs, dropping stopwords and
// very short tokens. CJK characters are kept whole (word segmentation is out of
// scope, consistent with EntityQueryTokens).
func tokenizeSignificant(content string) []string {
	norm := EntityNorm(content)
	if norm == "" {
		return nil
	}
	var out []string
	var current strings.Builder
	flush := func() {
		tok := current.String()
		current.Reset()
		tok = strings.TrimSpace(tok)
		if len(tok) < 3 {
			return // drop very short / punctuation fragments
		}
		if isStopword(tok) {
			return
		}
		out = append(out, tok)
	}
	for _, r := range norm {
		switch {
		case r == ' ':
			flush()
		case unicodeIsLetterOrDigit(r):
			current.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

func unicodeIsLetterOrDigit(r rune) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
		return true
	}
	return r >= 0x80 // non-ASCII (CJK etc.) kept whole
}

// isStopword removes high-frequency English function words that carry no
// topical signal. Kept intentionally small; CJK has no stopwords here.
func isStopword(tok string) bool {
	switch tok {
	case "the", "and", "for", "that", "this", "with", "are", "was",
		"from", "have", "has", "had", "will", "would", "should", "could",
		"about", "into", "than", "then", "they", "them", "their", "there",
		"what", "when", "where", "which", "while", "your", "youre",
		"been", "being", "does", "done", "did", "going", "gonna", "want",
		"need", "just", "like", "know", "really", "also", "but", "can":
		return true
	}
	return false
}

func tokenSet(tokens []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tokens))
	for _, tok := range tokens {
		set[tok] = struct{}{}
	}
	return set
}

// jaccardShared computes the Jaccard similarity of two token sets.
func jaccardShared(a, b map[string]struct{}) (inter, union int) {
	if len(a) == 0 || len(b) == 0 {
		return 0, len(a) + len(b)
	}
	// iterate the smaller set for intersections.
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	for tok := range small {
		if _, ok := large[tok]; ok {
			inter++
		}
	}
	union = len(a) + len(b) - inter
	return inter, union
}

// scoredEvidence is the per-evidence clustering working state.
type scoredEvidence struct {
	ev       Evidence
	tokens   map[string]struct{}
	assigned bool
}

// Cluster implements SemanticClusterer using a deterministic greedy pass.
// Evidence order is fixed first (ordinal, recorded_at, id) so results are
// reproducible. Each cluster is grown from its earliest unassigned evidence,
// adding up to cap-1 additional evidence that shares an entity token or meets
// the keyword Jaccard threshold with the cluster's seed.
func (c *offlineClusterer) Cluster(ctx context.Context, evidence []Evidence) ([]EpisodeCluster, error) {
	if ctx == nil {
		return nil, fmt.Errorf("memory: nil context")
	}
	ordered := append([]Evidence(nil), evidence...)
	sortEvidence(ordered)
	capN := c.opts.maxEvidencePerEpisode()

	// Pre-tokenize each evidence and build an entity-token → evidence index so
	// the pass is O(n · tokens) rather than O(n²) on content.
	items := make([]scoredEvidence, len(ordered))
	entityIndex := make(map[string][]int)
	for i, ev := range ordered {
		tokens := tokenSet(tokenizeSignificant(ev.Content))
		items[i] = scoredEvidence{ev: ev, tokens: tokens}
		for tok := range tokens {
			entityIndex[tok] = append(entityIndex[tok], i)
		}
	}

	var clusters []EpisodeCluster
	var stats ClusterStats
	stats.TotalEvidence = len(ordered)

	for i := range items {
		if items[i].assigned {
			continue
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		items[i].assigned = true

		members := []int{i}
		// Candidate pool: evidence sharing at least one entity token with the
		// seed, plus a deterministic scan for keyword-only matches.
		candidates := candidateNeighbors(entityIndex, items[i].tokens, i, capN)
		// The candidate set from the entity index covers token-sharing evidence;
		// also scan the rest for keyword-only (no shared exact token) matches
		// within the bound.
		if len(candidates) < capN {
			candidates = append(candidates, scanKeywordNeighbors(items, i, c.opts.minKeywordJaccard(), capN-len(candidates))...)
		}
		// Deterministic order, cap applied by taking the first bound items.
		sort.Ints(candidates)
		if len(candidates) > capN-1 {
			candidates = candidates[:capN-1]
		}
		signal := ""
		for _, j := range candidates {
			if items[j].assigned {
				continue
			}
			inter, _ := jaccardShared(items[i].tokens, items[j].tokens)
			if inter > 0 {
				// shared entity token.
				items[j].assigned = true
				members = append(members, j)
				if signal == "" {
					signal = ClusterSignalEntity
				}
			} else {
				// keyword-only: Jaccard threshold.
				s, u := jaccardShared(items[i].tokens, items[j].tokens)
				if u == 0 || float64(s)/float64(u) < c.opts.minKeywordJaccard() {
					continue
				}
				items[j].assigned = true
				members = append(members, j)
				if signal == "" {
					signal = ClusterSignalKeyword
				}
			}
			stats.Decisions++
			if len(members) >= capN {
				stats.SuspectedMist++ // truncated at the bound
				break
			}
		}
		if len(members) < 2 {
			continue // singleton evidence is not a cluster
		}
		ids := make([]string, len(members))
		for k, idx := range members {
			ids[k] = items[idx].ev.ID
		}
		if signal == "" {
			signal = ClusterSignalKeyword // default attribution
		}
		clusters = append(clusters, EpisodeCluster{EvidenceIDs: ids, Signal: signal})
		stats.Clusters++
	}
	c.lastStats = stats
	return clusters, nil
}

// Stats returns the audit counters from the most recent Cluster call (FR-006).
func (c *offlineClusterer) Stats() ClusterStats {
	return c.lastStats
}

// sortEvidence fixes a deterministic order for clustering.
func sortEvidence(evidence []Evidence) {
	sort.Slice(evidence, func(a, b int) bool {
		if evidence[a].SourceSessionID != evidence[b].SourceSessionID {
			return evidence[a].SourceSessionID < evidence[b].SourceSessionID
		}
		if evidence[a].Ordinal != evidence[b].Ordinal {
			return evidence[a].Ordinal < evidence[b].Ordinal
		}
		if evidence[a].RecordedAt.UnixNano() != evidence[b].RecordedAt.UnixNano() {
			return evidence[a].RecordedAt.Before(evidence[b].RecordedAt)
		}
		return evidence[a].ID < evidence[b].ID
	})
}

// candidateNeighbors returns indices of unassigned evidence sharing at least one
// token with the seed's token set, capped at n.
func candidateNeighbors(entityIndex map[string][]int, seedTokens map[string]struct{}, seedIdx, n int) []int {
	var out []int
	seen := map[int]struct{}{seedIdx: {}}
	for tok := range seedTokens {
		for _, idx := range entityIndex[tok] {
			if _, dup := seen[idx]; dup {
				continue
			}
			seen[idx] = struct{}{}
			out = append(out, idx)
			if len(out) >= n*3 { // generous candidate pool, capped later
				return out
			}
		}
	}
	return out
}

// scanKeywordNeighbors returns indices of unassigned evidence sharing no exact
// token but whose Jaccard with the seed meets the threshold. Deterministic scan
// over the ordered slice.
func scanKeywordNeighbors(items []scoredEvidence, seedIdx int, minJaccard float64, n int) []int {
	if n <= 0 {
		return nil
	}
	var out []int
	seed := items[seedIdx].tokens
	for j := range items {
		if j == seedIdx || items[j].assigned {
			continue
		}
		s, u := jaccardShared(seed, items[j].tokens)
		if u > 0 && float64(s)/float64(u) >= minJaccard {
			out = append(out, j)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}
