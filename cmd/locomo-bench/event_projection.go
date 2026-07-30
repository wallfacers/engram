package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// eventProjectionArm is deliberately separate from the runner flags. The
// closer wires this validation into CLI parsing so an experimental result is
// always attributable to exactly one E0--E3 arm.
type eventProjectionArm string

const (
	eventProjectionArmNone           eventProjectionArm = ""
	eventProjectionArmCurrentFields  eventProjectionArm = "E0-current-fields"
	eventProjectionArmEventObject    eventProjectionArm = "E1-event-object"
	eventProjectionArmDateOperator   eventProjectionArm = "E2-date-operator"
	eventProjectionArmSourceRecovery eventProjectionArm = "E3-source-recovery"
)

type eventProjectionConfig struct {
	CurrentFields  bool
	EventObject    bool
	DateOperator   bool
	SourceRecovery bool
}

func (config eventProjectionConfig) arm() (eventProjectionArm, error) {
	selected := make([]eventProjectionArm, 0, 4)
	if config.CurrentFields {
		selected = append(selected, eventProjectionArmCurrentFields)
	}
	if config.EventObject {
		selected = append(selected, eventProjectionArmEventObject)
	}
	if config.DateOperator {
		selected = append(selected, eventProjectionArmDateOperator)
	}
	if config.SourceRecovery {
		selected = append(selected, eventProjectionArmSourceRecovery)
	}
	if len(selected) == 0 {
		return eventProjectionArmNone, nil
	}
	if len(selected) != 1 {
		return eventProjectionArmNone, fmt.Errorf("event projection arms are mutually exclusive: %s", joinProjectionArms(selected))
	}
	return selected[0], nil
}

func joinProjectionArms(arms []eventProjectionArm) string {
	values := make([]string, len(arms))
	for index, arm := range arms {
		values[index] = string(arm)
	}
	return strings.Join(values, ", ")
}

// projectionRunConfig identifies a run-directory-only shadow. It contains the
// frozen comparison inputs needed to reject an accidental budget or candidate
// change when the runner is connected later.
type projectionRunConfig struct {
	RunDir             string
	CandidateSetDigest string
	TokenCap           int
	CandidateLimit     int
}

type shadowProjectionMetadata struct {
	Schema             string   `json:"schema"`
	Arm                string   `json:"arm"`
	CandidateSetDigest string   `json:"candidate_set_digest"`
	CandidateIDs       []string `json:"candidate_ids"`
	SourceIDs          []string `json:"source_ids"`
	TokenCap           int      `json:"token_cap"`
	CandidateLimit     int      `json:"candidate_limit,omitempty"`
}

const (
	shadowProjectionSchema    = "022.v1.shadow"
	shadowProjectionDirectory = "shadow-projections"
	eventObjectShadowFile     = "event-object.json"
)

type eventObjectShadow struct {
	Metadata shadowProjectionMetadata `json:"metadata"`
	Events   []eventShadowObject      `json:"events"`
}

// eventShadowObject is an evaluation-only representation. Its source IDs are
// copied directly from the frozen candidate; it is never written to the
// product Ledger, projection registry, or graph tables.
type eventShadowObject struct {
	ID          string   `json:"id"`
	CandidateID string   `json:"candidate_id"`
	TextDigest  string   `json:"text_digest"`
	EventTime   string   `json:"event_time,omitempty"`
	SourceIDs   []string `json:"source_ids"`
}

func buildEventObjectShadow(config projectionRunConfig, candidates []evidencecompiler.Candidate) (eventObjectShadow, error) {
	metadata, err := newShadowProjectionMetadata(string(eventProjectionArmEventObject), config, candidates)
	if err != nil {
		return eventObjectShadow{}, err
	}
	shadow := eventObjectShadow{
		Metadata: metadata,
		Events:   make([]eventShadowObject, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		eventTime := ""
		if candidate.Metadata != nil {
			eventTime = strings.TrimSpace(candidate.Metadata["event_time"])
		}
		shadow.Events = append(shadow.Events, eventShadowObject{
			ID:          "event:" + candidate.ID,
			CandidateID: candidate.ID,
			TextDigest:  candidate.TextDigest,
			EventTime:   eventTime,
			SourceIDs:   cloneStrings(candidate.SourceIDs),
		})
	}
	if err := writeShadowProjection(config.RunDir, eventObjectShadowFile, shadow); err != nil {
		return eventObjectShadow{}, fmt.Errorf("write event object shadow: %w", err)
	}
	return shadow, nil
}

func eventObjectShadowPath(runDir string) string {
	path, _ := shadowProjectionPath(runDir, eventObjectShadowFile)
	return path
}

func clearEventObjectShadow(runDir string) error {
	return clearShadowProjection(runDir, eventObjectShadowFile)
}

func newShadowProjectionMetadata(arm string, config projectionRunConfig, candidates []evidencecompiler.Candidate) (shadowProjectionMetadata, error) {
	if err := validateProjectionRunConfig(config); err != nil {
		return shadowProjectionMetadata{}, err
	}
	if err := validateShadowCandidates(candidates); err != nil {
		return shadowProjectionMetadata{}, err
	}
	if config.CandidateLimit > 0 && len(candidates) > config.CandidateLimit {
		return shadowProjectionMetadata{}, fmt.Errorf("shadow candidate count %d exceeds frozen limit %d", len(candidates), config.CandidateLimit)
	}
	return shadowProjectionMetadata{
		Schema:             shadowProjectionSchema,
		Arm:                arm,
		CandidateSetDigest: config.CandidateSetDigest,
		CandidateIDs:       candidateIDsFromCandidates(candidates),
		SourceIDs:          candidateSourceUnion(candidates),
		TokenCap:           config.TokenCap,
		CandidateLimit:     config.CandidateLimit,
	}, nil
}

func validateProjectionRunConfig(config projectionRunConfig) error {
	if strings.TrimSpace(config.RunDir) == "" {
		return fmt.Errorf("shadow projection requires run directory")
	}
	if strings.TrimSpace(config.CandidateSetDigest) == "" {
		return fmt.Errorf("shadow projection requires frozen candidate-set digest")
	}
	if config.TokenCap <= 0 {
		return fmt.Errorf("shadow projection requires positive token cap")
	}
	if config.CandidateLimit < 0 {
		return fmt.Errorf("shadow projection candidate limit cannot be negative")
	}
	return nil
}

func validateShadowCandidates(candidates []evidencecompiler.Candidate) error {
	if len(candidates) == 0 {
		return fmt.Errorf("shadow projection requires frozen candidates")
	}
	seenCandidates := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ID) == "" {
			return fmt.Errorf("shadow candidate has empty ID")
		}
		if _, exists := seenCandidates[candidate.ID]; exists {
			return fmt.Errorf("shadow candidate %q is duplicated", candidate.ID)
		}
		seenCandidates[candidate.ID] = struct{}{}
		if len(candidate.SourceIDs) == 0 {
			return fmt.Errorf("shadow candidate %q has no source lineage", candidate.ID)
		}
		seenSources := make(map[string]struct{}, len(candidate.SourceIDs))
		for _, sourceID := range candidate.SourceIDs {
			if strings.TrimSpace(sourceID) == "" {
				return fmt.Errorf("shadow candidate %q has empty source ID", candidate.ID)
			}
			if _, exists := seenSources[sourceID]; exists {
				return fmt.Errorf("shadow candidate %q repeats source ID %q", candidate.ID, sourceID)
			}
			seenSources[sourceID] = struct{}{}
		}
	}
	return nil
}

func candidateIDsFromCandidates(candidates []evidencecompiler.Candidate) []string {
	ids := make([]string, len(candidates))
	for index, candidate := range candidates {
		ids[index] = candidate.ID
	}
	return ids
}

func candidateSourceUnion(candidates []evidencecompiler.Candidate) []string {
	seen := make(map[string]struct{})
	var sourceIDs []string
	for _, candidate := range candidates {
		for _, sourceID := range candidate.SourceIDs {
			if _, exists := seen[sourceID]; exists {
				continue
			}
			seen[sourceID] = struct{}{}
			sourceIDs = append(sourceIDs, sourceID)
		}
	}
	sort.Strings(sourceIDs)
	return sourceIDs
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func cloneCandidates(candidates []evidencecompiler.Candidate) []evidencecompiler.Candidate {
	cloned := make([]evidencecompiler.Candidate, len(candidates))
	for index, candidate := range candidates {
		cloned[index] = candidate
		cloned[index].SourceIDs = cloneStrings(candidate.SourceIDs)
		if candidate.Metadata != nil {
			cloned[index].Metadata = make(map[string]string, len(candidate.Metadata))
			for key, value := range candidate.Metadata {
				cloned[index].Metadata[key] = value
			}
		}
	}
	return cloned
}

func shadowProjectionPath(runDir, fileName string) (string, error) {
	if strings.TrimSpace(runDir) == "" {
		return "", fmt.Errorf("shadow projection requires run directory")
	}
	if filepath.Base(fileName) != fileName || fileName == "." || fileName == "" {
		return "", fmt.Errorf("invalid shadow projection file %q", fileName)
	}
	root, err := filepath.Abs(filepath.Clean(runDir))
	if err != nil {
		return "", fmt.Errorf("resolve run directory: %w", err)
	}
	path := filepath.Join(root, shadowProjectionDirectory, fileName)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("shadow projection path escapes run directory")
	}
	return path, nil
}

func writeShadowProjection(runDir, fileName string, value any) error {
	path, err := shadowProjectionPath(runDir, fileName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create shadow projection directory: %w", err)
	}
	return writeJSON(path, value)
}

func clearShadowProjection(runDir, fileName string) error {
	path, err := shadowProjectionPath(runDir, fileName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear shadow projection %q: %w", fileName, err)
	}
	return nil
}
