package main

import (
	"context"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/wallfacers/engram/memory"
)

// RepresentationKind identifies one of the bake-off arms.
type RepresentationKind string

const (
	ReprChunk900        RepresentationKind = "chunk_900"
	ReprRawTurnWindow   RepresentationKind = "raw_turn_window"
	ReprSemanticEpisode RepresentationKind = "semantic_episode"
)

// RepresentationRenderer produces rendered candidates from the same set of
// ranked anchors. Each renderer receives the same anchors, projection store,
// and evidence reader; the only difference is how it expands source material.
type RepresentationRenderer interface {
	Render(ctx context.Context, anchors []evalRankedAnchor) ([]evalRenderedCandidate, error)
}

// evidenceReader is a narrow interface satisfied by LedgerStore for reading
// active Evidence by ID or listing by session.
type evidenceReader interface {
	Get(ctx context.Context, evidenceID string) (*memory.Evidence, error)
	GetMany(ctx context.Context, evidenceIDs []string) (map[string]memory.Evidence, error)
	ListSession(ctx context.Context, sourceSessionID string, includeTombstoned bool) ([]memory.Evidence, error)
}

// chunk900Renderer splits anchor source Evidence into ~900-character chunks.
// It reads the full source text for each ranked anchor and partitions it into
// fixed-size chunks. This is the legacy representation arm.
type chunk900Renderer struct {
	projections *memory.ProjectionStore
	evidence    evidenceReader
}

// NewChunk900Renderer creates the chunk_900 representation arm. Pass nil for
// projections or evidence only when the renderer will not be used to render.
func NewChunk900Renderer(projections *memory.ProjectionStore, evidence evidenceReader) RepresentationRenderer {
	return &chunk900Renderer{projections: projections, evidence: evidence}
}

const chunk900TargetChars = 900

func (r *chunk900Renderer) Render(ctx context.Context, anchors []evalRankedAnchor) ([]evalRenderedCandidate, error) {
	if len(anchors) == 0 {
		return nil, nil
	}
	if r.evidence == nil {
		return nil, fmt.Errorf("chunk_900 renderer requires an evidence reader")
	}

	var candidates []evalRenderedCandidate
	rank := 0

	for _, anchor := range anchors {
		for sourceIdx, sourceID := range anchor.SourceIDs {
			ev, err := r.evidence.Get(ctx, sourceID)
			if err != nil {
				// Skip unavailable evidence for bake-off comparison;
				// the miss-attribution layer records the gap.
				continue
			}
			content := ev.Content
			if content == "" {
				continue
			}
			runes := []rune(content)
			chunkIndex := 0
			for offset := 0; offset < len(runes); offset += chunk900TargetChars {
				end := offset + chunk900TargetChars
				if end > len(runes) {
					end = len(runes)
				}
				chunkText := string(runes[offset:end])
				rank++
				candidateID := fmt.Sprintf("%s/s%d/chunk:%d", anchor.CandidateID, sourceIdx, chunkIndex)
				candidates = append(candidates, evalRenderedCandidate{
					CandidateID:    candidateID,
					Kind:           string(ReprChunk900),
					Rank:           rank,
					Score:          anchor.Score,
					Text:           chunkText,
					TextDigest:     evalTextDigest(chunkText),
					SourceIDs:      []string{ev.ID},
					ExpandedFrom:   []string{anchor.CandidateID},
					ExpansionCount: (len(runes) / chunk900TargetChars),
				})
				chunkIndex++
			}
		}
	}
	return candidates, nil
}

// rawTurnWindowRenderer expands each anchor into a window of conversation
// turns from the same session. The windowSize parameter controls how many
// adjacent turns to include on each side (0 = anchor only).
type rawTurnWindowRenderer struct {
	projections *memory.ProjectionStore
	evidence    evidenceReader
	windowSize  int
}

// NewRawTurnWindowRenderer creates the raw_turn_window representation arm.
func NewRawTurnWindowRenderer(projections *memory.ProjectionStore, evidence evidenceReader, windowSize int) RepresentationRenderer {
	if windowSize < 0 {
		windowSize = 0
	}
	return &rawTurnWindowRenderer{projections: projections, evidence: evidence, windowSize: windowSize}
}

func (r *rawTurnWindowRenderer) Render(ctx context.Context, anchors []evalRankedAnchor) ([]evalRenderedCandidate, error) {
	if len(anchors) == 0 {
		return nil, nil
	}
	if r.evidence == nil {
		return nil, fmt.Errorf("raw_turn_window renderer requires an evidence reader")
	}

	// Group anchors by session to batch-fetch session evidence.
	type sessionKey string
	sessionCache := make(map[sessionKey][]memory.Evidence)

	var candidates []evalRenderedCandidate
	rank := 0

	for _, anchor := range anchors {
		for sourceIdx, sourceID := range anchor.SourceIDs {
			ev, err := r.evidence.Get(ctx, sourceID)
			if err != nil {
				continue
			}
			sessionID := sessionKey(ev.SourceSessionID)
			session, ok := sessionCache[sessionID]
			if !ok {
				var listErr error
				session, listErr = r.evidence.ListSession(ctx, ev.SourceSessionID, false)
				if listErr != nil {
					continue
				}
				sessionCache[sessionID] = session
			}

			// Find the anchor's position in the session.
			anchorPos := -1
			for i, s := range session {
				if s.ID == ev.ID {
					anchorPos = i
					break
				}
			}
			if anchorPos < 0 {
				continue
			}

			// Compute window boundaries.
			start := anchorPos - r.windowSize
			if start < 0 {
				start = 0
			}
			end := anchorPos + r.windowSize + 1 // inclusive of anchor
			if end > len(session) {
				end = len(session)
			}

			// Build window text from session turns.
			window := session[start:end]
			var sourceIDs []string
			var text string
			for _, turn := range window {
				sourceIDs = append(sourceIDs, turn.ID)
				text += turn.Speaker + ": " + turn.Content + "\n"
			}

			rank++
			candidateID := fmt.Sprintf("%s/s%d/window:%d", anchor.CandidateID, sourceIdx, anchorPos)
			candidates = append(candidates, evalRenderedCandidate{
				CandidateID:    candidateID,
				Kind:           string(ReprRawTurnWindow),
				Rank:           rank,
				Score:          anchor.Score,
				Text:           text,
				TextDigest:     evalTextDigest(text),
				SourceIDs:      sourceIDs,
				ExpandedFrom:   []string{anchor.CandidateID},
				ExpansionCount: len(window) - 1,
				PreCapInputTokens: utf8.RuneCountInString(text),
			})
		}
	}
	return candidates, nil
}

// semanticEpisodeRenderer resolves each anchor through the episode store.
// If the anchor's candidate ID matches an episode projection, it reads the
// episode narrative. Otherwise it falls back to reading the anchor's evidence
// directly (graceful degradation).
type semanticEpisodeRenderer struct {
	projections *memory.ProjectionStore
	evidence    evidenceReader
	episodes    *memory.EpisodeStore
}

// NewSemanticEpisodeRenderer creates the semantic_episode representation arm.
func NewSemanticEpisodeRenderer(projections *memory.ProjectionStore, evidence evidenceReader, episodes *memory.EpisodeStore) RepresentationRenderer {
	return &semanticEpisodeRenderer{projections: projections, evidence: evidence, episodes: episodes}
}

func (r *semanticEpisodeRenderer) Render(ctx context.Context, anchors []evalRankedAnchor) ([]evalRenderedCandidate, error) {
	if len(anchors) == 0 {
		return nil, nil
	}

	// Collect all anchor candidate IDs that might be episode projection IDs.
	var projectionIDs []string
	anchorByID := make(map[string]evalRankedAnchor)
	for _, anchor := range anchors {
		projectionIDs = append(projectionIDs, anchor.CandidateID)
		anchorByID[anchor.CandidateID] = anchor
	}

	// Try to resolve sources from the projection store.
	sourcesByID := make(map[string][]memory.EvidenceRef)
	if r.projections != nil && len(projectionIDs) > 0 {
		sources, err := r.projections.SourcesByProjectionIDs(ctx, projectionIDs)
		if err == nil {
			sourcesByID = sources
		}
		// Degrade gracefully: if SourcesByProjectionIDs fails, fall back to
		// reading evidence directly below.
	}

	var candidates []evalRenderedCandidate
	rank := 0

	for _, anchor := range anchors {
		refs := sourcesByID[anchor.CandidateID]
		if len(refs) == 0 && r.episodes != nil && len(anchor.SourceIDs) > 0 {
			// 025 cross-session semantic episodes: the anchor is a fact/chunk, not
			// an episode projection. Resolve its evidence lineage to the episodes
			// that reference it, and render the first (deterministic) episode so
			// the whole cross-message cluster enters answer context (research.md
			// R5). EpisodesForEvidence is a reverse lineage lookup, not retrieval.
			episodesByEv, epErr := r.episodes.EpisodesForEvidence(ctx, anchor.SourceIDs)
			if epErr == nil {
				for _, evID := range anchor.SourceIDs {
					projs := episodesByEv[evID]
					if len(projs) == 0 {
						continue
					}
					if r.projections == nil {
						break
					}
					epRefs, refErr := r.projections.SourcesByProjectionIDs(ctx, []string{projs[0].ID})
					if refErr == nil {
						refs = epRefs[projs[0].ID]
					}
					if len(refs) > 0 {
						break
					}
				}
			}
		}
		if len(refs) == 0 {
			// Fallback: render from the anchor's source IDs directly.
			for _, sourceID := range anchor.SourceIDs {
				if r.evidence == nil {
					continue
				}
				ev, err := r.evidence.Get(ctx, sourceID)
				if err != nil {
					continue
				}
				rank++
				candidateID := fmt.Sprintf("%s/episode-fallback:%s", anchor.CandidateID, sourceID)
				candidates = append(candidates, evalRenderedCandidate{
					CandidateID:    candidateID,
					Kind:           string(ReprSemanticEpisode),
					Rank:           rank,
					Score:          anchor.Score,
					Text:           ev.Content,
					TextDigest:     evalTextDigest(ev.Content),
					SourceIDs:      []string{ev.ID},
					ExpandedFrom:   []string{anchor.CandidateID},
					ExpansionCount: 0,
				})
			}
			continue
		}

		// Collect evidence IDs from the lineage.
		var evidenceIDs []string
		for _, ref := range refs {
			evidenceIDs = append(evidenceIDs, ref.EvidenceID)
		}

		// Read evidence content.
		var evidenceByID map[string]memory.Evidence
		if r.evidence != nil {
			var err error
			evidenceByID, err = r.evidence.GetMany(ctx, evidenceIDs)
			if err != nil {
				// Degrade: skip this anchor.
				continue
			}
		}

		// Build narrative in source order.
		type sourceOrder struct {
			ref     memory.EvidenceRef
			content string
		}
		var ordered []sourceOrder
		for _, ref := range refs {
			ev, ok := evidenceByID[ref.EvidenceID]
			if !ok {
				continue
			}
			ordered = append(ordered, sourceOrder{ref: ref, content: ev.Content})
		}
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].ref.SourceOrder < ordered[j].ref.SourceOrder
		})

		var text string
		var sourceIDs []string
		for _, item := range ordered {
			sourceIDs = append(sourceIDs, item.ref.EvidenceID)
			text += item.content + "\n"
		}

		rank++
		candidateID := fmt.Sprintf("%s/episode", anchor.CandidateID)
		candidates = append(candidates, evalRenderedCandidate{
			CandidateID:    candidateID,
			Kind:           string(ReprSemanticEpisode),
			Rank:           rank,
			Score:          anchor.Score,
			Text:           text,
			TextDigest:     evalTextDigest(text),
			SourceIDs:      sourceIDs,
			ExpandedFrom:   []string{anchor.CandidateID},
			ExpansionCount: len(sourceIDs) - 1,
		})
	}
	return candidates, nil
}

