package main

// 030 US2 fail-closed gate (specs/030, contracts/grounded-trace.md §Fail-Closed,
// data-model.md FailClosedGate). Deterministic, pure Go, offline-testable: it
// validates the sidecar packet and keeps every citation inside the closed
// candidate boundary C_q. Degradation order: parse_failed (caller retries once)
// → invalid citations dropped → empty evidence → fallback. The engine is
// untouched (FR-001).

import "encoding/json"

// mediateTrace runs the fail-closed gate over a raw sidecar packet. It returns
// the (possibly filtered) packet, the final evidence E exposed to the answerer,
// and the gate status. The returned status drives the caller's retry/fallback
// decisions:
//
//   - traceGateParseFailed: raw is not a valid tracePacket. Caller MAY retry
//     once (extended budget), then falls back.
//   - traceGateInvalidCitation: at least one evidence/action cited an ID outside
//     C_q or was not traceable, and was dropped; remaining E is non-empty.
//   - traceGateFallback: after dropping, E is empty — caller must fall back to
//     the US1 assembled path.
//   - traceGateValid: everything passed; E is the answer context.
func mediateTrace(input traceMediationInput) (tracePacket, []traceEvidence, traceGateStatus, error) {
	var pkt tracePacket
	if err := json.Unmarshal([]byte(input.Raw), &pkt); err != nil {
		return pkt, nil, traceGateParseFailed, nil
	}

	traceIDs := traceStepIDSet(pkt.Trace)
	dropped := false

	// Evidence: closed-boundary citation (≥1 ID, all inside C_q) AND traceable
	// to at least one trace step's cited IDs.
	keptEvidence := make([]traceEvidence, 0, len(pkt.Evidence))
	for _, ev := range pkt.Evidence {
		if !idsInside(ev.CitedIDs, input.CandidateIDs) {
			dropped = true
			continue
		}
		if !overlapsAny(ev.CitedIDs, traceIDs) {
			dropped = true
			continue
		}
		keptEvidence = append(keptEvidence, ev)
	}

	// Actions: closed-boundary citation (audit; DROP actions never produce
	// evidence but must not cite outside the boundary either).
	keptActions := make([]traceAction, 0, len(pkt.Actions))
	for _, a := range pkt.Actions {
		if !idsInside(a.CitedIDs, input.CandidateIDs) {
			dropped = true
			continue
		}
		keptActions = append(keptActions, a)
	}

	pkt.Evidence = keptEvidence
	pkt.Actions = keptActions
	if len(keptEvidence) == 0 {
		return pkt, nil, traceGateFallback, nil
	}
	status := traceGateValid
	if dropped {
		status = traceGateInvalidCitation
	}
	return pkt, keptEvidence, status, nil
}

// idsInside reports whether ids is non-empty and every ID lies inside the
// closed candidate boundary. An empty citation set fails: every trace/evidence
// step must cite at least one retrieved candidate (contracts §closed boundary).
func idsInside(ids []string, boundary map[string]bool) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if !boundary[id] {
			return false
		}
	}
	return true
}

// traceStepIDSet collects every ID cited across all trace steps.
func traceStepIDSet(steps []traceStep) map[string]bool {
	set := make(map[string]bool)
	for _, s := range steps {
		for _, id := range s.CitedIDs {
			set[id] = true
		}
	}
	return set
}

// overlapsAny reports whether any ID of ids appears in set.
func overlapsAny(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if set[id] {
			return true
		}
	}
	return false
}
