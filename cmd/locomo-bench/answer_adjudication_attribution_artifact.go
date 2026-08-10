package main

// 036 attribution report artifact schemas and read/write helpers. The report is
// benchmark-only diagnostic output (decision_gap_attribution), never a formal
// LoCoMo score. It is written atomically to the 034 stage directory.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// attributionReportSchema is the canonical JSON schema id for 036 reports.
const attributionReportSchema = "036.decision-gap-attribution.v1"

// attributionReportFile is the on-disk name of the attribution report inside
// the 034 stage directory.
const attributionReportFile = "decision-gap-attribution.json"

// attributionReport is the full deliverable of the 036 feature.
type attributionReport struct {
	Schema      string                         `json:"schema"`
	ResultKind  string                         `json:"result_kind"`
	Protocol    string                         `json:"protocol_hash"`
	Rows        *attributionRows               `json:"rows"`
	Categories  []attributionCategoryAggregate `json:"categories"`
	Summary     attributionSummary             `json:"summary"`
	AuditSource string                         `json:"audit_source,omitempty"`
}

// crossAudit attaches 035 audit status to each gap row by reading the 035 audit
// seal's resolver map (slot→group) and call journal (per-slot assessments).
// Best-effort: when the 035 dir or seal is missing/invalid, every gap row is
// marked audit_unavailable and the report still succeeds (US3 degradation
// contract, FR-006).
//
// parentRefutedAnyView is true when any view's assessment of the parent answer
// group is contradiction=yes with support!=yes. uniqueAlternative is true when
// exactly one non-parent group is supported and not contradicted in any view
// (consistent with the 035 resolver's current-refuted rule).
func crossAudit(gaps []attributionGapRow, auditDir string) ([]attributionGapRow, string, error) {
	if strings.TrimSpace(auditDir) == "" {
		for i := range gaps {
			gaps[i].AuditUnavailable = true
		}
		return gaps, "", nil
	}
	resolvers, calls, err := readAdjudicationAuditForCrossAudit(auditDir)
	if err != nil {
		for i := range gaps {
			gaps[i].AuditUnavailable = true
		}
		return gaps, "", nil // degrade, do not fail the diagnostic
	}
	resolverByPacket := make(map[string]adjudicationAuditResolverMapRecord, len(resolvers))
	for _, record := range resolvers {
		resolverByPacket[record.PacketID] = record
	}
	callsByPacket := make(map[string][]adjudicationAuditCallRecord, len(calls))
	for _, call := range calls {
		callsByPacket[call.PacketID] = append(callsByPacket[call.PacketID], call)
	}
	for i := range gaps {
		record, ok := resolverByPacket[gaps[i].PacketID]
		if !ok {
			gaps[i].AuditUnavailable = true
			continue
		}
		packetCalls := callsByPacket[gaps[i].PacketID]
		if len(packetCalls) == 0 {
			gaps[i].AuditUnavailable = true
			continue
		}
		refuted, unique := auditViewSignals(record, packetCalls)
		risk := record.Risk
		gaps[i].InRiskQueue = &risk
		gaps[i].ParentRefutedAnyView = &refuted
		gaps[i].UniqueAlternative = &unique
	}
	return gaps, "035-audit", nil
}

// auditViewSignals folds the 035 resolver record and per-view call assessments
// into the cross-audit booleans, mirroring the 035 resolver semantics in
// resolveAdjudicationAuditDecision (audit.go). For each view, map the
// assessment slots to group digests via the view map, then:
//
//	refuted   := parent group contradiction=yes && support!=yes
//	unique    := exactly one non-parent group support=yes && contradiction!=yes
//
// A view with a failed/invalid call contributes no signal. refuted is true if
// any view refutes the parent; unique is true only if exactly one non-parent
// group is the uniquely supported alternative in at least one view.
func auditViewSignals(record adjudicationAuditResolverMapRecord, calls []adjudicationAuditCallRecord) (bool, bool) {
	refuted := false
	unique := false
	viewMapByID := make(map[string]adjudicationAuditViewMap, len(record.ViewMaps))
	for _, vm := range record.ViewMaps {
		viewMapByID[vm.ViewID] = vm
	}
	for _, call := range calls {
		if call.State != adjudicationAuditCallCompleted {
			continue
		}
		vm, ok := viewMapByID[call.ViewID]
		if !ok {
			continue
		}
		assessmentByGroup := make(map[string]adjudicationAuditCandidateAssessment, len(call.Assessments))
		seen := make(map[string]bool, len(call.Assessments))
		for _, assessment := range call.Assessments {
			groupDigest := vm.SlotToGroup[assessment.Slot]
			if groupDigest == "" || seen[groupDigest] {
				continue
			}
			seen[groupDigest] = true
			assessmentByGroup[groupDigest] = assessment
		}
		current := assessmentByGroup[record.ParentSelectedGroupDigest]
		if current.Contradiction.Value == "yes" && current.Support.Value != "yes" {
			refuted = true
		}
		alternatives := make([]string, 0, 2)
		for groupDigest, assessment := range assessmentByGroup {
			if groupDigest == record.ParentSelectedGroupDigest {
				continue
			}
			if assessment.Support.Value == "yes" && assessment.Contradiction.Value != "yes" {
				alternatives = append(alternatives, groupDigest)
			}
		}
		if len(alternatives) == 1 {
			unique = true
		}
	}
	return refuted, unique
}

// readAdjudicationAuditForCrossAudit reads the 035 resolver map (JSONL) and the
// label-free call journal (JSONL). Additive: never opens hidden verdict/custody/score.
func readAdjudicationAuditForCrossAudit(auditDir string) ([]adjudicationAuditResolverMapRecord, []adjudicationAuditCallRecord, error) {
	resolvers, err := readJSONLRecords[adjudicationAuditResolverMapRecord](filepath.Join(auditDir, adjudicationAuditResolverMapFile))
	if err != nil {
		return nil, nil, err
	}
	calls, err := readJSONLRecords[adjudicationAuditCallRecord](filepath.Join(auditDir, adjudicationAuditCallsFile))
	if err != nil {
		return nil, nil, err
	}
	return resolvers, calls, nil
}

// readJSONLRecords decodes a JSONL file into typed records.
func readJSONLRecords[T any](path string) ([]T, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // score-only artifact path
	if err != nil {
		return nil, err
	}
	var records []T
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		if err := json.Unmarshal(raw, &records); err != nil {
			return nil, fmt.Errorf("decode %s as JSON array: %w", path, err)
		}
		return records, nil
	}
	scanner := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, line := range scanner {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record T
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("decode JSONL record in %s: %w", path, err)
		}
		records = append(records, record)
	}
	return records, nil
}

// writeAttributionReport writes the report atomically to dir. It refuses to
// overwrite an existing report.
func writeAttributionReport(dir string, report attributionReport) error {
	if _, err := os.Stat(filepath.Join(dir, attributionReportFile)); err == nil {
		return fmt.Errorf("attribution report refuses existing %s", attributionReportFile)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := writeJSON(filepath.Join(dir, attributionReportFile), report); err != nil {
		return err
	}
	return nil
}
