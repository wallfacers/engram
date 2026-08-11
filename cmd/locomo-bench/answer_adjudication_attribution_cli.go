package main

// 036 attribution CLI. Pure diagnostic: loads the validated 034 public
// artifacts + hidden verdict join, rebuilds per-question state, marks the gap,
// aggregates, optionally cross-references the 035 audit seal, and writes
// decision-gap-attribution.json atomically. Zero model calls, zero engine edits,
// default benchmark and 034/035 modes unchanged.

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// attributionMode is the adjudication-mode identifier for 036.
const attributionMode adjudicationMode = "attribution"

// runDecisionGapAttributionCLI is the 036 entry point: validate options, load
// validated public artifacts, load hidden verdicts (seal-gated), rebuild,
// aggregate, cross-audit, write report. (Named distinctly from the existing
// trace-attribution runAttributionCLI in attribution.go.)
func runDecisionGapAttributionCLI(ctx context.Context, opt options) error {
	if err := validateAdjudicationAttributionOptions(opt); err != nil {
		return err
	}
	dir := strings.TrimSpace(opt.adjudicationAttributionDir)
	manifest, packets, err := loadAndValidateAdjudicationPublic(dir, false)
	if err != nil {
		return err
	}
	hidden, err := loadAdjudicationHiddenInputs(dir, opt.adjudicationCandidates)
	if err != nil {
		return err
	}
	decisions, err := readAdjudicationDecisions(joinPath(dir, adjudicationDecisionsFile))
	if err != nil {
		return err
	}
	if len(decisions) != manifest.QuestionCount {
		return fmt.Errorf("adjudication decisions count = %d, want %d", len(decisions), manifest.QuestionCount)
	}
	rows, err := buildAttributionRows(manifest, packets, decisions, hidden)
	if err != nil {
		return err
	}
	categories, summary := aggregateAttribution(rows)
	gaps, auditSource, err := crossAudit(rows.Gaps, strings.TrimSpace(opt.adjudicationAuditDir))
	if err != nil {
		return err
	}
	rows.Gaps = gaps
	report := attributionReport{
		Schema:      attributionReportSchema,
		ResultKind:  "decision_gap_attribution",
		Protocol:    manifest.ProtocolHash,
		Rows:        rows,
		Categories:  categories,
		Summary:     summary,
		AuditSource: auditSource,
	}
	if err := writeAttributionReport(dir, report); err != nil {
		return err
	}
	fmt.Printf("attribution: gaps=%d control_only_loss=%d both_wrong=%d categories=%d audit=%s dominant=%s\n",
		summary.GapCount, summary.ControlOnlyLoss, summary.BothWrong, len(categories),
		auditSource, summary.DominantMode)
	return nil
}

// validateAdjudicationAttributionOptions enforces the attribution mode's
// argument contract: exactly one 034 dir, exactly three candidates, no
// paid/seed/trace/max-token, optional audit dir.
func validateAdjudicationAttributionOptions(opt options) error {
	if _, err := adjudicationModeFor(opt); err != nil {
		return err
	}
	if strings.TrimSpace(opt.adjudicationAttributionDir) == "" || len(opt.adjudicationCandidates) != 3 {
		return fmt.Errorf("attribution requires DIR and exactly three candidates")
	}
	// Reject only mode-foreign *explicit* options. adjudicationMaxTokens is a
	// 034 run-mode flag whose flag default is 512, so it must not gate the
	// attribution mode (same convention as 034/035 score/validate modes, which
	// ignore it entirely).
	if strings.TrimSpace(opt.adjudicationTracePath) != "" || strings.TrimSpace(opt.adjudicationSeed) != "" ||
		opt.adjudicationAllowPaid {
		return fmt.Errorf("attribution accepts only DIR, three candidates, and optional audit dir")
	}
	if strings.TrimSpace(opt.adjudicationAuditDir) != "" {
		// Audit dir must not be the 034 dir itself and must exist (read-only probe).
		if strings.TrimSpace(opt.adjudicationAuditDir) == strings.TrimSpace(opt.adjudicationAttributionDir) {
			return fmt.Errorf("attribution audit dir must differ from the 034 dir")
		}
		if _, err := os.Stat(opt.adjudicationAuditDir); err != nil {
			return fmt.Errorf("attribution audit dir: %w", err)
		}
	}
	return nil
}

// joinPath is a tiny local alias so attribution CLI reads are obvious and local.
func joinPath(dir, name string) string {
	return dir + string(os.PathSeparator) + name
}
