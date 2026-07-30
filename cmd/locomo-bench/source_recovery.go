package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// E3 is intentionally limited to formalEvidenceReader.GetMany. That interface
// has no Search/query method, so source-turn recovery cannot turn into a
// second retrieval or manufacture lineage outside frozen candidates.
type sourceRecoveryShadow struct {
	Metadata shadowProjectionMetadata `json:"metadata"`
	Sources  []recoveredSource        `json:"sources"`
}

type recoveredSource struct {
	SourceID        string   `json:"source_id"`
	SourceSessionID string   `json:"source_session_id,omitempty"`
	Speaker         string   `json:"speaker,omitempty"`
	Ordinal         int      `json:"ordinal"`
	ContentDigest   string   `json:"content_digest,omitempty"`
	CandidateIDs    []string `json:"candidate_ids"`
}

const sourceRecoveryShadowFile = "source-recovery.json"

func buildSourceRecoveryShadow(
	ctx context.Context,
	config projectionRunConfig,
	reader formalEvidenceReader,
	candidates []evidencecompiler.Candidate,
) (sourceRecoveryShadow, error) {
	if reader == nil {
		return sourceRecoveryShadow{}, fmt.Errorf("source recovery requires an Evidence reader")
	}
	metadata, err := newShadowProjectionMetadata(string(eventProjectionArmSourceRecovery), config, candidates)
	if err != nil {
		return sourceRecoveryShadow{}, err
	}
	resolved, err := reader.GetMany(ctx, metadata.SourceIDs)
	if err != nil {
		return sourceRecoveryShadow{}, fmt.Errorf("recover candidate sources: %w", err)
	}
	allowed := make(map[string]struct{}, len(metadata.SourceIDs))
	for _, sourceID := range metadata.SourceIDs {
		allowed[sourceID] = struct{}{}
	}
	for sourceID, evidence := range resolved {
		if _, ok := allowed[sourceID]; !ok {
			return sourceRecoveryShadow{}, fmt.Errorf("source recovery returned %q outside frozen lineage", sourceID)
		}
		if evidence.ID != sourceID {
			return sourceRecoveryShadow{}, fmt.Errorf("source recovery identity mismatch for %q", sourceID)
		}
		if evidence.State != memory.EvidenceActive {
			return sourceRecoveryShadow{}, fmt.Errorf("source recovery source %q is not active", sourceID)
		}
	}

	shadow := sourceRecoveryShadow{Metadata: metadata, Sources: make([]recoveredSource, 0, len(metadata.SourceIDs))}
	for _, sourceID := range metadata.SourceIDs {
		evidence, ok := resolved[sourceID]
		if !ok {
			return sourceRecoveryShadow{}, fmt.Errorf("source recovery did not resolve frozen source %q", sourceID)
		}
		shadow.Sources = append(shadow.Sources, recoveredSource{
			SourceID:        sourceID,
			SourceSessionID: evidence.SourceSessionID,
			Speaker:         evidence.Speaker,
			Ordinal:         evidence.Ordinal,
			ContentDigest:   evidence.ContentDigest,
			CandidateIDs:    sourceRecoveryCandidateIDs(sourceID, candidates),
		})
	}
	if err := writeShadowProjection(config.RunDir, sourceRecoveryShadowFile, shadow); err != nil {
		return sourceRecoveryShadow{}, fmt.Errorf("write source recovery shadow: %w", err)
	}
	return shadow, nil
}

func sourceRecoveryCandidateIDs(sourceID string, candidates []evidencecompiler.Candidate) []string {
	var candidateIDs []string
	for _, candidate := range candidates {
		for _, candidateSourceID := range candidate.SourceIDs {
			if candidateSourceID == sourceID {
				candidateIDs = append(candidateIDs, candidate.ID)
				break
			}
		}
	}
	sort.Strings(candidateIDs)
	return candidateIDs
}

func sourceRecoveryShadowPath(runDir string) string {
	path, _ := shadowProjectionPath(runDir, sourceRecoveryShadowFile)
	return path
}

func clearSourceRecoveryShadow(runDir string) error {
	return clearShadowProjection(runDir, sourceRecoveryShadowFile)
}
