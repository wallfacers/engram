package main

// T029/T034 seal half — batch-level validation of the author/review provenance
// aggregate that feeds the dataset seal: host-stable non-`unavailable`
// resolved models (three distinct host harnesses; hosts may share one
// underlying model by maintainer decision 2026-09-01), OpenCode free billing,
// one author + one review prompt digest per batch, complete isolation-probe
// aggregation over every launched attempt, and unique ephemeral state roots
// (contracts/dataset-protocol.md §4, §7.1).

import (
	"errors"
	"fmt"
	"sort"
)

// ValidateHoldoutResolvedModels enforces the 2026-09-01 revised invariant:
// every host lane resolves to exactly one stable non-`unavailable` model
// across all its author and reviewer attempts. Identical resolved models
// across different hosts are recorded fact, not failure; the host lanes
// themselves must be the three distinct harnesses.
func ValidateHoldoutResolvedModels(models map[string]*string) error {
	if len(models) != 3 {
		return fmt.Errorf("resolved-model aggregate covers %d hosts, want 3", len(models))
	}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		m, ok := models[h]
		if !ok || m == nil {
			return fmt.Errorf("host %s missing its stable resolved model", h)
		}
		if *m == "" || *m == "unavailable" {
			return fmt.Errorf("host %s resolved model %q is empty/unavailable", h, *m)
		}
	}
	return nil
}

// ValidateHoldoutBilling enforces that every authoring/review provenance
// record of the batch declares an honest billing class (contract v4.4: the
// three lanes were unified onto the maintainer's existing authorized Bailian
// qwen3.8-flash endpoint on 2026-09-01, so the original "OpenCode rides a
// free model" assumption no longer describes reality; the gate now pins the
// record to a declared class and forbids an unknown one, rather than
// requiring a specific class the configuration stopped matching).
func ValidateHoldoutBilling(provs []ToolProvenance) error {
	for _, p := range provs {
		switch p.BillingClass {
		case BillingAuthorized, BillingFree:
		default:
			return fmt.Errorf("host %s attempt billing_class %q is not a declared class", p.Host, p.BillingClass)
		}
	}
	return nil
}

// ValidatePromptConsistency enforces one author prompt digest and one review
// prompt digest across the whole batch (a batch cannot mix prompt digests).
func ValidatePromptConsistency(author, review []AuthoringPromptReceipt) error {
	for _, pair := range []struct {
		what  string
		rs    []AuthoringPromptReceipt
	}{
		{"author", author},
		{"review", review},
	} {
		if len(pair.rs) == 0 {
			return fmt.Errorf("no %s prompt receipts in the batch", pair.what)
		}
		for i, r := range pair.rs {
			if r.SHA256 == "" || r.DigestAlgorithm != "lf-normalized-sha256-v1" {
				return fmt.Errorf("%s prompt receipt %d malformed", pair.what, i)
			}
			if r.SHA256 != pair.rs[0].SHA256 {
				return fmt.Errorf("%s prompt digest drift in batch (%s vs %s)", pair.what, r.SHA256, pair.rs[0].SHA256)
			}
			if r.PromptID != pair.rs[0].PromptID || r.Version != pair.rs[0].Version {
				return fmt.Errorf("%s prompt identity drift in batch", pair.what)
			}
		}
	}
	return nil
}

// AggregateIsolationReceipts proves every launched attempt (any terminal
// outcome, including launch failure, parse error, timeout, rejection) carries
// a complete isolation probe set. Omitting a rejected or failed attempt fails
// the aggregate.
func AggregateIsolationReceipts(ledger *AuthorReviewAttemptLedgerV1, probes map[string][]AccessProbe) error {
	launched := map[string]bool{}
	for _, e := range ledger.Events {
		if e.EventKind == EventAttemptStarted {
			launched[e.AttemptID] = true
		}
	}
	var have []string
	for id := range probes {
		have = append(have, id)
	}
	sort.Strings(have)
	for id := range launched {
		ps, ok := probes[id]
		if !ok {
			return fmt.Errorf("launched attempt %s has no isolation receipts in the aggregate (have: %v)", id, have)
		}
		if err := ValidateIsolationProbes(StageKind(stageOf(ledger, id)), ps); err != nil {
			return fmt.Errorf("attempt %s: %w", id, err)
		}
	}
	for id := range probes {
		if !launched[id] {
			return fmt.Errorf("isolation receipts for unknown attempt %s in the aggregate", id)
		}
	}
	return nil
}

func stageOf(ledger *AuthorReviewAttemptLedgerV1, attemptID string) string {
	for _, e := range ledger.Events {
		if e.AttemptID == attemptID && e.EventKind == EventAttemptStarted {
			return e.Stage
		}
	}
	return ""
}

// ValidateStateRootsUnique enforces the ephemeral-root rule: no author/review
// state root may repeat within the batch.
func ValidateStateRootsUnique(roots []string) error {
	seen := map[string]bool{}
	for _, r := range roots {
		if r == "" {
			return errors.New("empty state root in batch")
		}
		if seen[r] {
			return fmt.Errorf("state root %s reused across attempts", r)
		}
		seen[r] = true
	}
	return nil
}
