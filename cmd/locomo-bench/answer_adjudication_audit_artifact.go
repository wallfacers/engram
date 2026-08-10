package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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
	adjudicationAuditManifestSchema = "035.adjudication.audit.manifest.v1"
	adjudicationAuditPacketSchema   = "035.adjudication.audit.packet.v1"
	adjudicationAuditResolverSchema = "035.adjudication.audit.resolver.v1"
	adjudicationAuditCallSchema     = "035.adjudication.audit.call.v1"
	adjudicationAuditDecisionSchema = "035.adjudication.audit.decision.v1"
	adjudicationAuditSealSchema     = "035.adjudication.audit.seal.v1"

	adjudicationAuditManifestFile    = "audit-manifest.json"
	adjudicationAuditPacketsFile     = "audit-packets.jsonl"
	adjudicationAuditResolverMapFile = "resolver-map.jsonl"
	adjudicationAuditCallsFile       = "audit-calls.jsonl"
	adjudicationAuditDecisionsFile   = "second-pass-decisions.jsonl"
	adjudicationAuditSealFile        = "audit-seal.json"
	adjudicationAuditScoreFile       = "audit-stage0-score.json"

	adjudicationAuditViewEntailment    = "entailment"
	adjudicationAuditViewFalsification = "falsification"

	adjudicationAuditCallStarted   = "started"
	adjudicationAuditCallCompleted = "completed"
	adjudicationAuditCallFailed    = "failed"

	adjudicationAuditFailureProvider = "provider_failed"
	adjudicationAuditFailureResponse = "invalid_response"
	adjudicationAuditFailureUsage    = "invalid_usage"

	adjudicationAuditResolutionRetainedNonrisk = "retained_nonrisk"
	adjudicationAuditResolutionRetained        = "retained_audit"
	adjudicationAuditResolutionSwitched        = "switched_dual_convergence"

	adjudicationAuditFrozenQuestionCount = 1540
	adjudicationAuditFrozenRiskCount     = 477
	adjudicationAuditFrozenOverrideCount = 424
	adjudicationAuditFrozenFallbackCount = 53
	adjudicationAuditFrozenRetainCount   = 1063
	adjudicationAuditFrozenViewCount     = 954

	adjudicationAuditFrozenParentProtocolHash       = "sha256:9b840473b0c1fef8c5c0f97a55c5cde6fb7fa771efb8103ff74a526aa99efb19"
	adjudicationAuditFrozenParentPacketSetDigest    = "sha256:70d63daf01bf07e3fc2de3535d940f12c3c2f198f854b89b7b1221bc687e0a4a"
	adjudicationAuditFrozenParentDecisionSetDigest  = "sha256:7f38f710bb9e7b42446f9c32ed94f0ee893b3a39b070df19d3e9e4481c3f3694"
	adjudicationAuditFrozenParentPromptDigest       = "sha256:a92fed147d2cf4a5deec0469c2fbfda36d28f8de06ed34a2a411682bc185f36e"
	adjudicationAuditFrozenParentManifestRawDigest  = "sha256:72028ae37355431fe7495e803fee1e8249b5c70374e915c099b4ddfa15843fd4"
	adjudicationAuditFrozenParentPacketsRawDigest   = adjudicationAuditFrozenParentPacketSetDigest
	adjudicationAuditFrozenParentCallsRawDigest     = "sha256:b3f42e321add36a88849b8c5ee09161e663834b82debb950557f001646fbab33"
	adjudicationAuditFrozenParentDecisionsRawDigest = adjudicationAuditFrozenParentDecisionSetDigest
	adjudicationAuditFrozenParentSealRawDigest      = "sha256:58a43d0950cd06631aca4dedde00031655b913dd964fc1da6cd50bb5fb542c90"
)

type adjudicationAuditParentReceipt struct {
	ProtocolHash       string `json:"protocol_hash"`
	PacketSetDigest    string `json:"packet_set_digest"`
	DecisionSetDigest  string `json:"decision_set_digest"`
	PromptDigest       string `json:"prompt_digest"`
	ManifestRawDigest  string `json:"manifest_raw_digest"`
	PacketsRawDigest   string `json:"packets_raw_digest"`
	CallsRawDigest     string `json:"calls_raw_digest"`
	DecisionsRawDigest string `json:"decisions_raw_digest"`
	SealRawDigest      string `json:"seal_raw_digest"`
	QuestionCount      int    `json:"question_count"`
	TriggeredCount     int    `json:"triggered_count"`
	SelectedCount      int    `json:"selected_count"`
	FallbackCount      int    `json:"fallback_count"`
	ProviderAttempts   int    `json:"provider_attempts"`
	Retries            int    `json:"retries"`
}

type adjudicationAuditManifest struct {
	Schema                    string                         `json:"schema"`
	ProtocolID                string                         `json:"protocol_id"`
	ProtocolHash              string                         `json:"protocol_hash"`
	Parent                    adjudicationAuditParentReceipt `json:"parent"`
	Normalizer                string                         `json:"normalizer"`
	QueueRule                 string                         `json:"queue_rule"`
	ViewSeedDigest            string                         `json:"view_seed_digest"`
	EntailmentPromptDigest    string                         `json:"entailment_prompt_digest"`
	FalsificationPromptDigest string                         `json:"falsification_prompt_digest"`
	ResolverDigest            string                         `json:"resolver_digest"`
	QuestionCount             int                            `json:"question_count"`
	RiskCount                 int                            `json:"risk_count"`
	OverrideCount             int                            `json:"override_count"`
	FallbackCount             int                            `json:"fallback_count"`
	RetainCount               int                            `json:"retain_count"`
	ViewCount                 int                            `json:"view_count"`
	PlannedCalls              int                            `json:"planned_calls"`
	AuditPacketSetDigest      string                         `json:"audit_packet_set_digest"`
	ResolverMapSetDigest      string                         `json:"resolver_map_set_digest"`
}

type adjudicationAuditViewCandidate struct {
	Slot                 string `json:"slot"`
	RepresentativeAnswer string `json:"representative_answer"`
}

type adjudicationAuditView struct {
	ViewID     string                           `json:"view_id"`
	ViewDigest string                           `json:"view_digest"`
	Candidates []adjudicationAuditViewCandidate `json:"candidates"`
}

type adjudicationAuditPacket struct {
	Schema        string                     `json:"schema"`
	ProtocolHash  string                     `json:"protocol_hash"`
	PacketID      string                     `json:"packet_id"`
	PacketDigest  string                     `json:"packet_digest"`
	Conv          int                        `json:"conv"`
	Q             int                        `json:"q"`
	QuestionID    string                     `json:"question_id"`
	Category      int                        `json:"category"`
	Question      string                     `json:"question"`
	ContextParity bool                       `json:"context_parity"`
	Evidence      []adjudicationEvidenceItem `json:"evidence"`
	Views         []adjudicationAuditView    `json:"views"`
}

type adjudicationAuditAnswerGroup struct {
	GroupDigest                string   `json:"group_digest"`
	NormalizedDigest           string   `json:"normalized_digest"`
	MemberAnswerDigests        []string `json:"member_answer_digests"`
	RepresentativeParentSlot   string   `json:"representative_parent_slot"`
	RepresentativeAnswerDigest string   `json:"representative_answer_digest"`

	representativeAnswer string
}

type adjudicationAuditViewMap struct {
	ViewID      string            `json:"view_id"`
	SlotToGroup map[string]string `json:"slot_to_group"`
}

type adjudicationAuditResolverMapRecord struct {
	Schema                     string                         `json:"schema"`
	ProtocolHash               string                         `json:"protocol_hash"`
	PacketID                   string                         `json:"packet_id"`
	ParentPacketDigest         string                         `json:"parent_packet_digest"`
	ParentDecisionDigest       string                         `json:"parent_decision_digest"`
	Conv                       int                            `json:"conv"`
	Q                          int                            `json:"q"`
	QuestionID                 string                         `json:"question_id"`
	ParentSelectedSlot         string                         `json:"parent_selected_slot"`
	ParentSelectedAnswerDigest string                         `json:"parent_selected_answer_digest"`
	ParentSelectedGroupDigest  string                         `json:"parent_selected_group_digest"`
	TextControlGroupDigest     string                         `json:"text_control_group_digest"`
	Groups                     []adjudicationAuditAnswerGroup `json:"groups"`
	Risk                       bool                           `json:"risk"`
	RiskPacketID               string                         `json:"risk_packet_id,omitempty"`
	ViewMaps                   []adjudicationAuditViewMap     `json:"view_maps,omitempty"`
}

type adjudicationAuditAxis struct {
	Value       string   `json:"value"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type adjudicationAuditCandidateAssessment struct {
	Slot          string                `json:"slot"`
	Support       adjudicationAuditAxis `json:"support"`
	Contradiction adjudicationAuditAxis `json:"contradiction"`
}

type adjudicationAuditResponse struct {
	Assessments []adjudicationAuditCandidateAssessment `json:"assessments"`
}

type adjudicationAuditCallRecord struct {
	Schema         string                                 `json:"schema"`
	ProtocolHash   string                                 `json:"protocol_hash"`
	PacketID       string                                 `json:"packet_id"`
	PacketDigest   string                                 `json:"packet_digest"`
	ViewID         string                                 `json:"view_id"`
	ViewDigest     string                                 `json:"view_digest"`
	State          string                                 `json:"state"`
	Attempt        int                                    `json:"attempt"`
	InputDigest    string                                 `json:"input_digest"`
	Assessments    []adjudicationAuditCandidateAssessment `json:"assessments,omitempty"`
	FailureReason  string                                 `json:"failure_reason,omitempty"`
	InputTokens    int                                    `json:"input_tokens,omitempty"`
	OutputTokens   int                                    `json:"output_tokens,omitempty"`
	TerminalDigest string                                 `json:"terminal_digest,omitempty"`
}

type adjudicationAuditDecision struct {
	Schema               string   `json:"schema"`
	ProtocolHash         string   `json:"protocol_hash"`
	PacketID             string   `json:"packet_id"`
	ParentPacketDigest   string   `json:"parent_packet_digest"`
	ParentDecisionDigest string   `json:"parent_decision_digest"`
	DecisionDigest       string   `json:"decision_digest"`
	Conv                 int      `json:"conv"`
	Q                    int      `json:"q"`
	QuestionID           string   `json:"question_id"`
	FinalParentSlot      string   `json:"final_parent_slot"`
	FinalAnswerDigest    string   `json:"final_answer_digest"`
	FinalGroupDigest     string   `json:"final_group_digest"`
	AuditTerminalDigests []string `json:"audit_terminal_digests"`
	Resolution           string   `json:"resolution"`
	ResolutionReason     string   `json:"resolution_reason"`
	ProviderAttempts     int      `json:"provider_attempts"`
	InputTokens          int      `json:"input_tokens"`
	OutputTokens         int      `json:"output_tokens"`
}

type adjudicationAuditSeal struct {
	Schema                    string                         `json:"schema"`
	SealDigest                string                         `json:"seal_digest"`
	ProtocolHash              string                         `json:"protocol_hash"`
	Parent                    adjudicationAuditParentReceipt `json:"parent"`
	AuditPacketSetDigest      string                         `json:"audit_packet_set_digest"`
	ResolverMapSetDigest      string                         `json:"resolver_map_set_digest"`
	CanonicalCallStateDigest  string                         `json:"canonical_call_state_digest"`
	DecisionSetDigest         string                         `json:"decision_set_digest"`
	EntailmentPromptDigest    string                         `json:"entailment_prompt_digest"`
	FalsificationPromptDigest string                         `json:"falsification_prompt_digest"`
	ResolverDigest            string                         `json:"resolver_digest"`
	Provider                  string                         `json:"provider"`
	BaseURLDigest             string                         `json:"base_url_digest"`
	Model                     string                         `json:"model"`
	ModelRevision             string                         `json:"model_revision"`
	MaxTokens                 int                            `json:"max_tokens"`
	BinaryDigest              string                         `json:"binary_digest"`
	QuestionCount             int                            `json:"question_count"`
	RiskCount                 int                            `json:"risk_count"`
	ViewCount                 int                            `json:"view_count"`
	PlannedCalls              int                            `json:"planned_calls"`
	StartedCalls              int                            `json:"started_calls"`
	TerminalCalls             int                            `json:"terminal_calls"`
	CompletedCalls            int                            `json:"completed_calls"`
	FailedCalls               int                            `json:"failed_calls"`
	ProviderAttempts          int                            `json:"provider_attempts"`
	Retries                   int                            `json:"retries"`
	DecisionCount             int                            `json:"decision_count"`
	RetainedCount             int                            `json:"retained_count"`
	SwitchedCount             int                            `json:"switched_count"`
	ResolutionCounts          map[string]int                 `json:"resolution_counts"`
	FailureCounts             map[string]int                 `json:"failure_counts"`
	InputTokens               int                            `json:"input_tokens"`
	OutputTokens              int                            `json:"output_tokens"`
	PricingStatus             string                         `json:"pricing_status"`
	InputCNYPerMillion        *float64                       `json:"input_cny_per_million,omitempty"`
	OutputCNYPerMillion       *float64                       `json:"output_cny_per_million,omitempty"`
	EstimatedCNY              *float64                       `json:"estimated_cny,omitempty"`
	Valid                     bool                           `json:"valid"`
}

func adjudicationAuditManifestProtocolHash(manifest adjudicationAuditManifest) string {
	manifest.ProtocolHash = ""
	manifest.AuditPacketSetDigest = ""
	manifest.ResolverMapSetDigest = ""
	return evalJSONDigest(manifest)
}

func adjudicationAuditViewDigest(view adjudicationAuditView) string {
	view.ViewDigest = ""
	return evalJSONDigest(view)
}

func adjudicationAuditPacketDigest(packet adjudicationAuditPacket) string {
	packet.PacketDigest = ""
	return evalJSONDigest(packet)
}

func adjudicationAuditCallTerminalDigest(record adjudicationAuditCallRecord) string {
	record.TerminalDigest = ""
	return evalJSONDigest(record)
}

func adjudicationAuditDecisionDigest(decision adjudicationAuditDecision) string {
	decision.DecisionDigest = ""
	return evalJSONDigest(decision)
}

func adjudicationAuditSealDigest(seal adjudicationAuditSeal) string {
	seal.SealDigest = ""
	return evalJSONDigest(seal)
}

func decodeAdjudicationAuditPacket(raw []byte) (adjudicationAuditPacket, error) {
	var packet adjudicationAuditPacket
	if err := decodeStrictAdjudicationJSON(raw, &packet); err != nil {
		return packet, fmt.Errorf("decode adjudication audit packet: %w", err)
	}
	return packet, nil
}

func validateAdjudicationAuditPacket(packet adjudicationAuditPacket, protocolHash string) error {
	if packet.Schema != adjudicationAuditPacketSchema || packet.ProtocolHash != protocolHash || !isDigest(protocolHash) ||
		packet.PacketDigest != adjudicationAuditPacketDigest(packet) {
		return fmt.Errorf("audit packet schema/protocol/digest mismatch")
	}
	if packet.Conv < 0 || packet.Q < 0 || packet.QuestionID != questionID(packet.Conv, packet.Q) ||
		packet.Category < 1 || packet.Category > 4 || strings.TrimSpace(packet.Question) == "" ||
		strings.TrimSpace(packet.PacketID) == "" || len(packet.Evidence) != 30 || len(packet.Views) != 2 {
		return fmt.Errorf("invalid audit packet identity or cardinality")
	}
	for index, item := range packet.Evidence {
		if item.EvidenceID != fmtEvidenceID(index+1) || item.Rank != index+1 || strings.TrimSpace(item.Content) == "" ||
			item.ContentDigest != adjudicationTextDigest(item.Content) {
			return fmt.Errorf("invalid audit evidence %d", index)
		}
	}
	if packet.Views[0].ViewID != adjudicationAuditViewEntailment || packet.Views[1].ViewID != adjudicationAuditViewFalsification ||
		len(packet.Views[0].Candidates) < 2 || len(packet.Views[0].Candidates) > 3 ||
		len(packet.Views[0].Candidates) != len(packet.Views[1].Candidates) ||
		reflect.DeepEqual(packet.Views[0].Candidates, packet.Views[1].Candidates) {
		return fmt.Errorf("invalid audit views")
	}
	for _, view := range packet.Views {
		if view.ViewDigest != adjudicationAuditViewDigest(view) {
			return fmt.Errorf("audit view digest mismatch")
		}
		answers := make(map[string]bool, len(view.Candidates))
		for index, candidate := range view.Candidates {
			if candidate.Slot != fmt.Sprintf("A%d", index+1) || strings.TrimSpace(candidate.RepresentativeAnswer) == "" ||
				answers[candidate.RepresentativeAnswer] {
				return fmt.Errorf("invalid audit view candidate")
			}
			answers[candidate.RepresentativeAnswer] = true
		}
	}
	return nil
}

func validateAdjudicationAuditResolver(record adjudicationAuditResolverMapRecord, protocolHash string) error {
	if record.Schema != adjudicationAuditResolverSchema || record.ProtocolHash != protocolHash ||
		strings.TrimSpace(record.PacketID) == "" || !isDigest(record.ParentPacketDigest) || !isDigest(record.ParentDecisionDigest) ||
		record.Conv < 0 || record.Q < 0 || record.QuestionID != questionID(record.Conv, record.Q) ||
		strings.TrimSpace(record.ParentSelectedSlot) == "" || !isDigest(record.ParentSelectedAnswerDigest) ||
		!isDigest(record.ParentSelectedGroupDigest) || !isDigest(record.TextControlGroupDigest) ||
		len(record.Groups) < 1 || len(record.Groups) > 3 {
		return fmt.Errorf("invalid audit resolver identity")
	}
	groups := make(map[string]adjudicationAuditAnswerGroup, len(record.Groups))
	members := make(map[string]bool, 3)
	for _, group := range record.Groups {
		if !isDigest(group.GroupDigest) || !isDigest(group.NormalizedDigest) ||
			!isDigest(group.RepresentativeAnswerDigest) || strings.TrimSpace(group.RepresentativeParentSlot) == "" ||
			len(group.MemberAnswerDigests) == 0 || groups[group.GroupDigest].GroupDigest != "" {
			return fmt.Errorf("invalid audit answer group")
		}
		if !sort.StringsAreSorted(group.MemberAnswerDigests) {
			return fmt.Errorf("audit answer group members are not sorted")
		}
		for _, digest := range group.MemberAnswerDigests {
			if !isDigest(digest) || members[digest] {
				return fmt.Errorf("invalid or duplicate audit group member")
			}
			members[digest] = true
		}
		wantGroupDigest := evalJSONDigest(struct {
			NormalizedDigest    string   `json:"normalized_digest"`
			MemberAnswerDigests []string `json:"member_answer_digests"`
		}{NormalizedDigest: group.NormalizedDigest, MemberAnswerDigests: group.MemberAnswerDigests})
		if group.GroupDigest != wantGroupDigest || !members[group.RepresentativeAnswerDigest] {
			return fmt.Errorf("audit answer group digest/representative mismatch")
		}
		groups[group.GroupDigest] = group
	}
	if len(members) < len(groups) || len(members) > 3 || groups[record.ParentSelectedGroupDigest].GroupDigest == "" ||
		groups[record.TextControlGroupDigest].GroupDigest == "" ||
		!containsAdjudicationAuditDigest(groups[record.ParentSelectedGroupDigest].MemberAnswerDigests, record.ParentSelectedAnswerDigest) {
		return fmt.Errorf("audit answer groups do not cover parent candidates")
	}
	if record.Risk {
		if record.RiskPacketID != record.PacketID || len(record.ViewMaps) != 2 ||
			record.ViewMaps[0].ViewID != adjudicationAuditViewEntailment ||
			record.ViewMaps[1].ViewID != adjudicationAuditViewFalsification {
			return fmt.Errorf("invalid risk resolver view maps")
		}
		for _, viewMap := range record.ViewMaps {
			if len(viewMap.SlotToGroup) != len(groups) {
				return fmt.Errorf("audit view map cardinality mismatch")
			}
			seen := make(map[string]bool, len(groups))
			for index := 0; index < len(groups); index++ {
				groupDigest := viewMap.SlotToGroup[fmt.Sprintf("A%d", index+1)]
				if groups[groupDigest].GroupDigest == "" || seen[groupDigest] {
					return fmt.Errorf("invalid audit view group mapping")
				}
				seen[groupDigest] = true
			}
		}
	} else if record.RiskPacketID != "" || len(record.ViewMaps) != 0 {
		return fmt.Errorf("non-risk resolver contains audit view state")
	}
	return nil
}

func containsAdjudicationAuditDigest(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type adjudicationAuditCallJournal struct {
	mu        sync.Mutex
	file      *os.File
	protocol  string
	packets   map[string]adjudicationAuditPacket
	started   map[string]string
	terminals map[string]adjudicationAuditCallRecord
	starts    int
	completed int
	failed    int
}

func adjudicationAuditCallKey(packetID, viewID string) string {
	return packetID + "\x00" + viewID
}

func findAdjudicationAuditView(packet adjudicationAuditPacket, viewID string) (adjudicationAuditView, bool) {
	for _, view := range packet.Views {
		if view.ViewID == viewID {
			return view, true
		}
	}
	return adjudicationAuditView{}, false
}

func openAdjudicationAuditCallJournal(path, protocolHash string, packets []adjudicationAuditPacket) (*adjudicationAuditCallJournal, error) {
	packetByID := make(map[string]adjudicationAuditPacket, len(packets))
	for _, packet := range packets {
		if packetByID[packet.PacketID].PacketID != "" {
			return nil, fmt.Errorf("duplicate audit journal packet")
		}
		packetByID[packet.PacketID] = packet
	}
	journal := &adjudicationAuditCallJournal{
		protocol: protocolHash, packets: packetByID, started: make(map[string]string),
		terminals: make(map[string]adjudicationAuditCallRecord),
	}
	if f, err := os.Open(path); err == nil { //nolint:gosec // protocol artifact path
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var record adjudicationAuditCallRecord
			if err := decodeStrictAdjudicationJSON(scanner.Bytes(), &record); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("decode audit call line %d: %w", line, err)
			}
			if err := journal.acceptExisting(record); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("validate audit call line %d: %w", line, err)
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
		return nil, fmt.Errorf("audit call journal contains orphan STARTED record")
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

func (journal *adjudicationAuditCallJournal) acceptExisting(record adjudicationAuditCallRecord) error {
	packet, ok := journal.packets[record.PacketID]
	view, viewOK := findAdjudicationAuditView(packet, record.ViewID)
	if !ok || !viewOK || record.Schema != adjudicationAuditCallSchema || record.ProtocolHash != journal.protocol ||
		record.PacketDigest != packet.PacketDigest || record.ViewDigest != view.ViewDigest || record.Attempt != 1 ||
		!isDigest(record.InputDigest) {
		return fmt.Errorf("audit call identity mismatch")
	}
	key := adjudicationAuditCallKey(record.PacketID, record.ViewID)
	switch record.State {
	case adjudicationAuditCallStarted:
		if len(record.Assessments) != 0 || record.FailureReason != "" || record.InputTokens != 0 || record.OutputTokens != 0 ||
			record.TerminalDigest != "" || journal.started[key] != "" || journal.terminals[key].PacketID != "" {
			return fmt.Errorf("duplicate or malformed audit STARTED record")
		}
		journal.started[key] = record.InputDigest
		journal.starts++
	case adjudicationAuditCallCompleted, adjudicationAuditCallFailed:
		if journal.started[key] == "" || journal.started[key] != record.InputDigest ||
			journal.terminals[key].PacketID != "" || record.InputTokens < 0 || record.OutputTokens < 0 ||
			record.TerminalDigest != adjudicationAuditCallTerminalDigest(record) {
			return fmt.Errorf("audit terminal does not match one STARTED record")
		}
		if record.State == adjudicationAuditCallCompleted {
			if record.FailureReason != "" || validateAdjudicationAuditAssessments(record.Assessments, packet, view) != nil {
				return fmt.Errorf("invalid completed audit terminal")
			}
			journal.completed++
		} else {
			if len(record.Assessments) != 0 || !validAdjudicationAuditFailureReason(record.FailureReason) {
				return fmt.Errorf("invalid failed audit terminal")
			}
			journal.failed++
		}
		delete(journal.started, key)
		journal.terminals[key] = record
	default:
		return fmt.Errorf("unknown audit call state")
	}
	return nil
}

func validAdjudicationAuditFailureReason(reason string) bool {
	switch reason {
	case adjudicationAuditFailureProvider, adjudicationAuditFailureResponse, adjudicationAuditFailureUsage:
		return true
	default:
		return false
	}
}

func (journal *adjudicationAuditCallJournal) append(record adjudicationAuditCallRecord) error {
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

func (journal *adjudicationAuditCallJournal) Start(packet adjudicationAuditPacket, view adjudicationAuditView, inputDigest string) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	key := adjudicationAuditCallKey(packet.PacketID, view.ViewID)
	knownView, ok := findAdjudicationAuditView(journal.packets[packet.PacketID], view.ViewID)
	if !ok || knownView.ViewDigest != view.ViewDigest || !isDigest(inputDigest) ||
		journal.started[key] != "" || journal.terminals[key].PacketID != "" {
		return fmt.Errorf("invalid or duplicate audit call start")
	}
	record := adjudicationAuditCallRecord{
		Schema: adjudicationAuditCallSchema, ProtocolHash: journal.protocol, PacketID: packet.PacketID,
		PacketDigest: packet.PacketDigest, ViewID: view.ViewID, ViewDigest: view.ViewDigest,
		State: adjudicationAuditCallStarted, Attempt: 1, InputDigest: inputDigest,
	}
	if err := journal.append(record); err != nil {
		return err
	}
	journal.started[key] = inputDigest
	journal.starts++
	return nil
}

func (journal *adjudicationAuditCallJournal) Terminal(packet adjudicationAuditPacket, view adjudicationAuditView, inputDigest string, assessments []adjudicationAuditCandidateAssessment, failureReason string, usage provider.Usage) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	key := adjudicationAuditCallKey(packet.PacketID, view.ViewID)
	if journal.started[key] != inputDigest || journal.terminals[key].PacketID != "" || usage.InputTokens < 0 || usage.OutputTokens < 0 {
		return fmt.Errorf("audit terminal lacks matching STARTED record")
	}
	state := adjudicationAuditCallCompleted
	if failureReason != "" {
		state = adjudicationAuditCallFailed
		assessments = nil
	}
	record := adjudicationAuditCallRecord{
		Schema: adjudicationAuditCallSchema, ProtocolHash: journal.protocol, PacketID: packet.PacketID,
		PacketDigest: packet.PacketDigest, ViewID: view.ViewID, ViewDigest: view.ViewDigest, State: state,
		Attempt: 1, InputDigest: inputDigest, Assessments: assessments, FailureReason: failureReason,
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
	}
	if state == adjudicationAuditCallCompleted {
		if err := validateAdjudicationAuditAssessments(record.Assessments, packet, view); err != nil {
			return err
		}
	} else if !validAdjudicationAuditFailureReason(failureReason) {
		return fmt.Errorf("invalid audit terminal failure reason")
	}
	record.TerminalDigest = adjudicationAuditCallTerminalDigest(record)
	if err := journal.append(record); err != nil {
		return err
	}
	delete(journal.started, key)
	journal.terminals[key] = record
	if state == adjudicationAuditCallCompleted {
		journal.completed++
	} else {
		journal.failed++
	}
	return nil
}

func (journal *adjudicationAuditCallJournal) TerminalRecord(packetID, viewID string) (adjudicationAuditCallRecord, bool) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	record, ok := journal.terminals[adjudicationAuditCallKey(packetID, viewID)]
	return record, ok
}

func (journal *adjudicationAuditCallJournal) Stats() (started, completed, failed int) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.starts, journal.completed, journal.failed
}

func (journal *adjudicationAuditCallJournal) SortedTerminals() []adjudicationAuditCallRecord {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	terminals := make([]adjudicationAuditCallRecord, 0, len(journal.terminals))
	for _, record := range journal.terminals {
		terminals = append(terminals, record)
	}
	sort.Slice(terminals, func(i, j int) bool {
		left, right := journal.packets[terminals[i].PacketID], journal.packets[terminals[j].PacketID]
		if left.Conv != right.Conv || left.Q != right.Q {
			return adjudicationIdentityLess(left.Conv, left.Q, right.Conv, right.Q)
		}
		return terminals[i].ViewID == adjudicationAuditViewEntailment && terminals[j].ViewID == adjudicationAuditViewFalsification
	})
	return terminals
}

func (journal *adjudicationAuditCallJournal) Close() error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.file == nil {
		return nil
	}
	err := journal.file.Close()
	journal.file = nil
	return err
}

func readAdjudicationAuditPackets(path string) ([]adjudicationAuditPacket, error) {
	var packets []adjudicationAuditPacket
	err := scanAdjudicationAuditJSONL(path, func(line int, raw []byte) error {
		packet, err := decodeAdjudicationAuditPacket(raw)
		if err != nil {
			return fmt.Errorf("audit packet line %d: %w", line, err)
		}
		packets = append(packets, packet)
		return nil
	})
	return packets, err
}

func readAdjudicationAuditResolverMap(path string) ([]adjudicationAuditResolverMapRecord, error) {
	var records []adjudicationAuditResolverMapRecord
	err := scanAdjudicationAuditJSONL(path, func(line int, raw []byte) error {
		var record adjudicationAuditResolverMapRecord
		if err := decodeStrictAdjudicationJSON(raw, &record); err != nil {
			return fmt.Errorf("decode audit resolver line %d: %w", line, err)
		}
		record.Groups = append([]adjudicationAuditAnswerGroup(nil), record.Groups...)
		for index := range record.Groups {
			record.Groups[index].representativeAnswer = ""
		}
		records = append(records, record)
		return nil
	})
	return records, err
}

func scanAdjudicationAuditJSONL(path string, accept func(int, []byte) error) error {
	f, err := os.Open(path) //nolint:gosec // operator-selected protocol artifact
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if err := accept(line, append([]byte(nil), scanner.Bytes()...)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func writeAdjudicationAuditBuild(dir string, manifest adjudicationAuditManifest, packets []adjudicationAuditPacket, resolver []adjudicationAuditResolverMapRecord) error {
	packetDigest, packetRaw, err := adjudicationJSONLDigest(packets)
	if err != nil {
		return err
	}
	resolverDigest, resolverRaw, err := adjudicationJSONLDigest(resolver)
	if err != nil {
		return err
	}
	if packetDigest != manifest.AuditPacketSetDigest || resolverDigest != manifest.ResolverMapSetDigest {
		return fmt.Errorf("audit build output digest mismatch")
	}
	manifestRaw, err := marshalAdjudicationAuditJSON(manifest)
	if err != nil {
		return err
	}
	if err := writeAdjudicationAtomic(filepath.Join(dir, adjudicationAuditPacketsFile), packetRaw); err != nil {
		return err
	}
	if err := writeAdjudicationAtomic(filepath.Join(dir, adjudicationAuditResolverMapFile), resolverRaw); err != nil {
		return err
	}
	return writeAdjudicationAtomic(filepath.Join(dir, adjudicationAuditManifestFile), manifestRaw)
}

func marshalAdjudicationAuditJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func loadAndValidateAdjudicationAuditBuild(dir string, requireFrozen bool) (adjudicationAuditManifest, []adjudicationAuditPacket, []adjudicationAuditResolverMapRecord, error) {
	var manifest adjudicationAuditManifest
	raw, err := os.ReadFile(filepath.Join(dir, adjudicationAuditManifestFile)) //nolint:gosec // operator-selected protocol artifact
	if err != nil {
		return manifest, nil, nil, fmt.Errorf("read audit manifest: %w", err)
	}
	if err := decodeStrictAdjudicationJSON(raw, &manifest); err != nil {
		return manifest, nil, nil, fmt.Errorf("decode audit manifest: %w", err)
	}
	if manifest.Schema != adjudicationAuditManifestSchema || manifest.ProtocolHash != adjudicationAuditManifestProtocolHash(manifest) ||
		!isDigest(manifest.ProtocolHash) || manifest.Normalizer != "ascii-alnum-lower-v1" ||
		manifest.QueueRule != "accepted-semantic-override-or-triggered-fallback-v1" ||
		manifest.EntailmentPromptDigest != adjudicationAuditPromptDigest(adjudicationAuditViewEntailment) ||
		manifest.FalsificationPromptDigest != adjudicationAuditPromptDigest(adjudicationAuditViewFalsification) ||
		manifest.ResolverDigest != adjudicationAuditResolverDigest() || !isDigest(manifest.ViewSeedDigest) {
		return manifest, nil, nil, fmt.Errorf("invalid audit manifest protocol")
	}
	packets, err := readAdjudicationAuditPackets(filepath.Join(dir, adjudicationAuditPacketsFile))
	if err != nil {
		return manifest, nil, nil, err
	}
	resolver, err := readAdjudicationAuditResolverMap(filepath.Join(dir, adjudicationAuditResolverMapFile))
	if err != nil {
		return manifest, nil, nil, err
	}
	if len(packets) != manifest.RiskCount || len(resolver) != manifest.QuestionCount ||
		manifest.OverrideCount+manifest.FallbackCount != manifest.RiskCount ||
		manifest.RiskCount+manifest.RetainCount != manifest.QuestionCount ||
		manifest.ViewCount != manifest.RiskCount*2 || manifest.PlannedCalls != manifest.ViewCount {
		return manifest, nil, nil, fmt.Errorf("audit manifest count mismatch")
	}
	packetByID := make(map[string]adjudicationAuditPacket, len(packets))
	for index, packet := range packets {
		if err := validateAdjudicationAuditPacket(packet, manifest.ProtocolHash); err != nil {
			return manifest, nil, nil, fmt.Errorf("validate audit packet %d: %w", index, err)
		}
		if packetByID[packet.PacketID].PacketID != "" ||
			(index > 0 && !adjudicationIdentityLess(packets[index-1].Conv, packets[index-1].Q, packet.Conv, packet.Q)) {
			return manifest, nil, nil, fmt.Errorf("duplicate or unsorted audit packets")
		}
		packetByID[packet.PacketID] = packet
	}
	riskCount := 0
	seenResolvers := make(map[string]bool, len(resolver))
	for index, record := range resolver {
		if err := validateAdjudicationAuditResolver(record, manifest.ProtocolHash); err != nil {
			return manifest, nil, nil, fmt.Errorf("validate audit resolver %d: %w", index, err)
		}
		if seenResolvers[record.PacketID] ||
			(index > 0 && !adjudicationIdentityLess(resolver[index-1].Conv, resolver[index-1].Q, record.Conv, record.Q)) {
			return manifest, nil, nil, fmt.Errorf("duplicate or unsorted audit resolver rows")
		}
		seenResolvers[record.PacketID] = true
		if record.Risk {
			riskCount++
			packet, ok := packetByID[record.PacketID]
			if !ok || packet.Conv != record.Conv || packet.Q != record.Q || packet.QuestionID != record.QuestionID {
				return manifest, nil, nil, fmt.Errorf("risk resolver lacks matching audit packet")
			}
			for viewIndex, view := range packet.Views {
				mapping := record.ViewMaps[viewIndex].SlotToGroup
				for _, candidate := range view.Candidates {
					groupDigest := mapping[candidate.Slot]
					matched := false
					for _, group := range record.Groups {
						if group.GroupDigest == groupDigest && group.RepresentativeAnswerDigest == adjudicationTextDigest(candidate.RepresentativeAnswer) {
							matched = true
						}
					}
					if !matched {
						return manifest, nil, nil, fmt.Errorf("provider candidate does not match resolver group")
					}
				}
			}
			entailmentOrder := make([]string, 0, len(record.ViewMaps[0].SlotToGroup))
			falsificationOrder := make([]string, 0, len(record.ViewMaps[1].SlotToGroup))
			for slotIndex := 0; slotIndex < len(record.ViewMaps[0].SlotToGroup); slotIndex++ {
				slot := fmt.Sprintf("A%d", slotIndex+1)
				entailmentOrder = append(entailmentOrder, record.ViewMaps[0].SlotToGroup[slot])
				falsificationOrder = append(falsificationOrder, record.ViewMaps[1].SlotToGroup[slot])
			}
			for slotIndex := range entailmentOrder {
				if falsificationOrder[slotIndex] != entailmentOrder[(slotIndex+1)%len(entailmentOrder)] {
					return manifest, nil, nil, fmt.Errorf("falsification view is not rotate-one deranged")
				}
			}
		}
	}
	if riskCount != manifest.RiskCount {
		return manifest, nil, nil, fmt.Errorf("audit risk resolver count mismatch")
	}
	packetDigest, _, err := adjudicationJSONLDigest(packets)
	if err != nil || packetDigest != manifest.AuditPacketSetDigest {
		return manifest, nil, nil, fmt.Errorf("audit packet set digest mismatch")
	}
	resolverDigest, _, err := adjudicationJSONLDigest(resolver)
	if err != nil || resolverDigest != manifest.ResolverMapSetDigest {
		return manifest, nil, nil, fmt.Errorf("audit resolver set digest mismatch")
	}
	if requireFrozen {
		if err := validateFrozenAdjudicationAuditParentReceipt(manifest.Parent); err != nil {
			return manifest, nil, nil, err
		}
		if manifest.QuestionCount != adjudicationAuditFrozenQuestionCount || manifest.RiskCount != adjudicationAuditFrozenRiskCount ||
			manifest.OverrideCount != adjudicationAuditFrozenOverrideCount || manifest.FallbackCount != adjudicationAuditFrozenFallbackCount ||
			manifest.RetainCount != adjudicationAuditFrozenRetainCount || manifest.ViewCount != adjudicationAuditFrozenViewCount ||
			manifest.PlannedCalls != adjudicationAuditFrozenViewCount {
			return manifest, nil, nil, fmt.Errorf("audit build does not match frozen denominators")
		}
	}
	return manifest, packets, resolver, nil
}

func validateFrozenAdjudicationAuditParentReceipt(receipt adjudicationAuditParentReceipt) error {
	if receipt.ProtocolHash != adjudicationAuditFrozenParentProtocolHash ||
		receipt.PacketSetDigest != adjudicationAuditFrozenParentPacketSetDigest ||
		receipt.DecisionSetDigest != adjudicationAuditFrozenParentDecisionSetDigest ||
		receipt.PromptDigest != adjudicationAuditFrozenParentPromptDigest ||
		receipt.ManifestRawDigest != adjudicationAuditFrozenParentManifestRawDigest ||
		receipt.PacketsRawDigest != adjudicationAuditFrozenParentPacketsRawDigest ||
		receipt.CallsRawDigest != adjudicationAuditFrozenParentCallsRawDigest ||
		receipt.DecisionsRawDigest != adjudicationAuditFrozenParentDecisionsRawDigest ||
		receipt.SealRawDigest != adjudicationAuditFrozenParentSealRawDigest ||
		receipt.QuestionCount != 1540 || receipt.TriggeredCount != 771 || receipt.SelectedCount != 718 ||
		receipt.FallbackCount != 822 || receipt.ProviderAttempts != 771 || receipt.Retries != 0 {
		return fmt.Errorf("parent receipt does not match frozen paid 034 Stage-0")
	}
	return nil
}

func readAdjudicationAuditDecisions(path string) ([]adjudicationAuditDecision, error) {
	var decisions []adjudicationAuditDecision
	err := scanAdjudicationAuditJSONL(path, func(line int, raw []byte) error {
		var decision adjudicationAuditDecision
		if err := decodeStrictAdjudicationJSON(raw, &decision); err != nil {
			return fmt.Errorf("decode audit decision line %d: %w", line, err)
		}
		decisions = append(decisions, decision)
		return nil
	})
	return decisions, err
}

func validateAdjudicationAuditSeal(dir string, manifest adjudicationAuditManifest, packets []adjudicationAuditPacket, resolver []adjudicationAuditResolverMapRecord, requireFrozen bool) (adjudicationAuditSeal, []adjudicationAuditDecision, error) {
	var seal adjudicationAuditSeal
	raw, err := os.ReadFile(filepath.Join(dir, adjudicationAuditSealFile)) //nolint:gosec // protocol artifact path
	if err != nil {
		return seal, nil, fmt.Errorf("read audit seal: %w", err)
	}
	if err := decodeStrictAdjudicationJSON(raw, &seal); err != nil {
		return seal, nil, fmt.Errorf("decode audit seal: %w", err)
	}
	decisions, err := readAdjudicationAuditDecisions(filepath.Join(dir, adjudicationAuditDecisionsFile))
	if err != nil {
		return seal, nil, err
	}
	callPath := filepath.Join(dir, adjudicationAuditCallsFile)
	if _, err := os.Stat(callPath); err != nil {
		return seal, nil, fmt.Errorf("read audit calls: %w", err)
	}
	journal, err := openAdjudicationAuditCallJournal(callPath, manifest.ProtocolHash, packets)
	if err != nil {
		return seal, nil, err
	}
	if err := journal.Close(); err != nil {
		return seal, nil, err
	}
	terminals := journal.SortedTerminals()
	callStateDigest, _, err := adjudicationJSONLDigest(terminals)
	if err != nil {
		return seal, nil, err
	}
	decisionSetDigest, _, err := adjudicationJSONLDigest(decisions)
	if err != nil {
		return seal, nil, err
	}
	if len(decisions) != len(resolver) || len(decisions) != manifest.QuestionCount || len(terminals) != manifest.PlannedCalls {
		return seal, nil, fmt.Errorf("audit seal decision/call cardinality mismatch")
	}
	packetByID := make(map[string]adjudicationAuditPacket, len(packets))
	terminalsByPacket := make(map[string][]adjudicationAuditCallRecord, len(packets))
	for _, packet := range packets {
		packetByID[packet.PacketID] = packet
	}
	config := adjudicationRunConfig{
		Provider: seal.Provider, BaseURLDigest: seal.BaseURLDigest, Model: seal.Model, ModelRevision: seal.ModelRevision,
		MaxTokens: seal.MaxTokens, BinaryDigest: seal.BinaryDigest,
	}
	runIdentity := adjudicationAuditRunIdentityDigest(config, manifest)
	failureCounts := make(map[string]int)
	inputTokens, outputTokens := 0, 0
	for _, terminal := range terminals {
		packet, ok := packetByID[terminal.PacketID]
		view, viewOK := findAdjudicationAuditView(packet, terminal.ViewID)
		if !ok || !viewOK || terminal.InputDigest != adjudicationAuditInputDigest(packet, view, runIdentity) {
			return seal, nil, fmt.Errorf("audit seal call identity mismatch")
		}
		terminalsByPacket[terminal.PacketID] = append(terminalsByPacket[terminal.PacketID], terminal)
		inputTokens += terminal.InputTokens
		outputTokens += terminal.OutputTokens
		if terminal.State == adjudicationAuditCallFailed {
			failureCounts[terminal.FailureReason]++
		}
	}
	resolutionCounts := make(map[string]int)
	retained, switched, attempts := 0, 0, 0
	for index, decision := range decisions {
		record := resolver[index]
		if decision.PacketID != record.PacketID || decision.Conv != record.Conv || decision.Q != record.Q ||
			decision.QuestionID != record.QuestionID || decision.DecisionDigest != adjudicationAuditDecisionDigest(decision) ||
			(index > 0 && !adjudicationIdentityLess(decisions[index-1].Conv, decisions[index-1].Q, decision.Conv, decision.Q)) {
			return seal, nil, fmt.Errorf("audit decision identity/order/digest mismatch")
		}
		var expected adjudicationAuditDecision
		if record.Risk {
			expected, err = resolveAdjudicationAuditDecision(record, packetByID[record.PacketID], terminalsByPacket[record.PacketID])
			if err != nil {
				return seal, nil, err
			}
		} else {
			expected = retainedNonriskAdjudicationAuditDecision(record)
		}
		if !reflect.DeepEqual(decision, expected) {
			return seal, nil, fmt.Errorf("audit decision differs from frozen resolver")
		}
		resolutionCounts[decision.Resolution]++
		attempts += decision.ProviderAttempts
		if decision.Resolution == adjudicationAuditResolutionSwitched {
			switched++
		} else {
			retained++
		}
	}
	started, completed, failed := journal.Stats()
	if seal.Schema != adjudicationAuditSealSchema || !seal.Valid || seal.SealDigest != adjudicationAuditSealDigest(seal) ||
		seal.ProtocolHash != manifest.ProtocolHash || !reflect.DeepEqual(seal.Parent, manifest.Parent) ||
		seal.AuditPacketSetDigest != manifest.AuditPacketSetDigest || seal.ResolverMapSetDigest != manifest.ResolverMapSetDigest ||
		seal.CanonicalCallStateDigest != callStateDigest || seal.DecisionSetDigest != decisionSetDigest ||
		seal.EntailmentPromptDigest != manifest.EntailmentPromptDigest || seal.FalsificationPromptDigest != manifest.FalsificationPromptDigest ||
		seal.ResolverDigest != manifest.ResolverDigest || strings.TrimSpace(seal.Provider) == "" ||
		strings.TrimSpace(seal.Model) == "" || strings.TrimSpace(seal.ModelRevision) == "" ||
		!isDigest(seal.BaseURLDigest) || !isDigest(seal.BinaryDigest) || seal.MaxTokens < 1 ||
		seal.QuestionCount != manifest.QuestionCount || seal.RiskCount != manifest.RiskCount ||
		seal.ViewCount != manifest.ViewCount || seal.PlannedCalls != manifest.PlannedCalls ||
		seal.StartedCalls != started || seal.TerminalCalls != completed+failed || seal.CompletedCalls != completed ||
		seal.FailedCalls != failed || seal.ProviderAttempts != attempts || seal.Retries != 0 ||
		seal.DecisionCount != len(decisions) || seal.RetainedCount != retained || seal.SwitchedCount != switched ||
		!reflect.DeepEqual(seal.ResolutionCounts, resolutionCounts) || !reflect.DeepEqual(seal.FailureCounts, failureCounts) ||
		seal.InputTokens != inputTokens || seal.OutputTokens != outputTokens ||
		started != manifest.PlannedCalls || completed+failed != manifest.PlannedCalls || attempts != manifest.PlannedCalls {
		return seal, nil, fmt.Errorf("audit seal receipt mismatch")
	}
	switch seal.PricingStatus {
	case "unpriced":
		if seal.InputCNYPerMillion != nil || seal.OutputCNYPerMillion != nil || seal.EstimatedCNY != nil {
			return seal, nil, fmt.Errorf("unpriced audit seal contains a numeric cost")
		}
	case "priced", "declared_zero":
		if seal.InputCNYPerMillion == nil || seal.OutputCNYPerMillion == nil || seal.EstimatedCNY == nil ||
			*seal.InputCNYPerMillion < 0 || *seal.OutputCNYPerMillion < 0 {
			return seal, nil, fmt.Errorf("priced audit seal is incomplete")
		}
		want := (float64(inputTokens)*(*seal.InputCNYPerMillion) + float64(outputTokens)*(*seal.OutputCNYPerMillion)) / 1_000_000
		if math.Abs(*seal.EstimatedCNY-want) > 1e-12 ||
			(seal.PricingStatus == "declared_zero" && (*seal.InputCNYPerMillion != 0 || *seal.OutputCNYPerMillion != 0)) {
			return seal, nil, fmt.Errorf("audit seal pricing receipt mismatch")
		}
	default:
		return seal, nil, fmt.Errorf("unknown audit seal pricing status")
	}
	if requireFrozen && (manifest.QuestionCount != adjudicationAuditFrozenQuestionCount ||
		manifest.RiskCount != adjudicationAuditFrozenRiskCount || manifest.PlannedCalls != adjudicationAuditFrozenViewCount) {
		return seal, nil, fmt.Errorf("audit seal does not match frozen denominators")
	}
	return seal, decisions, nil
}
