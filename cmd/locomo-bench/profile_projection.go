package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

const profileShadowFile = "profile-current-state.json"

type profileShadow struct {
	Metadata shadowProjectionMetadata `json:"metadata"`
	Profiles []profileShadowObject    `json:"profiles"`
}

type profileShadowObject struct {
	Subject                 string   `json:"subject"`
	PreferenceCandidateIDs  []string `json:"preference_candidate_ids,omitempty"`
	CurrentStateCandidateID string   `json:"current_state_candidate_id,omitempty"`
	SourceIDs               []string `json:"source_ids"`
}

// buildProfileShadow admits only explicitly classified preference/current-state
// candidates. It is intentionally not a biography or generic user-summary
// store, and its payload exists only under the evaluation run directory.
func buildProfileShadow(config projectionRunConfig, candidates []evidencecompiler.Candidate) (profileShadow, error) {
	if err := validateProjectionRunConfig(config); err != nil {
		return profileShadow{}, err
	}
	if err := validateShadowCandidates(candidates); err != nil {
		return profileShadow{}, err
	}
	type subjectGroup struct {
		preference []evidencecompiler.Candidate
		current    []evidencecompiler.Candidate
	}
	groups := make(map[string]*subjectGroup)
	var selected []evidencecompiler.Candidate
	for _, candidate := range candidates {
		if candidate.Metadata == nil {
			continue
		}
		subject := strings.TrimSpace(candidate.Metadata["profile_subject"])
		kind := strings.TrimSpace(candidate.Metadata["profile_kind"])
		if subject == "" || (kind != "preference" && kind != "current_state") {
			continue
		}
		group := groups[subject]
		if group == nil {
			group = &subjectGroup{}
			groups[subject] = group
		}
		if kind == "preference" {
			group.preference = append(group.preference, candidate)
		} else {
			group.current = append(group.current, candidate)
		}
		selected = append(selected, candidate)
	}
	if len(selected) == 0 {
		return profileShadow{}, fmt.Errorf("profile projection found no preference/current-state frozen candidates")
	}
	metadata, err := newShadowProjectionMetadata(string(narrowProjectionArmProfile), config, selected)
	if err != nil {
		return profileShadow{}, err
	}
	subjects := make([]string, 0, len(groups))
	for subject := range groups {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	shadow := profileShadow{Metadata: metadata, Profiles: make([]profileShadowObject, 0, len(subjects))}
	for _, subject := range subjects {
		group := groups[subject]
		all := append(cloneCandidates(group.preference), cloneCandidates(group.current)...)
		profile := profileShadowObject{
			Subject:                subject,
			PreferenceCandidateIDs: candidateIDsFromCandidates(group.preference),
			SourceIDs:              candidateSourceUnion(all),
		}
		if current, ok := newestCurrentStateCandidate(group.current); ok {
			profile.CurrentStateCandidateID = current.ID
		}
		shadow.Profiles = append(shadow.Profiles, profile)
	}
	if err := writeShadowProjection(config.RunDir, profileShadowFile, shadow); err != nil {
		return profileShadow{}, fmt.Errorf("write profile shadow: %w", err)
	}
	return shadow, nil
}

func newestCurrentStateCandidate(candidates []evidencecompiler.Candidate) (evidencecompiler.Candidate, bool) {
	if len(candidates) == 0 {
		return evidencecompiler.Candidate{}, false
	}
	best := candidates[0]
	bestTime, _, bestKnown := candidateEventTime(best)
	for _, candidate := range candidates[1:] {
		candidateTime, _, candidateKnown := candidateEventTime(candidate)
		if !bestKnown && candidateKnown ||
			(candidateKnown && bestKnown && (candidateTime.After(bestTime) || (candidateTime.Equal(bestTime) && profileCandidateBefore(candidate, best)))) ||
			(!candidateKnown && !bestKnown && profileCandidateBefore(candidate, best)) {
			best, bestTime, bestKnown = candidate, candidateTime, candidateKnown
		}
	}
	return best, true
}

func profileCandidateBefore(left, right evidencecompiler.Candidate) bool {
	if left.Rank != right.Rank {
		return left.Rank < right.Rank
	}
	return left.ID < right.ID
}

func profileShadowPath(runDir string) string {
	path, _ := shadowProjectionPath(runDir, profileShadowFile)
	return path
}

func clearProfileShadow(runDir string) error {
	return clearShadowProjection(runDir, profileShadowFile)
}
