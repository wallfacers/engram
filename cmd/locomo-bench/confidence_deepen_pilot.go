package main

// 043 confidence-gated deepening signal pilot — box-side stage.
//
// T001-T009 wire only the stage signature and fail-closed body: the 2-conv
// dual-signal AUC pilot needs a real sidecar env (vLLM logprob answerer +
// hybrid retriever + judge) and is executed on the AutoDL box by the maintainer
// in a later box phase (T010). Until the real runner lands, any invocation of
// --deepen-pilot signal fails closed with a clear box-required error — it can
// never silently produce a partial pilot artifact.

import (
	"fmt"
)

// runDeepenSignalPilotStage is the --deepen-pilot signal stage entry. The
// full body (manifest → buildConversationRuntime → worker pool answering →
// dual-signal AUC → GO/NO-GO seal, mirroring runUtilityPilotStage) is box-side
// work scheduled after the local pure-function layer (T010).
func runDeepenSignalPilotStage(opt *options) error {
	return fmt.Errorf("deepen signal pilot stage requires the AutoDL box sidecar env (T010); the local layer wires only the CLI boundary")
}
