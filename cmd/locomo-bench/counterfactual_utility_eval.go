package main

// 042 stage runners and label-blind boundaries. The per-stage implementations
// land in later user stories:
//
//	US1 (Phase 3): runUtilityLabelStage   — zero-model historical label audit
//	US2 (Phase 4): runUtilityPilotStage / runUtilityCollectStage /
//	               runUtilityDiagnoseStage
//	US3 (Phase 5): runUtilityConfirmStage / runUtilityTransferStage
//
// This file currently holds the runners' dispatch contracts; each stage's
// implementation is added with its failing tests.

import "fmt"

// runUtilityLabelStage performs the zero-model historical label-constructor
// audit (56 BENEFIT / 31 HARM / 1453 NEUTRAL). Implemented in US1.
func runUtilityLabelStage(opt *options) error {
	if opt.utilityShallowSource == "" || opt.utilityDeepSource == "" {
		return fmt.Errorf("label stage requires historical k30 and k150 run roots")
	}
	return fmt.Errorf("label stage not yet implemented")
}

func runUtilityPilotStage(opt *options) error {
	return fmt.Errorf("pilot stage not yet implemented")
}

func runUtilityCollectStage(opt *options) error {
	return fmt.Errorf("collect stage not yet implemented")
}

func runUtilityDiagnoseStage(opt *options) error {
	if opt.utilitySource == "" {
		return fmt.Errorf("diagnose stage requires a sealed collect source directory")
	}
	return fmt.Errorf("diagnose stage not yet implemented")
}

func runUtilityConfirmStage(opt *options) error {
	return fmt.Errorf("confirm stage not yet implemented")
}

func runUtilityTransferStage(opt *options) error {
	return fmt.Errorf("transfer stage not yet implemented")
}
