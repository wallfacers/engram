// Package eventstore implements the write-side event projection (027): a
// rebuildable view over Evidence that organizes conversation into temporally
// anchored events with dual-perspective entries (facts + relations). It is a
// research experiment, off by default; a nil/unconfigured store degrades to the
// existing chunk path with zero behavior change.
package eventstore

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// RelationType enumerates the supported relation perspectives (data-model.md).
type RelationType string

const (
	RelationInterpersonal   RelationType = "interpersonal"
	RelationCausal          RelationType = "causal"
	RelationCoParticipation RelationType = "co_participation"
	RelationTemporalOrder   RelationType = "temporal_order"
	RelationPreference      RelationType = "preference"
)

// validRelationTypes is the closed set used by the schema validator
// (contracts/event-contract.md rule 6).
var validRelationTypes = map[string]bool{
	string(RelationInterpersonal):   true,
	string(RelationCausal):          true,
	string(RelationCoParticipation): true,
	string(RelationTemporalOrder):   true,
	string(RelationPreference):      true,
}

// Text budget bounds (contracts/event-contract.md). Kept here so the engine
// stays self-contained; the eval harness may override via config.
const (
	// MaxTextRunes caps a single fact/relation text field.
	MaxTextRunes = 500
	// MaxTotalRunes caps the whole event JSON payload.
	MaxTotalRunes = 2000
)

// FactEntry is one factual-perspective entry: what happened.
type FactEntry struct {
	Text     string `json:"text"`
	Grounded bool   `json:"grounded"`
}

// RelationEntry is one relational-perspective entry: who/why/co-participation.
type RelationEntry struct {
	RelationType string `json:"relation_type"`
	Subject      string `json:"subject"`
	Object       string `json:"object"`
	Text         string `json:"text"`
}

// Event is one temporally anchored, dual-perspective event unit.
type Event struct {
	ID              string           `json:"event_id"`
	ConversationID  string           `json:"conversation_id"`
	SourceLedgerIDs []string         `json:"source_ledger_ids"`
	Speaker         string           `json:"speaker"`
	FactEntries     []FactEntry      `json:"fact_entries"`
	RelationEntries []RelationEntry  `json:"relation_entries"`
	AbsoluteTS      string           `json:"absolute_ts,omitempty"`
	RelativeRef     string           `json:"relative_ref,omitempty"`
}

// RelationSummary is the derived cross-event consolidation (data-model.md). It
// is a pure projection and never persisted independently.
type RelationSummary struct {
	SummaryID      string   `json:"summary_id"`
	EventIDs       []string `json:"event_ids"`
	WindowStartTS  string   `json:"window_start_ts,omitempty"`
	WindowEndTS    string   `json:"window_end_ts,omitempty"`
	Text           string   `json:"text"`
	GroundedEvents int      `json:"grounded_events"`
}

// ErrInvalidEvent is returned when an extraction output fails schema validation.
var ErrInvalidEvent = fmt.Errorf("eventstore: invalid event")

// Validate parses and schema-validates one extraction output (contracts/
// event-contract.md). A nil or empty output, malformed JSON, a missing required
// field, an unknown relation_type, or an over-budget payload all fail closed:
// the caller MUST NOT persist the event and MUST fall back to the raw chunk
// path. Validate is pure and does not consult the ledger; source-existence
// checking is the caller's responsibility (extract.go).
func Validate(raw []byte) (*Event, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty output", ErrInvalidEvent)
	}
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidEvent, err)
	}
	if err := ev.validate(); err != nil {
		return nil, err
	}
	return &ev, nil
}

// ValidateLenient is the extraction-path validator. It parses an event and, if
// a relation entry carries a relation_type outside the closed set (local 7B
// sidecars often invent one), DROPS that relation entry instead of failing the
// whole event — the factual core stays usable and the event stays well-formed.
// All other schema rules are enforced identically to Validate.
func ValidateLenient(raw []byte) (*Event, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty output", ErrInvalidEvent)
	}
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidEvent, err)
	}
	kept := ev.RelationEntries[:0]
	for _, r := range ev.RelationEntries {
		if validRelationTypes[r.RelationType] {
			kept = append(kept, r)
		}
	}
	ev.RelationEntries = kept
	if err := ev.validate(); err != nil {
		return nil, err
	}
	return &ev, nil
}

func (e *Event) validate() error {
	if strings.TrimSpace(e.ConversationID) == "" {
		return fmt.Errorf("%w: empty conversation_id", ErrInvalidEvent)
	}
	if len(e.SourceLedgerIDs) == 0 {
		return fmt.Errorf("%w: empty source_ledger_ids", ErrInvalidEvent)
	}
	for _, id := range e.SourceLedgerIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: blank source_ledger_ids entry", ErrInvalidEvent)
		}
	}
	if strings.TrimSpace(e.Speaker) == "" {
		return fmt.Errorf("%w: empty speaker", ErrInvalidEvent)
	}
	if len(e.FactEntries) == 0 {
		return fmt.Errorf("%w: empty fact_entries", ErrInvalidEvent)
	}
	total := 0
	for _, f := range e.FactEntries {
		n := utf8.RuneCountInString(f.Text)
		if n == 0 {
			return fmt.Errorf("%w: empty fact text", ErrInvalidEvent)
		}
		if n > MaxTextRunes {
			return fmt.Errorf("%w: fact text over %d runes", ErrInvalidEvent, MaxTextRunes)
		}
		total += n
	}
	for _, r := range e.RelationEntries {
		if !validRelationTypes[r.RelationType] {
			return fmt.Errorf("%w: unknown relation_type %q", ErrInvalidEvent, r.RelationType)
		}
		if strings.TrimSpace(r.Subject) == "" || strings.TrimSpace(r.Object) == "" || strings.TrimSpace(r.Text) == "" {
			return fmt.Errorf("%w: relation entry missing subject/object/text", ErrInvalidEvent)
		}
		n := utf8.RuneCountInString(r.Text)
		if n > MaxTextRunes {
			return fmt.Errorf("%w: relation text over %d runes", ErrInvalidEvent, MaxTextRunes)
		}
		total += n + utf8.RuneCountInString(r.Subject) + utf8.RuneCountInString(r.Object)
	}
	if total > MaxTotalRunes {
		return fmt.Errorf("%w: event payload over %d runes", ErrInvalidEvent, MaxTotalRunes)
	}
	return nil
}
