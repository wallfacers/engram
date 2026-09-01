package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// 048 holdout review/admission type layer (data-model.md §3–§6).

// AuthoringPromptReceipt binds one frozen prompt version + digest.
type AuthoringPromptReceipt struct {
	PromptID         string `json:"prompt_id"`
	Version          int    `json:"version"`
	DigestAlgorithm  string `json:"digest_algorithm"`
	SHA256           string `json:"sha256"`
	QuotaPlanDigest  string `json:"quota_plan_digest"`
}

// AuthoringReceipt is the private per-candidate authoring record. It never
// enters a review envelope.
type AuthoringReceipt struct {
	AttemptID                string               `json:"attempt_id"`
	BatchID                  string               `json:"batch_id"`
	QuotaSlot                string               `json:"quota_slot"`
	QuotaSlotDigest          string               `json:"quota_slot_digest"`
	Author                   ToolProvenance       `json:"author"`
	Prompt                   AuthoringPromptReceipt `json:"prompt"`
	CandidateTranscriptDigest string              `json:"candidate_transcript_digest"`
	PrivateCandidateDigest   string               `json:"private_candidate_digest"`
	BlindCandidateDigest     string               `json:"blind_candidate_digest"`
	AttemptOrdinal           int                  `json:"attempt_ordinal"`
	ReceiptDigest            string               `json:"receipt_digest"`
}

// BlindCandidateV1 is the recursively closed reviewer-visible projection.
type BlindCandidateV1 struct {
	SchemaVersion  string              `json:"schema_version"`
	Prompt         *string             `json:"prompt"`
	Turns          []BlindTurn         `json:"turns"`
	SeedMemories   []BlindSeedMemory   `json:"seed_memories"`
	WorkspaceFiles []BlindWorkspaceFile `json:"workspace_files"`
}

type BlindTurn struct {
	Session   int    `json:"session"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	SetupOnly bool   `json:"setup_only,omitempty"`
}

type BlindSeedMemory struct {
	Name      string  `json:"name"`
	Content   string  `json:"content"`
	EventDate *string `json:"event_date"`
}

type BlindWorkspaceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

// BuildBlindCandidate projects a validated case onto its de-labeled review
// form: only prompt/turns, seed memories and path-sorted workspace files
// survive; every identity/label/slot/provenance field is dropped here, and the
// closed schema guarantees none can be re-added.
func BuildBlindCandidate(c *TriggerCaseV2) *BlindCandidateV1 {
	bc := &BlindCandidateV1{SchemaVersion: "blind-candidate-v1"}
	if c.Prompt != nil {
		p := *c.Prompt
		bc.Prompt = &p
	}
	for _, t := range c.Turns {
		bc.Turns = append(bc.Turns, BlindTurn{Session: t.Session, Role: t.Role, Content: t.Content, SetupOnly: t.SetupOnly})
	}
	for _, s := range c.SeedMemories {
		bc.SeedMemories = append(bc.SeedMemories, BlindSeedMemory{Name: s.Name, Content: s.Content, EventDate: s.EventDate})
	}
	for _, f := range c.WorkspaceFiles {
		bc.WorkspaceFiles = append(bc.WorkspaceFiles, BlindWorkspaceFile{Path: f.Path, Content: f.Content, SHA256: f.SHA256})
	}
	sort.Slice(bc.WorkspaceFiles, func(i, j int) bool { return bc.WorkspaceFiles[i].Path < bc.WorkspaceFiles[j].Path })
	return bc
}

// BlindCandidateDigest is the reviewer-visible candidate digest: canonical
// bytes of the exact validated BlindCandidateV1. Two private candidates with
// identical blind projections always produce the same digest.
func BlindCandidateDigest(bc *BlindCandidateV1) (string, error) { return CanonicalSHA256(bc) }

// ReviewEnvelope is the transient anonymous object sent to each reviewer.
type ReviewEnvelope struct {
	Candidate                        BlindCandidateV1    `json:"candidate"`
	BlindCandidateDigest             string              `json:"blind_candidate_digest"`
	ReviewPromptDigest               string              `json:"review_prompt_digest"`
	DevFamilySummary                 FamilySummaryPayload `json:"dev_family_summary"`
	DevFamilySummaryDigest           string              `json:"dev_family_summary_digest"`
	AcceptedHoldoutFamilySummary     *FamilySummaryPayload `json:"accepted_holdout_family_summary,omitempty"`
	AcceptedHoldoutFamilySummaryDigest *string           `json:"accepted_holdout_family_summary_digest,omitempty"`
	AcceptedHoldoutFamilyRevision    *int                `json:"accepted_holdout_family_revision,omitempty"`
	EnvelopeDigest                   string              `json:"envelope_digest"`
}

// ReviewRecord is one reviewer's full output (both reviewers for a candidate).
type ReviewRecord struct {
	AttemptID              string               `json:"attempt_id"`
	AuthorAttemptID        string               `json:"author_attempt_id"`
	ReviewID               string               `json:"review_id"`
	ReviewEnvelopeDigest   string               `json:"review_envelope_digest"`
	BlindCandidateDigest   string               `json:"blind_candidate_digest"`
	Reviewer               ToolProvenance       `json:"reviewer"`
	ReviewPrompt           AuthoringPromptReceipt `json:"review_prompt"`
	Verdict                string               `json:"verdict"` // accept | reject
	InferredModule         string               `json:"inferred_module"`
	InferredLang           string               `json:"inferred_lang"`
	InferredScenarioBucket string               `json:"inferred_scenario_bucket"`
	InferredCategory       string               `json:"inferred_category"`
	InferredExpect         ExpectV2             `json:"inferred_expect"`
	NormalizedLabelDigest  string               `json:"normalized_label_digest"`
	Novel                  bool                 `json:"novel"`
	NearestFamilyID        *string              `json:"nearest_family_id"`
	NearestFamilyScope     *string              `json:"nearest_family_scope"` // dev-regression | holdout-accepted
	ReasonCode             string               `json:"reason_code"`
	ReviewedAt             string               `json:"reviewed_at"`
	ReceiptDigest          string               `json:"receipt_digest"`
}

// NormalizedLabelDigest recomputes the controller label digest from all
// inferred dimensions and every machine field of the expect block (the
// human-only `observable` is excluded). Two reviewers must match exactly.
func NormalizedLabelDigest(module, lang, scenario, category string, expect ExpectV2) (string, error) {
	proj := struct {
		Module      string   `json:"module"`
		Lang        string   `json:"lang"`
		Scenario    string   `json:"scenario"`
		Category    string   `json:"category"`
		Trigger     bool     `json:"trigger"`
		AllowedOps  []string `json:"allowed_ops,omitempty"`
		MinCalls    *int     `json:"min_calls,omitempty"`
		MaxCalls    *int     `json:"max_calls,omitempty"`
		StoreInclude  []Alternation `json:"store_include,omitempty"`
		StoreExclude  []string      `json:"store_exclude,omitempty"`
		AnswerInclude []Alternation `json:"answer_include,omitempty"`
		AnswerExclude []string      `json:"answer_exclude,omitempty"`
		NotFound    bool     `json:"not_found,omitempty"`
	}{
		Module: module, Lang: lang, Scenario: scenario, Category: category,
		Trigger: expect.Trigger, AllowedOps: expect.AllowedOps,
		MinCalls: expect.MinCalls, MaxCalls: expect.MaxCalls,
		StoreInclude: expect.StoreInclude, StoreExclude: expect.StoreExclude,
		AnswerInclude: expect.AnswerInclude, AnswerExclude: expect.AnswerExclude,
		NotFound: expect.NotFound,
	}
	return CanonicalSHA256(proj)
}

// ReviewersAgree reports whether two review records agree on every inferred
// dimension and label digest (controller compares against the private author
// candidate separately).
func ReviewersAgree(a, b ReviewRecord) error {
	if a.NormalizedLabelDigest == "" || a.NormalizedLabelDigest != b.NormalizedLabelDigest {
		return errors.New("reviewers disagree on the normalized label digest")
	}
	if a.InferredModule != b.InferredModule || a.InferredLang != b.InferredLang ||
		a.InferredScenarioBucket != b.InferredScenarioBucket || a.InferredCategory != b.InferredCategory {
		return errors.New("reviewers disagree on inferred dimensions")
	}
	return nil
}

// ---------- family summary payloads ----------

const BlindFamilySummaryProjection = "blind-family-summary-v1"

// FamilySummaryEntry is one anonymous family projection.
type FamilySummaryEntry struct {
	FamilyID             string   `json:"family_id"`
	LanguageMembers      []string `json:"language_members"`
	BlindSemanticPayloads []string `json:"blind_semantic_payloads"`
	EntryDigest          string   `json:"entry_digest"`
}

// FamilySummaryPayload is the anonymous, label-free novelty-review input. A
// digest-only envelope is invalid: reviewers receive the materialized payload.
type FamilySummaryPayload struct {
	SchemaVersion      int                   `json:"schema_version"`
	Scope              string                `json:"scope"` // dev-regression | holdout-accepted
	Revision           int                   `json:"revision"`
	ProjectionVersion  string                `json:"projection_version"`
	SourceStateDigest  string                `json:"source_state_digest"`
	SourceFamilyCount  int                   `json:"source_family_count"`
	Entries            []FamilySummaryEntry  `json:"entries"`
	EntriesRootDigest  string                `json:"entries_root_digest"`
	PayloadDigest      string                `json:"payload_digest"`
}

// EntriesRootDigest computes the ordered concatenation root over entry digests.
func EntriesRootDigest(entries []FamilySummaryEntry) string {
	h := ""
	_ = h
	var buf strings.Builder
	for _, e := range entries {
		buf.WriteString(e.EntryDigest)
	}
	return sha256Hex([]byte(buf.String()))
}

// ComputePayloadDigest seals the summary (excluding its own digest field via
// omitempty at the call site — here we zero it explicitly).
func (p *FamilySummaryPayload) ComputePayloadDigest() (string, error) {
	saved := p.PayloadDigest
	p.PayloadDigest = ""
	d, err := CanonicalSHA256(p)
	p.PayloadDigest = saved
	return d, err
}

// ReprojectFamilySummary rebuilds the summary from the full source state and
// verifies a one-to-one family/entry mapping plus every digest.
func ReprojectFamilySummary(p *FamilySummaryPayload, sourceFamilies map[string]FamilySummaryEntry, sourceStateDigest string) error {
	if p.SchemaVersion != 1 || p.ProjectionVersion != BlindFamilySummaryProjection {
		return fmt.Errorf("summary schema %d/projection %q invalid", p.SchemaVersion, p.ProjectionVersion)
	}
	if p.Scope != "dev-regression" && p.Scope != "holdout-accepted" {
		return fmt.Errorf("summary scope %q invalid", p.Scope)
	}
	if p.SourceStateDigest != sourceStateDigest {
		return fmt.Errorf("summary source_state_digest %q != source %q", p.SourceStateDigest, sourceStateDigest)
	}
	if p.SourceFamilyCount != len(sourceFamilies) {
		return fmt.Errorf("summary family count %d != source %d", p.SourceFamilyCount, len(sourceFamilies))
	}
	if len(p.Entries) != len(sourceFamilies) {
		return fmt.Errorf("summary entries %d != source families %d", len(p.Entries), len(sourceFamilies))
	}
	seen := map[string]bool{}
	for _, e := range p.Entries {
		src, ok := sourceFamilies[e.FamilyID]
		if !ok {
			return fmt.Errorf("summary entry %q has no source family", e.FamilyID)
		}
		if seen[e.FamilyID] {
			return fmt.Errorf("summary entry %q duplicated", e.FamilyID)
		}
		seen[e.FamilyID] = true
		want := sha256Hex([]byte(strings.Join([]string{
			e.FamilyID,
			joinSorted(e.LanguageMembers),
			joinSorted(e.BlindSemanticPayloads),
		}, "\x00")))
		if want != e.EntryDigest {
			return fmt.Errorf("summary entry %q entry_digest mismatch", e.FamilyID)
		}
		if joinSorted(src.LanguageMembers) != joinSorted(e.LanguageMembers) ||
			joinSorted(src.BlindSemanticPayloads) != joinSorted(e.BlindSemanticPayloads) {
			return fmt.Errorf("summary entry %q payload does not match source projection", e.FamilyID)
		}
	}
	if p.EntriesRootDigest != EntriesRootDigest(p.Entries) {
		return errors.New("summary entries_root_digest mismatch")
	}
	d, err := p.ComputePayloadDigest()
	if err != nil {
		return err
	}
	if d != p.PayloadDigest {
		return errors.New("summary payload_digest mismatch (post-hoc mutation or self-reference)")
	}
	return nil
}

func joinSorted(s []string) string {
	c := append([]string{}, s...)
	sort.Strings(c)
	return strings.Join(c, "\x1f")
}

// ---------- admission CAS chain ----------

// AdmissionReceipt is one immutable CAS attempt on the accepted-family state.
type AdmissionReceipt struct {
	AdmissionSequence             int     `json:"admission_sequence"`
	PreviousAdmissionReceiptDigest *string `json:"previous_admission_receipt_digest"`
	AuthorAttemptID               string  `json:"author_attempt_id"`
	AuthoringReceiptDigest        string  `json:"authoring_receipt_digest"`
	ReviewAttemptIDs              []string `json:"review_attempt_ids"`
	ReviewRecordDigests           []string `json:"review_record_digests"`
	PrivateCandidateDigest        string  `json:"private_candidate_digest"`
	BlindCandidateDigest          string  `json:"blind_candidate_digest"`
	QuotaSlotDigest               string  `json:"quota_slot_digest"`
	NormalizedLabelDigest         string  `json:"normalized_label_digest"`
	ReviewedSummaryRevision       int     `json:"reviewed_summary_revision"`
	ReviewedSummaryDigest         string  `json:"reviewed_summary_digest"`
	ReviewedSourceStateDigest     string  `json:"reviewed_source_state_digest"`
	ObservedPreRevision           int     `json:"observed_pre_revision"`
	ObservedPreStateDigest        string  `json:"observed_pre_state_digest"`
	CASResult                     string  `json:"cas_result"` // committed | stale
	FinalCaseID                   *string `json:"final_case_id"`
	ControllerFamilyID            *string `json:"controller_family_id"`
	FamilyEntryDigest             *string `json:"family_entry_digest"`
	PostRevision                  int     `json:"post_revision"`
	PostStateDigest               string  `json:"post_state_digest"`
	ReceiptDigest                 string  `json:"receipt_digest"`
}

// ---------- stage isolation + attempt ledger ----------

// AuthorReviewIsolationReceipt is the per-attempt private process-boundary
// proof (closed probes, controller target proofs, ephemeral state root).
type AuthorReviewIsolationReceipt struct {
	Stage                     string `json:"stage"` // author | review
	Host                      string `json:"host"`
	AttemptID                 string `json:"attempt_id"`
	InputDigest               string `json:"input_digest"`
	ChildIdentityDigest       string `json:"child_identity_digest"`
	ExecutionTemplateDigest   string `json:"execution_template_digest"`
	StageIsolationConfigDigest string `json:"stage_isolation_config_digest"`
	EphemeralStateRootDigest  string `json:"ephemeral_state_root_digest"`
	OwnWorkspaceDigest        string `json:"own_workspace_digest"`
	ProbeMatrixDigest         string `json:"probe_matrix_digest"`
	ReceiptDigest             string `json:"receipt_digest"`
}

// AuthorReviewAttemptLedgerV1 is the append-only event chain.
type AuthorReviewAttemptLedgerV1 struct {
	Events []AttemptEvent `json:"events"`
}

// AttemptEvent is AttemptStarted or AttemptTerminal (EventKind discriminates).
type AttemptEvent struct {
	EventKind            string  `json:"event_kind"` // attempt-started | attempt-terminal
	EventSequence        int     `json:"event_sequence"`
	PreviousEventDigest  *string `json:"previous_event_digest"`
	AttemptID            string  `json:"attempt_id"`
	Stage                string  `json:"stage"`
	Host                 string  `json:"host"`
	ToolIdentityDigest   string  `json:"tool_identity_digest"`
	ResolvedModel        string  `json:"resolved_model"`
	PromptInputDigest    string  `json:"prompt_input_digest"`
	AuthorAttemptID      *string `json:"author_attempt_id"`
	StartedEventDigest   *string `json:"started_event_digest"`
	IsolationReceiptDigest *string `json:"isolation_receipt_digest"`
	OutputTranscriptDigest *string `json:"output_transcript_digest"`
	AuthoringReceiptDigest *string `json:"authoring_receipt_digest"`
	ReviewRecordDigest     *string `json:"review_record_digest"`
	TerminalOutcome       *string `json:"terminal_outcome"`
	ReasonCode            *string `json:"reason_code"`
	EventDigest           string  `json:"event_digest"`
}

const (
	EventAttemptStarted  = "attempt-started"
	EventAttemptTerminal = "attempt-terminal"
)

// AppendStarted appends an AttemptStarted event (launch-before proof).
func (l *AuthorReviewAttemptLedgerV1) AppendStarted(e AttemptEvent) error {
	e.EventKind = EventAttemptStarted
	return l.append(e)
}

// AppendTerminal appends the exactly-one terminal event for an attempt.
func (l *AuthorReviewAttemptLedgerV1) AppendTerminal(e AttemptEvent) error {
	e.EventKind = EventAttemptTerminal
	return l.append(e)
}

func (l *AuthorReviewAttemptLedgerV1) append(e AttemptEvent) error {
	e.EventSequence = len(l.Events) + 1
	if e.EventSequence == 1 {
		e.PreviousEventDigest = nil
	} else {
		prev := l.Events[len(l.Events)-1].EventDigest
		e.PreviousEventDigest = &prev
	}
	d, err := CanonicalSHA256(e)
	if err != nil {
		return err
	}
	e.EventDigest = d
	l.Events = append(l.Events, e)
	return nil
}

// VerifyLedger replays the chain: continuity, one terminal per start, correct
// stage pairing and digest integrity.
func (l *AuthorReviewAttemptLedgerV1) VerifyLedger() error {
	started := map[string]bool{}
	terminals := map[string]bool{}
	prevDigest := ""
	for i, e := range l.Events {
		if e.EventSequence != i+1 {
			return fmt.Errorf("event %d sequence %d out of order", i, e.EventSequence)
		}
		if i == 0 && e.PreviousEventDigest != nil {
			return errors.New("first event must have null previous_event_digest")
		}
		if i > 0 && (e.PreviousEventDigest == nil || *e.PreviousEventDigest != prevDigest) {
			return fmt.Errorf("event %d breaks the chain", i)
		}
		saved := e.EventDigest
		e.EventDigest = ""
		d, err := CanonicalSHA256(e)
		e.EventDigest = saved
		if err != nil {
			return err
		}
		if d != saved {
			return fmt.Errorf("event %d digest mismatch", i)
		}
		prevDigest = saved
		switch e.EventKind {
		case EventAttemptStarted:
			if started[e.AttemptID] {
				return fmt.Errorf("duplicate start for attempt %s", e.AttemptID)
			}
			started[e.AttemptID] = true
		case EventAttemptTerminal:
			if !started[e.AttemptID] {
				return fmt.Errorf("terminal for unknown attempt %s", e.AttemptID)
			}
			if terminals[e.AttemptID] {
				return fmt.Errorf("attempt %s has two terminals", e.AttemptID)
			}
			terminals[e.AttemptID] = true
		default:
			return fmt.Errorf("event %d kind %q invalid", i, e.EventKind)
		}
	}
	for id := range started {
		if !terminals[id] {
			return fmt.Errorf("attempt %s started but never terminalized", id)
		}
	}
	return nil
}
