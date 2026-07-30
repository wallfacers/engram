package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// graphProjectionReader is precisely the read-only public 003 surface this
// experiment needs. It intentionally excludes UpsertEdges, direct SQL, and
// every schema/data mutation path.
type graphProjectionReader interface {
	EntityCues(context.Context, string) ([]string, error)
	EntityClusterEntries(context.Context, []string) ([]string, error)
}

var _ graphProjectionReader = (*memory.EntryStore)(nil)

const graphShadowFile = "graph-missing-bridge.json"

type graphProjectionRequest struct {
	Config              projectionRunConfig
	Query               string
	SeedCandidates      []evidencecompiler.Candidate
	CandidateCatalog    []evidencecompiler.Candidate
	MaxBridgeCandidates int
}

type graphProjectionShadow struct {
	Metadata           shadowProjectionMetadata `json:"metadata"`
	Cues               []string                 `json:"cues"`
	SeedCandidateIDs   []string                 `json:"seed_candidate_ids"`
	BridgeCandidateIDs []string                 `json:"bridge_candidate_ids"`
}

// buildGraphProjectionShadow queries only the 003 read APIs, then maps their
// entry names to a frozen, source-complete candidate catalog. Unknown graph
// entries are never admitted: an experiment cannot use a graph hit without a
// candidate/source artifact that can be audited later.
func buildGraphProjectionShadow(ctx context.Context, reader graphProjectionReader, request graphProjectionRequest) (graphProjectionShadow, error) {
	if reader == nil {
		return graphProjectionShadow{}, fmt.Errorf("graph projection requires 003 public reader")
	}
	if strings.TrimSpace(request.Query) == "" {
		return graphProjectionShadow{}, fmt.Errorf("graph projection requires query")
	}
	if request.MaxBridgeCandidates <= 0 {
		return graphProjectionShadow{}, fmt.Errorf("graph projection requires positive bridge limit")
	}
	if err := validateProjectionRunConfig(request.Config); err != nil {
		return graphProjectionShadow{}, err
	}
	if err := validateShadowCandidates(request.SeedCandidates); err != nil {
		return graphProjectionShadow{}, fmt.Errorf("invalid graph seed candidates: %w", err)
	}
	if err := validateShadowCandidates(request.CandidateCatalog); err != nil {
		return graphProjectionShadow{}, fmt.Errorf("invalid graph candidate catalog: %w", err)
	}
	if request.Config.CandidateLimit > 0 && len(request.SeedCandidates)+request.MaxBridgeCandidates > request.Config.CandidateLimit {
		return graphProjectionShadow{}, fmt.Errorf("graph seed plus bridge allocation exceeds frozen candidate limit")
	}
	catalog := make(map[string]evidencecompiler.Candidate, len(request.CandidateCatalog))
	for _, candidate := range request.CandidateCatalog {
		entryName := graphEntryName(candidate)
		if existing, exists := catalog[entryName]; exists && existing.ID != candidate.ID {
			return graphProjectionShadow{}, fmt.Errorf("graph catalog maps entry %q to multiple candidates", entryName)
		}
		catalog[entryName] = candidate
	}
	for _, candidate := range request.SeedCandidates {
		entryName := graphEntryName(candidate)
		catalogCandidate, ok := catalog[entryName]
		if !ok || !sameCandidateLineage(catalogCandidate, candidate) {
			return graphProjectionShadow{}, fmt.Errorf("graph seed %q is absent from frozen catalog", candidate.ID)
		}
	}

	cues, err := reader.EntityCues(ctx, request.Query)
	if err != nil {
		return graphProjectionShadow{}, fmt.Errorf("read graph cues: %w", err)
	}
	cues = sortedUniqueNonEmpty(cues)
	if len(cues) == 0 {
		return graphProjectionShadow{}, fmt.Errorf("graph projection found no 003 entity cues")
	}
	entries, err := reader.EntityClusterEntries(ctx, cues)
	if err != nil {
		return graphProjectionShadow{}, fmt.Errorf("read graph bridge entries: %w", err)
	}
	seedEntryNames := make(map[string]struct{}, len(request.SeedCandidates))
	for _, candidate := range request.SeedCandidates {
		seedEntryNames[graphEntryName(candidate)] = struct{}{}
	}
	entryIDs := sortedUniqueNonEmpty(entries)
	bridges := make([]evidencecompiler.Candidate, 0, request.MaxBridgeCandidates)
	for _, entryID := range entryIDs {
		if _, isSeed := seedEntryNames[entryID]; isSeed {
			continue
		}
		candidate, known := catalog[entryID]
		if !known {
			continue
		}
		bridges = append(bridges, candidate)
		if len(bridges) == request.MaxBridgeCandidates {
			break
		}
	}
	if len(bridges) == 0 {
		return graphProjectionShadow{}, fmt.Errorf("graph projection found no source-complete bridge candidate")
	}
	selected := append(cloneCandidates(request.SeedCandidates), cloneCandidates(bridges)...)
	metadata, err := newShadowProjectionMetadata(string(narrowProjectionArmGraph), request.Config, selected)
	if err != nil {
		return graphProjectionShadow{}, err
	}
	shadow := graphProjectionShadow{
		Metadata:           metadata,
		Cues:               cues,
		SeedCandidateIDs:   candidateIDsFromCandidates(request.SeedCandidates),
		BridgeCandidateIDs: candidateIDsFromCandidates(bridges),
	}
	if err := writeShadowProjection(request.Config.RunDir, graphShadowFile, shadow); err != nil {
		return graphProjectionShadow{}, fmt.Errorf("write graph shadow: %w", err)
	}
	return shadow, nil
}

func graphEntryName(candidate evidencecompiler.Candidate) string {
	if candidate.Metadata != nil && strings.TrimSpace(candidate.Metadata["entry_name"]) != "" {
		return strings.TrimSpace(candidate.Metadata["entry_name"])
	}
	return candidate.ID
}

func sortedUniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func graphShadowPath(runDir string) string {
	path, _ := shadowProjectionPath(runDir, graphShadowFile)
	return path
}

func clearGraphShadow(runDir string) error {
	return clearShadowProjection(runDir, graphShadowFile)
}
