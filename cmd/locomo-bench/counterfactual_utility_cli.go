package main

// 042 CLI boundary. The utility protocol is a dedicated, default-off research
// mode of cmd/locomo-bench: --utility-stage empty means every --utility-* flag
// is a usage error and the ordinary benchmark path is byte-identical. Offline
// stages (label/diagnose) are dispatched before any provider/env resolution.

import (
	"fmt"
	"strings"
)

// utilityExperimentalFlags returns a display name of the first conflicting
// experimental mode found, or "" when none is set. Utility stages are mutually
// exclusive with modes that would change the answer path.
func utilityExperimentalFlags(opt *options) string {
	switch {
	case opt.nav:
		return "--nav"
	case opt.iris:
		return "--iris"
	case opt.unifiedAnswerContract:
		return "--unified-answer-contract"
	case opt.abstainPrompt:
		return "--abstain-prompt"
	case opt.pcic:
		return "--pcic"
	case opt.rerank:
		return "--rerank"
	case opt.multiQuery:
		return "--multi-query"
	case opt.recallDiagnostic:
		return "--recall-diagnostic"
	}
	return ""
}

// utilitySubsetSelector reports a subset/narrowing selector that formal utility
// stages reject (they must cover the full question set).
func utilitySubsetSelector(opt *options) string {
	switch {
	case opt.maxConvs > 0:
		return "--conversations"
	case opt.maxQuestions > 0:
		return "--questions"
	case opt.onlyCategory > 0:
		return "--only-category"
	case opt.onlyEnumeration:
		return "--only-enumeration"
	case len(opt.onlyQuestions) > 0:
		return "--only-questions"
	case opt.adversarial > 0:
		return "--adversarial"
	}
	return ""
}

// validateUtilityCLIOptions validates the utility-related fields of opt. When
// --utility-stage is empty it returns nil unless an auxiliary --utility-* flag
// is set (set-but-ignored is a usage error). It also checks per-stage required
// and forbidden inputs and the frozen k values.
func validateUtilityCLIOptions(opt *options) error {
	stage := strings.TrimSpace(opt.utilityStageFlag)
	if stage == "" {
		// Any auxiliary flag without --utility-stage is a usage error.
		if opt.utilitySource != "" || opt.utilityLabelSource != "" || opt.utilityPilotSource != "" ||
			opt.utilityShallowSource != "" || opt.utilityDeepSource != "" ||
			opt.utilityShallowK != 0 || opt.utilityDeepK != 0 {
			return fmt.Errorf("--utility-* flags require --utility-stage")
		}
		return nil
	}
	st, err := parseUtilityStage(stage)
	if err != nil {
		return err
	}

	// Mutual exclusivity with experimental answer-path modes.
	if conflict := utilityExperimentalFlags(opt); conflict != "" {
		return fmt.Errorf("--utility-stage %s cannot be combined with %s", st, conflict)
	}
	// Formal stages fix retrieval to hybrid (no multiple arms).
	if opt.retrieval != "" && opt.retrieval != "hybrid" {
		return fmt.Errorf("--utility-stage %s fixes retrieval to hybrid, got %q", st, opt.retrieval)
	}

	// Frozen retrieval depths for formal stages.
	shallowK := opt.utilityShallowK
	if shallowK == 0 {
		shallowK = utilityShallowK
	}
	deepK := opt.utilityDeepK
	if deepK == 0 {
		deepK = utilityDeepK
	}
	if shallowK != utilityShallowK || deepK != utilityDeepK {
		return fmt.Errorf("--utility-stage %s fixes k=%d/%d, got k=%d/%d", st, utilityShallowK, utilityDeepK, shallowK, deepK)
	}
	if opt.runDir == "" {
		return fmt.Errorf("--utility-stage %s requires --run-dir", st)
	}

	switch st {
	case utilityStageLabel:
		if opt.utilityShallowSource == "" || opt.utilityDeepSource == "" {
			return fmt.Errorf("--utility-stage label requires --utility-shallow-source and --utility-deep-source")
		}
		if opt.dataPath != "" {
			return fmt.Errorf("--utility-stage label must not read --data")
		}
		if opt.storeDir != "" {
			return fmt.Errorf("--utility-stage label must not read --store-dir")
		}
	case utilityStagePilot:
		if opt.utilityLabelSource == "" {
			return fmt.Errorf("--utility-stage pilot requires --utility-label-source (label GO)")
		}
		if err := utilityModelStageInputs(opt, st); err != nil {
			return err
		}
	case utilityStageCollect:
		if opt.utilityLabelSource == "" {
			return fmt.Errorf("--utility-stage collect requires --utility-label-source (label GO)")
		}
		if opt.utilityPilotSource == "" {
			return fmt.Errorf("--utility-stage collect requires --utility-pilot-source (pilot GO)")
		}
		if err := utilityModelStageInputs(opt, st); err != nil {
			return err
		}
	case utilityStageDiagnose:
		if opt.utilitySource == "" {
			return fmt.Errorf("--utility-stage diagnose requires --utility-source (collect dir)")
		}
		if opt.dataPath != "" {
			return fmt.Errorf("--utility-stage diagnose must not read --data")
		}
		if opt.storeDir != "" {
			return fmt.Errorf("--utility-stage diagnose must not read --store-dir")
		}
	case utilityStageConfirm:
		if opt.utilitySource == "" {
			return fmt.Errorf("--utility-stage confirm requires --utility-source (diagnose GO dir)")
		}
		if err := utilityModelStageInputs(opt, st); err != nil {
			return err
		}
	case utilityStageTransfer:
		if opt.utilitySource == "" {
			return fmt.Errorf("--utility-stage transfer requires --utility-source (confirm GO dir)")
		}
		if opt.datasetFormat != "" && opt.datasetFormat != "longmemeval" {
			return fmt.Errorf("--utility-stage transfer fixes --dataset-format longmemeval, got %q", opt.datasetFormat)
		}
		if err := utilityModelStageInputs(opt, st); err != nil {
			return err
		}
	}
	return nil
}

// utilityModelStageInputs validates the common required inputs of the model
// stages (pilot/collect/confirm/transfer): dataset, store, hybrid recipe flags,
// and the frozen mem0-aligned clean-final judge regime. Formal runs also reject
// subset selectors.
func utilityModelStageInputs(opt *options, st utilityStage) error {
	if opt.dataPath == "" {
		return fmt.Errorf("--utility-stage %s requires --data", st)
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--utility-stage %s requires --store-dir", st)
	}
	if !opt.judgeMem0Aligned {
		return fmt.Errorf("--utility-stage %s requires --judge-mem0-aligned (frozen clean-final regime)", st)
	}
	if !opt.chunks {
		return fmt.Errorf("--utility-stage %s requires --chunks (frozen hybrid chunk recipe)", st)
	}
	if opt.repeats != 0 && opt.repeats != utilityRepetitions {
		return fmt.Errorf("--utility-stage %s fixes --repeats %d, got %d", st, utilityRepetitions, opt.repeats)
	}
	if sel := utilitySubsetSelector(opt); sel != "" {
		return fmt.Errorf("--utility-stage %s must cover the full question set, rejects %s", st, sel)
	}
	return nil
}

// runUtilityCLI is the early-dispatch entry: it runs the selected stage without
// touching the ordinary benchmark path. Offline stages require no provider env.
func runUtilityCLI(opt *options) error {
	st, err := parseUtilityStage(opt.utilityStageFlag)
	if err != nil {
		return err
	}
	switch st {
	case utilityStageLabel:
		return runUtilityLabelStage(opt)
	case utilityStagePilot:
		return runUtilityPilotStage(opt)
	case utilityStageCollect:
		return runUtilityCollectStage(opt)
	case utilityStageDiagnose:
		return runUtilityDiagnoseStage(opt)
	case utilityStageConfirm:
		return runUtilityConfirmStage(opt)
	case utilityStageTransfer:
		return runUtilityTransferStage(opt)
	}
	return fmt.Errorf("unhandled utility stage %s", st)
}

// The per-stage runners are implemented in counterfactual_utility_eval.go
// (label in US1, collect/pilot in US2, confirm/transfer in US3).
