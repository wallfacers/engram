package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/wallfacers/engram/provider"
)

const (
	adjudicationManifestSchema = "034.adjudication.manifest.v1"
	adjudicationCustodySchema  = "034.adjudication.custody.v1"
	adjudicationPacketSchema   = "034.adjudication.packet.v1"
	adjudicationDecisionSchema = "034.adjudication.decision.v1"
	adjudicationCallSchema     = "034.adjudication.call.v1"
	adjudicationSealSchema     = "034.adjudication.seal.v1"

	adjudicationManifestFile  = "manifest.json"
	adjudicationPacketsFile   = "packets.jsonl"
	adjudicationSlotMapFile   = "slot-map.jsonl"
	adjudicationCustodyFile   = "custody.json"
	adjudicationCallsFile     = "calls.jsonl"
	adjudicationDecisionsFile = "sealed-decisions.jsonl"
	adjudicationSealFile      = "seal.json"
	adjudicationScoreFile     = "stage0-score.json"
)

const (
	adjudicationDecisionSelected = "selected"
	adjudicationDecisionFallback = "fallback"

	adjudicationFallbackNotTriggered    = "not_triggered"
	adjudicationFallbackProviderFailed  = "provider_failed"
	adjudicationFallbackInvalidResponse = "invalid_response"
	adjudicationFallbackLowConfidence   = "low_confidence"

	adjudicationCallStarted   = "started"
	adjudicationCallCompleted = "completed"
	adjudicationCallFailed    = "failed"
)

const (
	adjudicationFrozenQuestionCount               = 1540
	adjudicationFrozenTriggerCount                = 771
	adjudicationFrozenContextParityCount          = 1532
	adjudicationFrozenTriggeredContextParityCount = 766
)

type adjudicationCandidateInput struct {
	Conv                int    `json:"conv"`
	Q                   int    `json:"q"`
	QuestionID          string `json:"question_id"`
	Category            int    `json:"category"`
	CategoryName        string `json:"category_name,omitempty"`
	Question            string `json:"question"`
	Predicted           string `json:"predicted"`
	AnswerRegime        string `json:"answer_regime"`
	RetrievalFlags      string `json:"retrieval_flags"`
	InputTokens         int    `json:"input_tokens"`
	AnswerContextTokens int    `json:"answer_context_tokens"`
}

type adjudicationCandidateSource struct {
	RawDigest       string
	RawSize         int64
	SanitizedDigest string
	Records         []adjudicationCandidateInput
}

type adjudicationTraceHit struct {
	Name string `json:"name"`
	Rank int    `json:"rank"`
}

type adjudicationTraceInput struct {
	Conv      int                    `json:"conv"`
	Q         int                    `json:"q"`
	Category  int                    `json:"category"`
	Retrieved []adjudicationTraceHit `json:"retrieved"`
}

type adjudicationTraceSource struct {
	RawDigest       string
	RawSize         int64
	SanitizedDigest string
	Records         []adjudicationTraceInput
}

type adjudicationHiddenCandidateLine struct {
	Conv       int    `json:"conv"`
	Q          int    `json:"q"`
	QuestionID string `json:"question_id"`
	Predicted  string `json:"predicted"`
	Gold       string `json:"gold"`
	Correct    *bool  `json:"correct"`
}

type adjudicationHiddenCandidateSource struct {
	Receipt         adjudicationRawReceipt
	SanitizedDigest string
	Candidates      map[string]adjudicationHiddenCandidate
}

type adjudicationManifest struct {
	Schema                          string   `json:"schema"`
	ProtocolID                      string   `json:"protocol_id"`
	ProtocolHash                    string   `json:"protocol_hash"`
	Normalizer                      string   `json:"normalizer"`
	PermutationSeedDigest           string   `json:"permutation_seed_digest"`
	SanitizedCandidateSourceDigests []string `json:"sanitized_candidate_source_digests"`
	SanitizedTraceDigest            string   `json:"sanitized_trace_digest"`
	StoreSemanticDigest             string   `json:"store_semantic_digest"`
	QuestionIDsDigest               string   `json:"question_ids_digest"`
	PromptDigest                    string   `json:"prompt_digest"`
	QuestionCount                   int      `json:"question_count"`
	TriggeredCount                  int      `json:"triggered_count"`
	ContextParityCount              int      `json:"context_parity_count"`
	TriggeredContextParityCount     int      `json:"triggered_context_parity_count"`
	PacketSetDigest                 string   `json:"packet_set_digest"`
}

type adjudicationRawReceipt struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Count  int    `json:"count"`
}

type adjudicationStoreReceipt struct {
	Conv           int    `json:"conv"`
	RawDigest      string `json:"raw_digest"`
	Size           int64  `json:"size"`
	SemanticDigest string `json:"semantic_digest"`
}

type adjudicationCustody struct {
	Schema                    string                     `json:"schema"`
	ProtocolHash              string                     `json:"protocol_hash"`
	CandidateSources          []adjudicationRawReceipt   `json:"candidate_sources"`
	TraceSource               adjudicationRawReceipt     `json:"trace_source"`
	Stores                    []adjudicationStoreReceipt `json:"stores"`
	StoreInventoryDigest      string                     `json:"store_inventory_digest"`
	QuestionIDsDigest         string                     `json:"question_ids_digest"`
	SlotMapDigest             string                     `json:"slot_map_digest"`
	GitCommit                 string                     `json:"git_commit,omitempty"`
	GitDirty                  bool                       `json:"git_dirty"`
	BuildBinaryDigest         string                     `json:"build_binary_digest,omitempty"`
	CandidateModelClaim       string                     `json:"candidate_model_claim"`
	CandidateProvenanceStatus string                     `json:"candidate_provenance_status"`
}

type adjudicationSlotSource struct {
	Slot                   string `json:"slot"`
	SourceDigest           string `json:"source_digest"`
	AnswerDigest           string `json:"answer_digest"`
	NormalizedAnswerDigest string `json:"normalized_answer_digest"`
}

type adjudicationSlotMapRecord struct {
	PacketID   string                   `json:"packet_id"`
	Conv       int                      `json:"conv"`
	Q          int                      `json:"q"`
	QuestionID string                   `json:"question_id"`
	Slots      []adjudicationSlotSource `json:"slots"`
}

type adjudicationEvidenceItem struct {
	EvidenceID    string `json:"evidence_id"`
	Rank          int    `json:"rank"`
	Content       string `json:"content"`
	ContentDigest string `json:"content_digest"`
}

type adjudicationPacketCandidate struct {
	Slot         string `json:"slot"`
	Answer       string `json:"answer"`
	AnswerDigest string `json:"answer_digest"`
}

type adjudicationPacket struct {
	Schema        string                        `json:"schema"`
	ProtocolHash  string                        `json:"protocol_hash"`
	PacketID      string                        `json:"packet_id"`
	PacketDigest  string                        `json:"packet_digest"`
	Conv          int                           `json:"conv"`
	Q             int                           `json:"q"`
	QuestionID    string                        `json:"question_id"`
	Category      int                           `json:"category"`
	Question      string                        `json:"question"`
	Triggered     bool                          `json:"triggered"`
	ContextParity bool                          `json:"context_parity"`
	Evidence      []adjudicationEvidenceItem    `json:"evidence"`
	Candidates    []adjudicationPacketCandidate `json:"candidates"`
}

type adjudicationDecision struct {
	Schema               string   `json:"schema"`
	ProtocolHash         string   `json:"protocol_hash"`
	PacketID             string   `json:"packet_id"`
	PacketDigest         string   `json:"packet_digest"`
	DecisionDigest       string   `json:"decision_digest"`
	Conv                 int      `json:"conv"`
	Q                    int      `json:"q"`
	QuestionID           string   `json:"question_id"`
	Triggered            bool     `json:"triggered"`
	State                string   `json:"state"`
	SelectedSlot         string   `json:"selected_slot"`
	SelectedAnswerDigest string   `json:"selected_answer_digest"`
	EvidenceIDs          []string `json:"evidence_ids"`
	Confidence           string   `json:"confidence"`
	FallbackReason       string   `json:"fallback_reason"`
	ProviderAttempts     int      `json:"provider_attempts"`
	InputTokens          int      `json:"input_tokens"`
	OutputTokens         int      `json:"output_tokens"`
}

type adjudicationCallRecord struct {
	Schema                 string                `json:"schema"`
	ProtocolHash           string                `json:"protocol_hash"`
	PacketID               string                `json:"packet_id"`
	PacketDigest           string                `json:"packet_digest"`
	State                  string                `json:"state"`
	Attempt                int                   `json:"attempt"`
	InputDigest            string                `json:"input_digest"`
	TerminalDecision       *adjudicationDecision `json:"terminal_decision,omitempty"`
	TerminalDecisionDigest string                `json:"terminal_decision_digest,omitempty"`
}

type adjudicationSeal struct {
	Schema              string         `json:"schema"`
	ProtocolHash        string         `json:"protocol_hash"`
	PacketSetDigest     string         `json:"packet_set_digest"`
	DecisionSetDigest   string         `json:"decision_set_digest"`
	PromptDigest        string         `json:"prompt_digest"`
	Provider            string         `json:"provider"`
	BaseURLDigest       string         `json:"base_url_digest"`
	Model               string         `json:"model"`
	ModelRevision       string         `json:"model_revision"`
	MaxTokens           int            `json:"max_tokens"`
	BinaryDigest        string         `json:"binary_digest,omitempty"`
	PlannedCalls        int            `json:"planned_calls"`
	StartedCalls        int            `json:"started_calls"`
	CompletedCalls      int            `json:"completed_calls"`
	FailedCalls         int            `json:"failed_calls"`
	ProviderAttempts    int            `json:"provider_attempts"`
	Retries             int            `json:"retries"`
	InputTokens         int            `json:"input_tokens"`
	OutputTokens        int            `json:"output_tokens"`
	FallbackCounts      map[string]int `json:"fallback_counts"`
	PricingStatus       string         `json:"pricing_status"`
	InputCNYPerMillion  *float64       `json:"input_cny_per_million,omitempty"`
	OutputCNYPerMillion *float64       `json:"output_cny_per_million,omitempty"`
	EstimatedCNY        *float64       `json:"estimated_cny,omitempty"`
	QuestionCount       int            `json:"question_count"`
	DecisionCount       int            `json:"decision_count"`
	Valid               bool           `json:"valid"`
}

func adjudicationDecisionDigest(decision adjudicationDecision) string {
	decision.DecisionDigest = ""
	return evalJSONDigest(decision)
}

func selectedAdjudicationDecision(packet adjudicationPacket, response adjudicationVerifierResponse, usage provider.Usage) adjudicationDecision {
	decision := adjudicationDecision{
		Schema: adjudicationDecisionSchema, ProtocolHash: packet.ProtocolHash, PacketID: packet.PacketID,
		PacketDigest: packet.PacketDigest, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID,
		Triggered: packet.Triggered, State: adjudicationDecisionSelected, SelectedSlot: response.SelectedSlot,
		EvidenceIDs: append([]string(nil), response.EvidenceIDs...), Confidence: "high", ProviderAttempts: 1,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
	}
	for _, candidate := range packet.Candidates {
		if candidate.Slot == response.SelectedSlot {
			decision.SelectedAnswerDigest = candidate.AnswerDigest
			break
		}
	}
	decision.DecisionDigest = adjudicationDecisionDigest(decision)
	return decision
}

func fallbackAdjudicationDecision(packet adjudicationPacket, reason string, attempts int, usage provider.Usage) adjudicationDecision {
	slot := adjudicationTextControlSlot(packet.Candidates)
	decision := adjudicationDecision{
		Schema: adjudicationDecisionSchema, ProtocolHash: packet.ProtocolHash, PacketID: packet.PacketID,
		PacketDigest: packet.PacketDigest, Conv: packet.Conv, Q: packet.Q, QuestionID: packet.QuestionID,
		Triggered: packet.Triggered, State: adjudicationDecisionFallback, SelectedSlot: slot,
		EvidenceIDs: []string{}, Confidence: "fallback", FallbackReason: reason, ProviderAttempts: attempts,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
	}
	for _, candidate := range packet.Candidates {
		if candidate.Slot == slot {
			decision.SelectedAnswerDigest = candidate.AnswerDigest
			break
		}
	}
	decision.DecisionDigest = adjudicationDecisionDigest(decision)
	return decision
}

func validateAdjudicationDecision(decision adjudicationDecision, packet adjudicationPacket) error {
	if decision.Schema != adjudicationDecisionSchema || decision.ProtocolHash != packet.ProtocolHash ||
		decision.PacketID != packet.PacketID || decision.PacketDigest != packet.PacketDigest ||
		decision.Conv != packet.Conv || decision.Q != packet.Q || decision.QuestionID != packet.QuestionID ||
		decision.Triggered != packet.Triggered || decision.DecisionDigest != adjudicationDecisionDigest(decision) {
		return fmt.Errorf("decision identity/digest mismatch")
	}
	if decision.InputTokens < 0 || decision.OutputTokens < 0 {
		return fmt.Errorf("decision usage must be non-negative")
	}
	answerDigest := ""
	for _, candidate := range packet.Candidates {
		if candidate.Slot == decision.SelectedSlot {
			answerDigest = candidate.AnswerDigest
			break
		}
	}
	if answerDigest == "" || answerDigest != decision.SelectedAnswerDigest {
		return fmt.Errorf("decision selected slot/digest mismatch")
	}
	switch decision.State {
	case adjudicationDecisionSelected:
		if !packet.Triggered || decision.ProviderAttempts != 1 || decision.Confidence != "high" ||
			decision.FallbackReason != "" || len(decision.EvidenceIDs) == 0 {
			return fmt.Errorf("invalid selected decision")
		}
		validEvidence := make(map[string]bool, len(packet.Evidence))
		for _, item := range packet.Evidence {
			validEvidence[item.EvidenceID] = true
		}
		seen := make(map[string]bool, len(decision.EvidenceIDs))
		for _, id := range decision.EvidenceIDs {
			if !validEvidence[id] || seen[id] {
				return fmt.Errorf("invalid selected evidence citation")
			}
			seen[id] = true
		}
	case adjudicationDecisionFallback:
		if decision.SelectedSlot != adjudicationTextControlSlot(packet.Candidates) || decision.Confidence != "fallback" ||
			len(decision.EvidenceIDs) != 0 || !validAdjudicationFallbackReason(decision.FallbackReason) {
			return fmt.Errorf("invalid fallback decision")
		}
		if packet.Triggered {
			if decision.ProviderAttempts != 1 || decision.FallbackReason == adjudicationFallbackNotTriggered {
				return fmt.Errorf("invalid triggered fallback")
			}
		} else if decision.ProviderAttempts != 0 || decision.FallbackReason != adjudicationFallbackNotTriggered {
			return fmt.Errorf("invalid non-trigger fallback")
		}
	default:
		return fmt.Errorf("unknown decision state %q", decision.State)
	}
	return nil
}

func validAdjudicationFallbackReason(reason string) bool {
	switch reason {
	case adjudicationFallbackNotTriggered, adjudicationFallbackProviderFailed,
		adjudicationFallbackInvalidResponse, adjudicationFallbackLowConfidence:
		return true
	default:
		return false
	}
}

func loadAdjudicationCandidateSource(path string) (adjudicationCandidateSource, error) {
	rawDigest, err := fileSHA256(path)
	if err != nil {
		return adjudicationCandidateSource{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return adjudicationCandidateSource{}, fmt.Errorf("stat candidate source: %w", err)
	}
	f, err := os.Open(path) //nolint:gosec // operator-selected frozen artifact
	if err != nil {
		return adjudicationCandidateSource{}, fmt.Errorf("open candidate source: %w", err)
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	seen := make(map[string]bool)
	var records []adjudicationCandidateInput
	line := 0
	for scanner.Scan() {
		line++
		var record adjudicationCandidateInput
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return adjudicationCandidateSource{}, fmt.Errorf("decode candidate source line %d: %w", line, err)
		}
		if err := validateAdjudicationCandidateInput(record); err != nil {
			return adjudicationCandidateSource{}, fmt.Errorf("candidate source line %d: %w", line, err)
		}
		if seen[record.QuestionID] {
			return adjudicationCandidateSource{}, fmt.Errorf("duplicate candidate question %q", record.QuestionID)
		}
		seen[record.QuestionID] = true
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return adjudicationCandidateSource{}, fmt.Errorf("scan candidate source: %w", err)
	}
	if len(records) == 0 {
		return adjudicationCandidateSource{}, fmt.Errorf("candidate source is empty")
	}
	sortAdjudicationCandidateInputs(records)
	return adjudicationCandidateSource{
		RawDigest: rawDigest, RawSize: info.Size(), SanitizedDigest: evalJSONDigest(records), Records: records,
	}, nil
}

func loadAdjudicationTraceSource(path string) (adjudicationTraceSource, error) {
	rawDigest, err := fileSHA256(path)
	if err != nil {
		return adjudicationTraceSource{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return adjudicationTraceSource{}, fmt.Errorf("stat trace source: %w", err)
	}
	f, err := os.Open(path) //nolint:gosec // operator-selected frozen artifact
	if err != nil {
		return adjudicationTraceSource{}, fmt.Errorf("open trace source: %w", err)
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	seen := make(map[string]bool)
	var records []adjudicationTraceInput
	line := 0
	for scanner.Scan() {
		line++
		var record adjudicationTraceInput
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return adjudicationTraceSource{}, fmt.Errorf("decode trace line %d: %w", line, err)
		}
		qid := questionID(record.Conv, record.Q)
		if record.Conv < 0 || record.Q < 0 || record.Category < 1 || record.Category > 4 || len(record.Retrieved) != 30 {
			return adjudicationTraceSource{}, fmt.Errorf("invalid trace line %d", line)
		}
		if seen[qid] {
			return adjudicationTraceSource{}, fmt.Errorf("duplicate trace question %q", qid)
		}
		seen[qid] = true
		hitNames := make(map[string]bool, len(record.Retrieved))
		for i, hit := range record.Retrieved {
			if hit.Rank != i+1 || strings.TrimSpace(hit.Name) == "" || hitNames[hit.Name] {
				return adjudicationTraceSource{}, fmt.Errorf("invalid trace hits for %q", qid)
			}
			hitNames[hit.Name] = true
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return adjudicationTraceSource{}, fmt.Errorf("scan trace source: %w", err)
	}
	if len(records) == 0 {
		return adjudicationTraceSource{}, fmt.Errorf("trace source is empty")
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Conv != records[j].Conv {
			return records[i].Conv < records[j].Conv
		}
		return records[i].Q < records[j].Q
	})
	return adjudicationTraceSource{
		RawDigest: rawDigest, RawSize: info.Size(), SanitizedDigest: evalJSONDigest(records), Records: records,
	}, nil
}

func loadAdjudicationHiddenCandidateSource(path string) (adjudicationHiddenCandidateSource, error) {
	sanitized, err := loadAdjudicationCandidateSource(path)
	if err != nil {
		return adjudicationHiddenCandidateSource{}, err
	}
	f, err := os.Open(path) //nolint:gosec // score-only operator-selected artifact
	if err != nil {
		return adjudicationHiddenCandidateSource{}, err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	candidates := make(map[string]adjudicationHiddenCandidate, len(sanitized.Records))
	line := 0
	for scanner.Scan() {
		line++
		var item adjudicationHiddenCandidateLine
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return adjudicationHiddenCandidateSource{}, fmt.Errorf("decode hidden candidate line %d: %w", line, err)
		}
		if item.Conv < 0 || item.Q < 0 || item.QuestionID != questionID(item.Conv, item.Q) ||
			strings.TrimSpace(item.Predicted) == "" || strings.TrimSpace(item.Gold) == "" || item.Correct == nil {
			return adjudicationHiddenCandidateSource{}, fmt.Errorf("incomplete hidden candidate line %d", line)
		}
		if candidates[item.QuestionID].Answer != "" {
			return adjudicationHiddenCandidateSource{}, fmt.Errorf("duplicate hidden candidate %q", item.QuestionID)
		}
		candidates[item.QuestionID] = adjudicationHiddenCandidate{
			Answer: item.Predicted, Normalized: normalizeAdjudicationAnswer(item.Predicted), Correct: *item.Correct,
		}
	}
	if err := scanner.Err(); err != nil {
		return adjudicationHiddenCandidateSource{}, err
	}
	if len(candidates) != len(sanitized.Records) {
		return adjudicationHiddenCandidateSource{}, fmt.Errorf("hidden/sanitized candidate count mismatch")
	}
	for _, record := range sanitized.Records {
		candidate, ok := candidates[record.QuestionID]
		if !ok || candidate.Answer != record.Predicted {
			return adjudicationHiddenCandidateSource{}, fmt.Errorf("hidden candidate projection mismatch for %q", record.QuestionID)
		}
	}
	return adjudicationHiddenCandidateSource{
		Receipt:         adjudicationRawReceipt{Digest: sanitized.RawDigest, Size: sanitized.RawSize, Count: len(sanitized.Records)},
		SanitizedDigest: sanitized.SanitizedDigest, Candidates: candidates,
	}, nil
}

func validateAdjudicationCandidateInput(record adjudicationCandidateInput) error {
	if record.Conv < 0 || record.Q < 0 || record.QuestionID != questionID(record.Conv, record.Q) {
		return fmt.Errorf("invalid candidate identity")
	}
	if record.Category < 1 || record.Category > 4 || strings.TrimSpace(record.Question) == "" ||
		strings.TrimSpace(record.Predicted) == "" || strings.TrimSpace(record.AnswerRegime) == "" ||
		strings.TrimSpace(record.RetrievalFlags) == "" || record.InputTokens < 0 || record.AnswerContextTokens < 0 {
		return fmt.Errorf("incomplete candidate input")
	}
	return nil
}

func sortAdjudicationCandidateInputs(records []adjudicationCandidateInput) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Conv != records[j].Conv {
			return records[i].Conv < records[j].Conv
		}
		return records[i].Q < records[j].Q
	})
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // operator-selected artifact
	if err != nil {
		return "", fmt.Errorf("open for digest: %w", err)
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("digest file: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func decodeAdjudicationPacket(raw []byte) (adjudicationPacket, error) {
	var packet adjudicationPacket
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&packet); err != nil {
		return packet, fmt.Errorf("decode adjudication packet: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return packet, err
	}
	return packet, nil
}

func decodeStrictAdjudicationJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func adjudicationPacketDigest(packet adjudicationPacket) string {
	packet.PacketDigest = ""
	return evalJSONDigest(packet)
}

func validateAdjudicationPacket(packet adjudicationPacket, protocolHash string) error {
	if packet.Schema != adjudicationPacketSchema || packet.ProtocolHash != protocolHash || !isDigest(protocolHash) {
		return fmt.Errorf("packet schema/protocol mismatch")
	}
	if packet.Conv < 0 || packet.Q < 0 || packet.QuestionID != questionID(packet.Conv, packet.Q) ||
		packet.Category < 1 || packet.Category > 4 || strings.TrimSpace(packet.Question) == "" ||
		strings.TrimSpace(packet.PacketID) == "" {
		return fmt.Errorf("invalid packet identity")
	}
	if packet.PacketDigest != adjudicationPacketDigest(packet) {
		return fmt.Errorf("packet digest mismatch")
	}
	if len(packet.Evidence) != 30 {
		return fmt.Errorf("packet requires exactly 30 evidence items")
	}
	for i, item := range packet.Evidence {
		if item.EvidenceID != fmtEvidenceID(i+1) || item.Rank != i+1 || strings.TrimSpace(item.Content) == "" ||
			item.ContentDigest != adjudicationTextDigest(item.Content) {
			return fmt.Errorf("invalid evidence item %d", i)
		}
	}
	if len(packet.Candidates) != 3 {
		return fmt.Errorf("packet requires exactly three candidates")
	}
	for i, candidate := range packet.Candidates {
		if candidate.Slot != fmt.Sprintf("C%d", i+1) || strings.TrimSpace(candidate.Answer) == "" ||
			candidate.AnswerDigest != adjudicationTextDigest(candidate.Answer) {
			return fmt.Errorf("invalid candidate slot %d", i)
		}
	}
	wantTriggered := normalizeAdjudicationAnswer(packet.Candidates[0].Answer) != normalizeAdjudicationAnswer(packet.Candidates[1].Answer) ||
		normalizeAdjudicationAnswer(packet.Candidates[0].Answer) != normalizeAdjudicationAnswer(packet.Candidates[2].Answer)
	if packet.Triggered != wantTriggered {
		return fmt.Errorf("packet trigger mismatch")
	}
	return nil
}

func fmtEvidenceID(rank int) string {
	return fmt.Sprintf("E%02d", rank)
}

func adjudicationManifestProtocolHash(manifest adjudicationManifest) string {
	manifest.ProtocolHash = ""
	manifest.PacketSetDigest = ""
	return evalJSONDigest(manifest)
}

func adjudicationJSONLDigest(records any) (string, []byte, error) {
	value := reflect.ValueOf(records)
	if value.Kind() != reflect.Slice {
		return "", nil, fmt.Errorf("JSONL value must be a slice")
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	for i := 0; i < value.Len(); i++ {
		if err := encoder.Encode(value.Index(i).Interface()); err != nil {
			return "", nil, err
		}
	}
	return adjudicationTextDigest(buffer.String()), buffer.Bytes(), nil
}

func writeAdjudicationJSONL(path string, records any) (string, error) {
	digest, raw, err := adjudicationJSONLDigest(records)
	if err != nil {
		return "", err
	}
	if err := writeAdjudicationAtomic(path, raw); err != nil {
		return "", err
	}
	return digest, nil
}

func writeAdjudicationAtomic(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	remove = false
	return nil
}

func readAdjudicationPackets(path string) ([]adjudicationPacket, error) {
	f, err := os.Open(path) //nolint:gosec // operator-selected artifact
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var packets []adjudicationPacket
	line := 0
	for scanner.Scan() {
		line++
		packet, err := decodeAdjudicationPacket(scanner.Bytes())
		if err != nil {
			return nil, fmt.Errorf("packet line %d: %w", line, err)
		}
		packets = append(packets, packet)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return packets, nil
}

func loadAndValidateAdjudicationPublic(dir string, requireFrozen bool) (adjudicationManifest, []adjudicationPacket, error) {
	var manifest adjudicationManifest
	raw, err := os.ReadFile(filepath.Join(dir, adjudicationManifestFile)) //nolint:gosec
	if err != nil {
		return manifest, nil, fmt.Errorf("read adjudication manifest: %w", err)
	}
	if err := decodeStrictAdjudicationJSON(raw, &manifest); err != nil {
		return manifest, nil, fmt.Errorf("decode adjudication manifest: %w", err)
	}
	if manifest.Schema != adjudicationManifestSchema || manifest.ProtocolHash != adjudicationManifestProtocolHash(manifest) ||
		!isDigest(manifest.ProtocolHash) || manifest.PromptDigest != adjudicationPromptDigest() {
		return manifest, nil, fmt.Errorf("invalid adjudication manifest protocol")
	}
	packets, err := readAdjudicationPackets(filepath.Join(dir, adjudicationPacketsFile))
	if err != nil {
		return manifest, nil, err
	}
	if len(packets) != manifest.QuestionCount {
		return manifest, nil, fmt.Errorf("packet count %d differs from manifest %d", len(packets), manifest.QuestionCount)
	}
	seen := make(map[string]bool, len(packets))
	triggered, parity, triggeredParity := 0, 0, 0
	for i, packet := range packets {
		if err := validateAdjudicationPacket(packet, manifest.ProtocolHash); err != nil {
			return manifest, nil, fmt.Errorf("validate packet %d: %w", i, err)
		}
		if seen[packet.QuestionID] {
			return manifest, nil, fmt.Errorf("duplicate packet question %q", packet.QuestionID)
		}
		seen[packet.QuestionID] = true
		if i > 0 && !adjudicationIdentityLess(packets[i-1].Conv, packets[i-1].Q, packet.Conv, packet.Q) {
			return manifest, nil, fmt.Errorf("packets are not numeric identity sorted")
		}
		if packet.Triggered {
			triggered++
		}
		if packet.ContextParity {
			parity++
			if packet.Triggered {
				triggeredParity++
			}
		}
	}
	digest, _, err := adjudicationJSONLDigest(packets)
	if err != nil || digest != manifest.PacketSetDigest {
		return manifest, nil, fmt.Errorf("packet set digest mismatch")
	}
	if triggered != manifest.TriggeredCount || parity != manifest.ContextParityCount || triggeredParity != manifest.TriggeredContextParityCount {
		return manifest, nil, fmt.Errorf("packet summary differs from manifest")
	}
	if requireFrozen && (manifest.QuestionCount != adjudicationFrozenQuestionCount ||
		manifest.TriggeredCount != adjudicationFrozenTriggerCount ||
		manifest.ContextParityCount != adjudicationFrozenContextParityCount ||
		manifest.TriggeredContextParityCount != adjudicationFrozenTriggeredContextParityCount) {
		return manifest, nil, fmt.Errorf("manifest does not match frozen 034 denominators")
	}
	return manifest, packets, nil
}

func adjudicationIdentityLess(leftConv, leftQ, rightConv, rightQ int) bool {
	return leftConv < rightConv || (leftConv == rightConv && leftQ < rightQ)
}

type adjudicationCallJournal struct {
	mu        sync.Mutex
	file      *os.File
	protocol  string
	packets   map[string]adjudicationPacket
	started   map[string]string
	terminals map[string]adjudicationDecision
	inputs    map[string]string
	starts    int
	completed int
	failed    int
}

func openAdjudicationCallJournal(path, protocolHash string, packets []adjudicationPacket) (*adjudicationCallJournal, error) {
	packetByID := make(map[string]adjudicationPacket, len(packets))
	for _, packet := range packets {
		if packetByID[packet.PacketID].PacketID != "" {
			return nil, fmt.Errorf("duplicate journal packet %q", packet.PacketID)
		}
		packetByID[packet.PacketID] = packet
	}
	journal := &adjudicationCallJournal{
		protocol: protocolHash, packets: packetByID, started: make(map[string]string),
		terminals: make(map[string]adjudicationDecision), inputs: make(map[string]string),
	}
	if f, err := os.Open(path); err == nil { //nolint:gosec // protocol artifact path
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var record adjudicationCallRecord
			if err := decodeStrictAdjudicationJSON(scanner.Bytes(), &record); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("decode call journal line %d: %w", line, err)
			}
			if err := journal.acceptExistingRecord(record); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("validate call journal line %d: %w", line, err)
			}
		}
		if err := scanner.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if len(journal.started) != 0 {
		return nil, fmt.Errorf("call journal contains orphan STARTED record")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // protocol artifact path
	if err != nil {
		return nil, err
	}
	journal.file = f
	return journal, nil
}

func (journal *adjudicationCallJournal) acceptExistingRecord(record adjudicationCallRecord) error {
	packet, ok := journal.packets[record.PacketID]
	if !ok || record.Schema != adjudicationCallSchema || record.ProtocolHash != journal.protocol ||
		record.PacketDigest != packet.PacketDigest || record.Attempt != 1 || !isDigest(record.InputDigest) || !packet.Triggered {
		return fmt.Errorf("call record identity mismatch")
	}
	switch record.State {
	case adjudicationCallStarted:
		if record.TerminalDecision != nil || record.TerminalDecisionDigest != "" ||
			journal.started[record.PacketID] != "" || journal.terminals[record.PacketID].PacketID != "" {
			return fmt.Errorf("duplicate or malformed STARTED record")
		}
		journal.started[record.PacketID] = record.InputDigest
		journal.starts++
	case adjudicationCallCompleted, adjudicationCallFailed:
		if journal.started[record.PacketID] == "" || journal.started[record.PacketID] != record.InputDigest ||
			record.TerminalDecision == nil || record.TerminalDecisionDigest != record.TerminalDecision.DecisionDigest ||
			journal.terminals[record.PacketID].PacketID != "" {
			return fmt.Errorf("terminal record does not match one STARTED record")
		}
		if err := validateAdjudicationDecision(*record.TerminalDecision, packet); err != nil {
			return err
		}
		if (record.State == adjudicationCallCompleted) != (record.TerminalDecision.State == adjudicationDecisionSelected) {
			return fmt.Errorf("call state and terminal decision disagree")
		}
		delete(journal.started, record.PacketID)
		journal.terminals[record.PacketID] = *record.TerminalDecision
		journal.inputs[record.PacketID] = record.InputDigest
		if record.State == adjudicationCallCompleted {
			journal.completed++
		} else {
			journal.failed++
		}
	default:
		return fmt.Errorf("unknown call state %q", record.State)
	}
	return nil
}

func (journal *adjudicationCallJournal) appendRecord(record adjudicationCallRecord) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record); err != nil {
		return err
	}
	if _, err := journal.file.Write(buffer.Bytes()); err != nil {
		return err
	}
	return journal.file.Sync()
}

func (journal *adjudicationCallJournal) Start(packet adjudicationPacket, inputDigest string) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if !packet.Triggered || journal.packets[packet.PacketID].PacketDigest != packet.PacketDigest || !isDigest(inputDigest) ||
		journal.started[packet.PacketID] != "" || journal.terminals[packet.PacketID].PacketID != "" {
		return fmt.Errorf("invalid or duplicate adjudication call start")
	}
	record := adjudicationCallRecord{
		Schema: adjudicationCallSchema, ProtocolHash: journal.protocol, PacketID: packet.PacketID,
		PacketDigest: packet.PacketDigest, State: adjudicationCallStarted, Attempt: 1, InputDigest: inputDigest,
	}
	if err := journal.appendRecord(record); err != nil {
		return err
	}
	journal.started[packet.PacketID] = inputDigest
	journal.starts++
	return nil
}

func (journal *adjudicationCallJournal) Terminal(packet adjudicationPacket, inputDigest string, decision adjudicationDecision, completed bool) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.started[packet.PacketID] != inputDigest || journal.terminals[packet.PacketID].PacketID != "" {
		return fmt.Errorf("adjudication terminal lacks matching STARTED record")
	}
	if err := validateAdjudicationDecision(decision, packet); err != nil {
		return err
	}
	state := adjudicationCallFailed
	if completed {
		state = adjudicationCallCompleted
	}
	if completed != (decision.State == adjudicationDecisionSelected) {
		return fmt.Errorf("call terminal state disagrees with decision")
	}
	record := adjudicationCallRecord{
		Schema: adjudicationCallSchema, ProtocolHash: journal.protocol, PacketID: packet.PacketID,
		PacketDigest: packet.PacketDigest, State: state, Attempt: 1, InputDigest: inputDigest,
		TerminalDecision: &decision, TerminalDecisionDigest: decision.DecisionDigest,
	}
	if err := journal.appendRecord(record); err != nil {
		return err
	}
	delete(journal.started, packet.PacketID)
	journal.terminals[packet.PacketID] = decision
	journal.inputs[packet.PacketID] = inputDigest
	if completed {
		journal.completed++
	} else {
		journal.failed++
	}
	return nil
}

func (journal *adjudicationCallJournal) TerminalDecision(packetID string) (adjudicationDecision, bool) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	decision, ok := journal.terminals[packetID]
	return decision, ok
}

func (journal *adjudicationCallJournal) TerminalInputDigest(packetID string) (string, bool) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	digest, ok := journal.inputs[packetID]
	return digest, ok
}

func (journal *adjudicationCallJournal) Stats() (started, completed, failed int) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.starts, journal.completed, journal.failed
}

func (journal *adjudicationCallJournal) Close() error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.file == nil {
		return nil
	}
	err := journal.file.Close()
	journal.file = nil
	return err
}

func readAdjudicationDecisions(path string) ([]adjudicationDecision, error) {
	f, err := os.Open(path) //nolint:gosec // protocol artifact path
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var decisions []adjudicationDecision
	line := 0
	for scanner.Scan() {
		line++
		var decision adjudicationDecision
		if err := decodeStrictAdjudicationJSON(scanner.Bytes(), &decision); err != nil {
			return nil, fmt.Errorf("decode decision line %d: %w", line, err)
		}
		decisions = append(decisions, decision)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return decisions, nil
}

func readAdjudicationCallJournalState(path, protocolHash string, packets []adjudicationPacket) (*adjudicationCallJournal, error) {
	packetByID := make(map[string]adjudicationPacket, len(packets))
	for _, packet := range packets {
		if packetByID[packet.PacketID].PacketID != "" {
			return nil, fmt.Errorf("duplicate journal packet %q", packet.PacketID)
		}
		packetByID[packet.PacketID] = packet
	}
	journal := &adjudicationCallJournal{
		protocol: protocolHash, packets: packetByID, started: make(map[string]string),
		terminals: make(map[string]adjudicationDecision), inputs: make(map[string]string),
	}
	f, err := os.Open(path) //nolint:gosec // protocol artifact path
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var record adjudicationCallRecord
		if err := decodeStrictAdjudicationJSON(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode call journal line %d: %w", line, err)
		}
		if err := journal.acceptExistingRecord(record); err != nil {
			return nil, fmt.Errorf("validate call journal line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(journal.started) != 0 {
		return nil, fmt.Errorf("call journal contains orphan STARTED record")
	}
	return journal, nil
}

func validateAdjudicationSeal(dir string, manifest adjudicationManifest, packets []adjudicationPacket) (adjudicationSeal, []adjudicationDecision, error) {
	var seal adjudicationSeal
	sealRaw, err := os.ReadFile(filepath.Join(dir, adjudicationSealFile)) //nolint:gosec // protocol artifact path
	if err != nil {
		return seal, nil, fmt.Errorf("read adjudication seal: %w", err)
	}
	if err := decodeStrictAdjudicationJSON(sealRaw, &seal); err != nil {
		return seal, nil, fmt.Errorf("decode adjudication seal: %w", err)
	}
	decisions, err := readAdjudicationDecisions(filepath.Join(dir, adjudicationDecisionsFile))
	if err != nil {
		return seal, nil, fmt.Errorf("read adjudication decisions: %w", err)
	}
	if len(decisions) != len(packets) || len(decisions) != manifest.QuestionCount {
		return seal, nil, fmt.Errorf("sealed decision count mismatch")
	}
	packetByID := make(map[string]adjudicationPacket, len(packets))
	for _, packet := range packets {
		packetByID[packet.PacketID] = packet
	}
	seen := make(map[string]bool, len(decisions))
	fallbacks := make(map[string]int)
	providerAttempts, inputTokens, outputTokens := 0, 0, 0
	for i, decision := range decisions {
		packet, ok := packetByID[decision.PacketID]
		if !ok || seen[decision.PacketID] {
			return seal, nil, fmt.Errorf("unknown or duplicate sealed decision %q", decision.PacketID)
		}
		seen[decision.PacketID] = true
		if i > 0 && !adjudicationIdentityLess(decisions[i-1].Conv, decisions[i-1].Q, decision.Conv, decision.Q) {
			return seal, nil, fmt.Errorf("sealed decisions are not numeric identity sorted")
		}
		if err := validateAdjudicationDecision(decision, packet); err != nil {
			return seal, nil, fmt.Errorf("validate sealed decision %d: %w", i, err)
		}
		providerAttempts += decision.ProviderAttempts
		inputTokens += decision.InputTokens
		outputTokens += decision.OutputTokens
		if decision.State == adjudicationDecisionFallback {
			fallbacks[decision.FallbackReason]++
		}
	}
	decisionSetDigest, _, err := adjudicationJSONLDigest(decisions)
	if err != nil {
		return seal, nil, err
	}
	journal, err := readAdjudicationCallJournalState(filepath.Join(dir, adjudicationCallsFile), manifest.ProtocolHash, packets)
	if err != nil {
		return seal, nil, err
	}
	started, completed, failed := journal.Stats()
	runIdentity := adjudicationRunIdentityDigest(seal.Provider, seal.BaseURLDigest, seal.Model, seal.ModelRevision,
		seal.MaxTokens, seal.BinaryDigest, seal.PromptDigest)
	for _, decision := range decisions {
		terminal, ok := journal.TerminalDecision(decision.PacketID)
		if decision.Triggered {
			if !ok || terminal.DecisionDigest != decision.DecisionDigest {
				return seal, nil, fmt.Errorf("triggered decision lacks matching journal terminal")
			}
			inputDigest, inputOK := journal.TerminalInputDigest(decision.PacketID)
			wantInputDigest, digestErr := adjudicationPacketInputDigest(packetByID[decision.PacketID], runIdentity)
			if digestErr != nil || !inputOK || inputDigest != wantInputDigest {
				return seal, nil, fmt.Errorf("triggered decision call identity mismatch")
			}
		} else if ok {
			return seal, nil, fmt.Errorf("non-triggered decision has a call journal record")
		}
	}
	if seal.Schema != adjudicationSealSchema || !seal.Valid || seal.ProtocolHash != manifest.ProtocolHash ||
		seal.PacketSetDigest != manifest.PacketSetDigest || seal.DecisionSetDigest != decisionSetDigest ||
		seal.PromptDigest != manifest.PromptDigest || strings.TrimSpace(seal.Provider) == "" ||
		strings.TrimSpace(seal.Model) == "" || strings.TrimSpace(seal.ModelRevision) == "" || !isDigest(seal.BaseURLDigest) ||
		seal.MaxTokens < 1 || !isDigest(seal.BinaryDigest) ||
		seal.PlannedCalls != manifest.TriggeredCount || seal.StartedCalls != started || seal.CompletedCalls != completed ||
		seal.FailedCalls != failed || seal.ProviderAttempts != providerAttempts || seal.Retries != 0 ||
		seal.InputTokens != inputTokens || seal.OutputTokens != outputTokens ||
		!reflect.DeepEqual(seal.FallbackCounts, fallbacks) || seal.QuestionCount != manifest.QuestionCount ||
		seal.DecisionCount != len(decisions) || started != providerAttempts || providerAttempts != completed+failed {
		return seal, nil, fmt.Errorf("adjudication seal receipt mismatch")
	}
	switch seal.PricingStatus {
	case "unpriced":
		if seal.InputCNYPerMillion != nil || seal.OutputCNYPerMillion != nil || seal.EstimatedCNY != nil {
			return seal, nil, fmt.Errorf("unpriced seal contains a numeric cost claim")
		}
	case "priced", "declared_zero":
		if seal.InputCNYPerMillion == nil || seal.OutputCNYPerMillion == nil || seal.EstimatedCNY == nil ||
			*seal.InputCNYPerMillion < 0 || *seal.OutputCNYPerMillion < 0 {
			return seal, nil, fmt.Errorf("priced seal is incomplete")
		}
		wantCost := (float64(inputTokens)*(*seal.InputCNYPerMillion) + float64(outputTokens)*(*seal.OutputCNYPerMillion)) / 1_000_000
		if math.Abs(*seal.EstimatedCNY-wantCost) > 1e-12 ||
			(seal.PricingStatus == "declared_zero" && (*seal.InputCNYPerMillion != 0 || *seal.OutputCNYPerMillion != 0)) {
			return seal, nil, fmt.Errorf("seal pricing receipt mismatch")
		}
	default:
		return seal, nil, fmt.Errorf("unknown seal pricing status %q", seal.PricingStatus)
	}
	return seal, decisions, nil
}

func loadAdjudicationSlotMaps(path string) ([]adjudicationSlotMapRecord, string, error) {
	f, err := os.Open(path) //nolint:gosec // score-only protocol artifact path
	if err != nil {
		return nil, "", err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var records []adjudicationSlotMapRecord
	seenPackets := make(map[string]bool)
	line := 0
	for scanner.Scan() {
		line++
		var record adjudicationSlotMapRecord
		if err := decodeStrictAdjudicationJSON(scanner.Bytes(), &record); err != nil {
			return nil, "", fmt.Errorf("decode slot map line %d: %w", line, err)
		}
		if strings.TrimSpace(record.PacketID) == "" || record.Conv < 0 || record.Q < 0 ||
			record.QuestionID != questionID(record.Conv, record.Q) || len(record.Slots) != 3 || seenPackets[record.PacketID] {
			return nil, "", fmt.Errorf("invalid or duplicate slot map line %d", line)
		}
		seenPackets[record.PacketID] = true
		for index, slot := range record.Slots {
			if slot.Slot != fmt.Sprintf("C%d", index+1) || strings.TrimSpace(slot.SourceDigest) == "" ||
				!isDigest(slot.AnswerDigest) || !isDigest(slot.NormalizedAnswerDigest) {
				return nil, "", fmt.Errorf("invalid slot map slot at line %d", line)
			}
		}
		if len(records) > 0 && !adjudicationIdentityLess(records[len(records)-1].Conv, records[len(records)-1].Q, record.Conv, record.Q) {
			return nil, "", fmt.Errorf("slot maps are not numeric identity sorted")
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	digest, _, err := adjudicationJSONLDigest(records)
	if err != nil {
		return nil, "", err
	}
	return records, digest, nil
}

func loadAdjudicationHiddenInputs(dir string, candidatePaths []string) (adjudicationHiddenInputs, error) {
	manifest, packets, err := loadAndValidateAdjudicationPublic(dir, true)
	if err != nil {
		return adjudicationHiddenInputs{}, err
	}
	var custody adjudicationCustody
	custodyRaw, err := os.ReadFile(filepath.Join(dir, adjudicationCustodyFile)) //nolint:gosec // score-only artifact path
	if err != nil {
		return adjudicationHiddenInputs{}, err
	}
	if err := decodeStrictAdjudicationJSON(custodyRaw, &custody); err != nil {
		return adjudicationHiddenInputs{}, fmt.Errorf("decode adjudication custody: %w", err)
	}
	if custody.Schema != adjudicationCustodySchema || custody.ProtocolHash != manifest.ProtocolHash ||
		custody.QuestionIDsDigest != manifest.QuestionIDsDigest || len(custody.CandidateSources) != 3 ||
		len(candidatePaths) != 3 || len(custody.Stores) != 10 ||
		custody.CandidateProvenanceStatus != "legacy_operator_claim" ||
		strings.TrimSpace(custody.CandidateModelClaim) == "" || !isDigest(custody.BuildBinaryDigest) ||
		custody.TraceSource.Count != adjudicationFrozenQuestionCount || custody.TraceSource.Size <= 0 ||
		!isDigest(custody.TraceSource.Digest) || custody.StoreInventoryDigest != evalJSONDigest(custody.Stores) {
		return adjudicationHiddenInputs{}, fmt.Errorf("invalid adjudication custody receipt")
	}
	type semanticStoreReceipt struct {
		Conv   int    `json:"conv"`
		Digest string `json:"digest"`
	}
	semanticReceipts := make([]semanticStoreReceipt, 0, len(custody.Stores))
	for index, receipt := range custody.Stores {
		if receipt.Conv != index || receipt.Size <= 0 || !isDigest(receipt.RawDigest) || !isDigest(receipt.SemanticDigest) {
			return adjudicationHiddenInputs{}, fmt.Errorf("invalid store custody receipt %d", index)
		}
		semanticReceipts = append(semanticReceipts, semanticStoreReceipt{Conv: receipt.Conv, Digest: receipt.SemanticDigest})
	}
	if evalJSONDigest(semanticReceipts) != manifest.StoreSemanticDigest {
		return adjudicationHiddenInputs{}, fmt.Errorf("store semantic custody mismatch")
	}
	sources := make([]adjudicationHiddenCandidateSource, 0, 3)
	for _, path := range candidatePaths {
		source, err := loadAdjudicationHiddenCandidateSource(path)
		if err != nil {
			return adjudicationHiddenInputs{}, err
		}
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].SanitizedDigest < sources[j].SanitizedDigest })
	if len(manifest.SanitizedCandidateSourceDigests) != len(sources) {
		return adjudicationHiddenInputs{}, fmt.Errorf("sanitized candidate source count mismatch")
	}
	hidden := adjudicationHiddenInputs{
		Sources:  make(map[string]map[string]adjudicationHiddenCandidate, len(sources)),
		SlotMaps: make(map[string]adjudicationSlotMapRecord, len(packets)), IntegrityValid: true,
	}
	for index, source := range sources {
		if source.SanitizedDigest != manifest.SanitizedCandidateSourceDigests[index] ||
			source.Receipt != custody.CandidateSources[index] {
			return adjudicationHiddenInputs{}, fmt.Errorf("candidate custody mismatch at source %d", index)
		}
		if hidden.Sources[source.SanitizedDigest] != nil {
			return adjudicationHiddenInputs{}, fmt.Errorf("duplicate hidden candidate source")
		}
		hidden.Sources[source.SanitizedDigest] = source.Candidates
	}
	slotMaps, slotMapDigest, err := loadAdjudicationSlotMaps(filepath.Join(dir, adjudicationSlotMapFile))
	if err != nil {
		return adjudicationHiddenInputs{}, err
	}
	if slotMapDigest != custody.SlotMapDigest || len(slotMaps) != len(packets) {
		return adjudicationHiddenInputs{}, fmt.Errorf("slot-map custody/count mismatch")
	}
	packetByID := make(map[string]adjudicationPacket, len(packets))
	for _, packet := range packets {
		packetByID[packet.PacketID] = packet
	}
	for _, slotMap := range slotMaps {
		packet, ok := packetByID[slotMap.PacketID]
		if !ok || slotMap.Conv != packet.Conv || slotMap.Q != packet.Q || slotMap.QuestionID != packet.QuestionID {
			return adjudicationHiddenInputs{}, fmt.Errorf("slot map packet identity mismatch")
		}
		hidden.SlotMaps[slotMap.PacketID] = slotMap
	}
	return hidden, nil
}
