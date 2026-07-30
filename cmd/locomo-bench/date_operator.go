package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/wallfacers/engram/memory/evidencecompiler"
)

// dateOperator is an E2 annotation over the already frozen candidate list. It
// intentionally never filters, retrieves, re-ranks, or appends candidates;
// callers can render its deterministic matches while preserving the same
// candidate/cap artifact as E0/E1/E3.
type dateOperatorKind string

const (
	dateOperatorBefore  dateOperatorKind = "before"
	dateOperatorAfter   dateOperatorKind = "after"
	dateOperatorBetween dateOperatorKind = "between"
	dateOperatorLatest  dateOperatorKind = "latest"
)

type dateOperator struct {
	Kind  dateOperatorKind
	Start *time.Time
	End   *time.Time
}

type dateOperatorShadow struct {
	Metadata   shadowProjectionMetadata `json:"metadata"`
	Operator   dateOperatorRecord       `json:"operator"`
	Candidates []dateOperatorCandidate  `json:"candidates"`
}

type dateOperatorRecord struct {
	Kind  dateOperatorKind `json:"kind"`
	Start string           `json:"start,omitempty"`
	End   string           `json:"end,omitempty"`
}

type dateOperatorCandidate struct {
	CandidateID string   `json:"candidate_id"`
	EventTime   string   `json:"event_time,omitempty"`
	Matches     bool     `json:"matches"`
	SourceIDs   []string `json:"source_ids"`
}

const dateOperatorShadowFile = "date-operator.json"

func applyDateOperatorShadow(config projectionRunConfig, operator dateOperator, candidates []evidencecompiler.Candidate) (dateOperatorShadow, error) {
	if err := validateDateOperator(operator); err != nil {
		return dateOperatorShadow{}, err
	}
	metadata, err := newShadowProjectionMetadata(string(eventProjectionArmDateOperator), config, candidates)
	if err != nil {
		return dateOperatorShadow{}, err
	}
	shadow := dateOperatorShadow{
		Metadata: metadata,
		Operator: dateOperatorRecord{
			Kind:  operator.Kind,
			Start: dateOperatorTimeString(operator.Start),
			End:   dateOperatorTimeString(operator.End),
		},
		Candidates: make([]dateOperatorCandidate, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		eventTime, eventTimeText, known := candidateEventTime(candidate)
		shadow.Candidates = append(shadow.Candidates, dateOperatorCandidate{
			CandidateID: candidate.ID,
			EventTime:   eventTimeText,
			Matches:     known && dateOperatorMatches(operator, eventTime),
			SourceIDs:   cloneStrings(candidate.SourceIDs),
		})
	}
	if operator.Kind == dateOperatorLatest {
		markLatestDateOperatorCandidate(shadow.Candidates)
	}
	if err := writeShadowProjection(config.RunDir, dateOperatorShadowFile, shadow); err != nil {
		return dateOperatorShadow{}, fmt.Errorf("write date operator shadow: %w", err)
	}
	return shadow, nil
}

func validateDateOperator(operator dateOperator) error {
	switch operator.Kind {
	case dateOperatorBefore:
		if operator.End == nil {
			return fmt.Errorf("before date operator requires end")
		}
	case dateOperatorAfter:
		if operator.Start == nil {
			return fmt.Errorf("after date operator requires start")
		}
	case dateOperatorBetween:
		if operator.Start == nil || operator.End == nil {
			return fmt.Errorf("between date operator requires start and end")
		}
		if operator.End.Before(*operator.Start) {
			return fmt.Errorf("between date operator end precedes start")
		}
	case dateOperatorLatest:
		if operator.Start != nil || operator.End != nil {
			return fmt.Errorf("latest date operator does not accept bounds")
		}
	default:
		return fmt.Errorf("unknown date operator %q", operator.Kind)
	}
	return nil
}

func candidateEventTime(candidate evidencecompiler.Candidate) (time.Time, string, bool) {
	if candidate.Metadata == nil {
		return time.Time{}, "", false
	}
	value := strings.TrimSpace(candidate.Metadata["event_time"])
	if value == "" {
		value = strings.TrimSpace(candidate.Metadata["event_date"])
	}
	if value == "" {
		return time.Time{}, "", false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, value, false
	}
	return parsed.UTC(), parsed.UTC().Format(time.RFC3339), true
}

func dateOperatorMatches(operator dateOperator, eventTime time.Time) bool {
	switch operator.Kind {
	case dateOperatorBefore:
		return !eventTime.After(operator.End.UTC())
	case dateOperatorAfter:
		return !eventTime.Before(operator.Start.UTC())
	case dateOperatorBetween:
		return !eventTime.Before(operator.Start.UTC()) && !eventTime.After(operator.End.UTC())
	case dateOperatorLatest:
		return false // marked in a deterministic second pass below
	default:
		return false
	}
}

func markLatestDateOperatorCandidate(candidates []dateOperatorCandidate) {
	latestIndex := -1
	var latest time.Time
	for index, candidate := range candidates {
		if candidate.EventTime == "" {
			continue
		}
		eventTime, err := time.Parse(time.RFC3339, candidate.EventTime)
		if err != nil {
			continue
		}
		if latestIndex < 0 || eventTime.After(latest) || (eventTime.Equal(latest) && candidate.CandidateID < candidates[latestIndex].CandidateID) {
			latestIndex, latest = index, eventTime
		}
	}
	if latestIndex >= 0 {
		candidates[latestIndex].Matches = true
	}
}

func dateOperatorTimeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func dateOperatorShadowPath(runDir string) string {
	path, _ := shadowProjectionPath(runDir, dateOperatorShadowFile)
	return path
}

func clearDateOperatorShadow(runDir string) error {
	return clearShadowProjection(runDir, dateOperatorShadowFile)
}
