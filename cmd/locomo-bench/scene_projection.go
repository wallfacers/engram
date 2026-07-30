package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// narrowProjectionConfig keeps Scene, Profile, and graph experiments in one
// arm per run. Each mechanism gets its own residual cohort, artifact, and
// verdict; combinations are not evidence for any individual projection.
type narrowProjectionArm string

const (
	narrowProjectionArmNone    narrowProjectionArm = ""
	narrowProjectionArmScene   narrowProjectionArm = "scene-cross-session"
	narrowProjectionArmProfile narrowProjectionArm = "profile-current-state"
	narrowProjectionArmGraph   narrowProjectionArm = "graph-missing-bridge"
)

type narrowProjectionConfig struct {
	Scene   bool
	Profile bool
	Graph   bool
}

func (config narrowProjectionConfig) arm() (narrowProjectionArm, error) {
	selected := make([]narrowProjectionArm, 0, 3)
	if config.Scene {
		selected = append(selected, narrowProjectionArmScene)
	}
	if config.Profile {
		selected = append(selected, narrowProjectionArmProfile)
	}
	if config.Graph {
		selected = append(selected, narrowProjectionArmGraph)
	}
	if len(selected) == 0 {
		return narrowProjectionArmNone, nil
	}
	if len(selected) != 1 {
		return narrowProjectionArmNone, fmt.Errorf("narrow projection arms are mutually exclusive: %s", joinNarrowProjectionArms(selected))
	}
	return selected[0], nil
}

func joinNarrowProjectionArms(arms []narrowProjectionArm) string {
	values := make([]string, len(arms))
	for index, arm := range arms {
		values[index] = string(arm)
	}
	return strings.Join(values, ", ")
}

const sceneShadowFile = "scene-cross-session.json"

type sceneShadow struct {
	Metadata shadowProjectionMetadata `json:"metadata"`
	Scenes   []sceneShadowObject      `json:"scenes"`
}

type sceneShadowObject struct {
	ID           string   `json:"id"`
	SceneKey     string   `json:"scene_key"`
	CandidateIDs []string `json:"candidate_ids"`
	SourceIDs    []string `json:"source_ids"`
	SessionIDs   []string `json:"session_ids"`
}

// buildSceneShadow only materializes a group when its frozen candidates cover
// at least two source sessions. It does not search the store or promote a
// same-session summary into an artificial cross-session candidate.
func buildSceneShadow(config projectionRunConfig, candidates []evidencecompiler.Candidate) (sceneShadow, error) {
	if err := validateProjectionRunConfig(config); err != nil {
		return sceneShadow{}, err
	}
	if err := validateShadowCandidates(candidates); err != nil {
		return sceneShadow{}, err
	}
	type sceneGroup struct {
		candidates []evidencecompiler.Candidate
		sessions   map[string]struct{}
	}
	groups := make(map[string]*sceneGroup)
	for _, candidate := range candidates {
		if candidate.Metadata == nil {
			continue
		}
		key := strings.TrimSpace(candidate.Metadata["scene_key"])
		sessionID := strings.TrimSpace(candidate.Metadata["source_session_id"])
		if key == "" || sessionID == "" {
			continue
		}
		group := groups[key]
		if group == nil {
			group = &sceneGroup{sessions: make(map[string]struct{})}
			groups[key] = group
		}
		group.candidates = append(group.candidates, candidate)
		group.sessions[sessionID] = struct{}{}
	}

	keys := make([]string, 0, len(groups))
	for key, group := range groups {
		if len(group.sessions) >= 2 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	selected := make([]evidencecompiler.Candidate, 0)
	for _, key := range keys {
		selected = append(selected, groups[key].candidates...)
	}
	if len(selected) == 0 {
		return sceneShadow{}, fmt.Errorf("scene projection found no cross-session frozen candidates")
	}
	metadata, err := newShadowProjectionMetadata(string(narrowProjectionArmScene), config, selected)
	if err != nil {
		return sceneShadow{}, err
	}
	shadow := sceneShadow{Metadata: metadata, Scenes: make([]sceneShadowObject, 0, len(keys))}
	for _, key := range keys {
		group := groups[key]
		sessions := make([]string, 0, len(group.sessions))
		for sessionID := range group.sessions {
			sessions = append(sessions, sessionID)
		}
		sort.Strings(sessions)
		shadow.Scenes = append(shadow.Scenes, sceneShadowObject{
			ID:           "scene:" + key,
			SceneKey:     key,
			CandidateIDs: candidateIDsFromCandidates(group.candidates),
			SourceIDs:    candidateSourceUnion(group.candidates),
			SessionIDs:   sessions,
		})
	}
	if err := writeShadowProjection(config.RunDir, sceneShadowFile, shadow); err != nil {
		return sceneShadow{}, fmt.Errorf("write scene shadow: %w", err)
	}
	return shadow, nil
}

func sceneShadowPath(runDir string) string {
	path, _ := shadowProjectionPath(runDir, sceneShadowFile)
	return path
}

func clearSceneShadow(runDir string) error {
	return clearShadowProjection(runDir, sceneShadowFile)
}
