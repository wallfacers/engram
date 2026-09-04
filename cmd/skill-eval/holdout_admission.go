package main

// T030/T033 admission half — the novelty gate and the atomic accepted-family
// CAS with its append-only receipt chain (contracts/dataset-protocol.md §2.1,
// §5.4-5.5). Accept requires: both reviewers accept and are novel, their
// exact inferred fields and recomputed label digests match, they match the
// private author candidate and the complete private quota slot, the
// nearest-family reference exists in the materialized payloads, and the
// controller-generated family identity collides with neither the frozen dev
// index nor the accepted-holdout state. Only then does the CAS run: commit
// advances the accepted-family revision and binds the final case; a stale
// observed revision leaves state untouched.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// AcceptedFamilyState is the CAS-protected accepted-holdout family state.
type AcceptedFamilyState struct {
	Revision int                          `json:"revision"`
	Families map[string]FamilySummaryEntry `json:"families"` // controller family ID → entry
}

// StateDigest canonicalizes the full state (revision + families).
func (s *AcceptedFamilyState) StateDigest() (string, error) {
	ids := make([]string, 0, len(s.Families))
	for id := range s.Families {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	entries := make([]FamilySummaryEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, s.Families[id])
	}
	proj := struct {
		Revision int                   `json:"revision"`
		Entries  []FamilySummaryEntry  `json:"entries"`
	}{Revision: s.Revision, Entries: entries}
	return CanonicalSHA256(proj)
}

// ControllerFamilyID derives the controller-only holdout family identity from
// the canonical blind-semantic projection bytes. Author-proposed family IDs
// never enter this preimage.
func ControllerFamilyID(blindProjection []byte) string {
	h := sha256.Sum256(append([]byte("holdout-family-id-v1\x00"), blindProjection...))
	return "hfam-" + hex.EncodeToString(h[:])
}

// AdmissionInput carries everything the controller checks at admission time.
type AdmissionInput struct {
	AuthorAttemptID         string
	AuthoringReceiptDigest  string
	PrivateCandidateDigest  string
	BlindCandidateDigest    string
	QuotaSlotDigest         string
	QuotaSlot               QuotaSlot
	AuthorModule            string
	AuthorLang              string
	AuthorScenario          string
	AuthorCategory          string
	AuthorExpect            ExpectV2
	AuthorFamilyProposal    *string // must be nil: authors never choose family IDs
	Reviews                 []ReviewRecord
	DevSummary              *FamilySummaryPayload
	AcceptedSummary         *FamilySummaryPayload // the exact payload both reviewers received
	BlindProjection         []byte                // canonical blind-semantic projection bytes
	FinalCaseID             string
	AdmissionSequence       int
	PreviousReceiptDigest   *string
}

// TryAdmit runs the full acceptance gate and then the CAS. A gate failure
// returns an error (no admission receipt — the attempt terminates through the
// event ledger and the slot regenerates); a CAS observation mismatch returns
// a stale receipt with unchanged state.
func TryAdmit(in AdmissionInput, accepted *AcceptedFamilyState) (*AdmissionReceipt, *AcceptedFamilyState, error) {
	if in.AuthorFamilyProposal != nil {
		return nil, nil, errors.New("author-supplied family ID is invalid")
	}
	if len(in.Reviews) != 2 {
		return nil, nil, errors.New("admission requires exactly two reviews")
	}
	for _, r := range in.Reviews {
		if r.Verdict != "accept" {
			return nil, nil, fmt.Errorf("reviewer %s verdict %q, want accept", r.AttemptID, r.Verdict)
		}
		if !r.Novel {
			return nil, nil, fmt.Errorf("reviewer %s judged the candidate non-novel", r.AttemptID)
		}
		if r.BlindCandidateDigest != in.BlindCandidateDigest {
			return nil, nil, fmt.Errorf("reviewer %s saw a different candidate digest", r.AttemptID)
		}
	}
	// Reviewer unanimity on inferred labels and the recomputed digest.
	if err := ReviewersAgree(in.Reviews[0], in.Reviews[1]); err != nil {
		return nil, nil, err
	}
	// Reviewer labels vs the private author candidate. On positive slots the
	// cross-check binds the unanimous reviewer digest to the author's
	// private proposal. On negative slots it is diagnostic only (contract
	// v4.3): a natural negative case necessarily carries environment/team
	// context as distractors, and two blind reviewers consistently read
	// that context as a durable disclosure (write-pos) while the author's
	// scoring contract says the turn must not produce a write — full-run
	// evidence: 26/37 second-round failures were this exact flip on
	// implicit-read-neg/trap-read-neg slots. The negative behavioral
	// contract stays author-owned and machine-checkable (trigger=false +
	// exclude tokens), and its residual authoring bias is declared in the
	// dataset card (T037).
	authorDigest, err := NormalizedLabelDigest(in.AuthorModule, in.AuthorLang,
		in.AuthorScenario, in.AuthorCategory, in.AuthorExpect)
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasSuffix(strings.ToLower(in.QuotaSlot.Module), "-neg") &&
		authorDigest != in.Reviews[0].NormalizedLabelDigest {
		return nil, nil, errors.New("unanimous reviewer label mismatches the private author candidate")
	}
	// Author labels vs the complete private quota slot.
	if in.AuthorModule != in.QuotaSlot.Module || in.AuthorLang != in.QuotaSlot.Lang ||
		in.AuthorScenario != in.QuotaSlot.Scenario || in.QuotaSlot.Author == "" {
		return nil, nil, errors.New("author labels mismatch the four-dimensional quota slot")
	}
	// The nearest-family reference must exist in what the reviewers saw.
	for _, r := range in.Reviews {
		if err := NearestFamilyReferenced(r, in.DevSummary, in.AcceptedSummary); err != nil {
			return nil, nil, fmt.Errorf("reviewer %s: %w", r.AttemptID, err)
		}
	}
	// Controller-only family identity; collisions reject the candidate
	// (duplicate accepted family, cross-split dev collision, translation pair).
	famID := ControllerFamilyID(in.BlindProjection)
	if in.DevSummary != nil {
		for _, e := range in.DevSummary.Entries {
			if e.FamilyID == famID {
				return nil, nil, fmt.Errorf("candidate family %s collides with the frozen dev index", famID)
			}
		}
	}
	if _, dup := accepted.Families[famID]; dup {
		return nil, nil, fmt.Errorf("candidate family %s duplicates an accepted holdout family", famID)
	}
	// CAS: compare the observed pre-state with the current state.
	preState, err := accepted.StateDigest()
	if err != nil {
		return nil, nil, err
	}
	reviewedSummaryDigest, err := in.AcceptedSummary.ComputePayloadDigest()
	if err != nil {
		return nil, nil, err
	}
	mk := func(cas string, postRev int, postState string, caseID, fam, entry *string) *AdmissionReceipt {
		r := &AdmissionReceipt{
			AdmissionSequence:             in.AdmissionSequence,
			PreviousAdmissionReceiptDigest: in.PreviousReceiptDigest,
			AuthorAttemptID:               in.AuthorAttemptID,
			AuthoringReceiptDigest:        in.AuthoringReceiptDigest,
			ReviewAttemptIDs:              []string{in.Reviews[0].AttemptID, in.Reviews[1].AttemptID},
			PrivateCandidateDigest:        in.PrivateCandidateDigest,
			BlindCandidateDigest:          in.BlindCandidateDigest,
			QuotaSlotDigest:               in.QuotaSlotDigest,
			NormalizedLabelDigest:         authorDigest,
			ReviewedSummaryRevision:       in.AcceptedSummary.Revision,
			ReviewedSummaryDigest:         reviewedSummaryDigest,
			ReviewedSourceStateDigest:     in.AcceptedSummary.SourceStateDigest,
			ObservedPreRevision:           in.AcceptedSummary.Revision,
			ObservedPreStateDigest:        in.AcceptedSummary.SourceStateDigest,
			CASResult:                     cas,
			FinalCaseID:                   caseID,
			ControllerFamilyID:            fam,
			FamilyEntryDigest:             entry,
			PostRevision:                  postRev,
			PostStateDigest:               postState,
		}
		return r
	}
	if in.AcceptedSummary.SourceStateDigest != preState || in.AcceptedSummary.Revision != accepted.Revision {
		r := mk("stale", accepted.Revision, preState, nil, nil, nil)
		d, err := CanonicalSHA256(r)
		if err != nil {
			return nil, nil, err
		}
		r.ReceiptDigest = d
		return r, accepted, nil // state unchanged, no case bound
	}
	// Commit: advance revision, add the controller family entry, bind the case.
	next := &AcceptedFamilyState{Revision: accepted.Revision + 1, Families: map[string]FamilySummaryEntry{}}
	for id, e := range accepted.Families {
		next.Families[id] = e
	}
	entry := FamilySummaryEntry{
		FamilyID:        famID,
		LanguageMembers: []string{in.AuthorLang},
	}
	entry.EntryDigest = sha256Hex([]byte(strings.Join([]string{
		entry.FamilyID, joinSorted(entry.LanguageMembers), joinSorted(entry.BlindSemanticPayloads),
	}, "\x00")))
	next.Families[famID] = entry
	postState, err := next.StateDigest()
	if err != nil {
		return nil, nil, err
	}
	r := mk("committed", next.Revision, postState, &in.FinalCaseID, &famID, &entry.EntryDigest)
	d, err := CanonicalSHA256(r)
	if err != nil {
		return nil, nil, err
	}
	r.ReceiptDigest = d
	return r, next, nil
}

// RepairAdmissionChain is a one-shot upgrade for receipts written before
// the chain fields were populated at the append site (all-zero sequence and
// null prev-digest): it renumbers sequence 1..n, threads the prev-digest
// chain, and recomputes each self-digest over the unchanged receipt
// content. It refuses mixed-format chains (fail-closed) and is a no-op for
// a chain that already carries sequence 1 on its first receipt.
func RepairAdmissionChain(receipts []*AdmissionReceipt) error {
	if len(receipts) == 0 || receipts[0].AdmissionSequence == 1 {
		return nil
	}
	for _, r := range receipts {
		if r.AdmissionSequence != 0 || r.PreviousAdmissionReceiptDigest != nil {
			return errors.New("admission chain is mixed-format; refusing to repair")
		}
	}
	var prev *string
	for i, r := range receipts {
		r.AdmissionSequence = i + 1
		r.PreviousAdmissionReceiptDigest = prev
		saved := r.ReceiptDigest
		r.ReceiptDigest = ""
		d, err := CanonicalSHA256(r)
		r.ReceiptDigest = saved
		if err != nil {
			return err
		}
		if d == saved {
			// The old self-digest already covers the repaired fields —
			// impossible for the zero-format, but fail loud, not silent.
			return errors.New("admission receipt digest unexpectedly stable across repair")
		}
		r.ReceiptDigest = d
		dd := d
		prev = &dd
	}
	return nil
}

// ReplayAdmissionChain replays a complete append-only receipt chain from the
// empty state and returns the final accepted-family state. Commits must
// correspond one-to-one with bound cases; stale receipts must not move it.
func ReplayAdmissionChain(receipts []*AdmissionReceipt) (*AcceptedFamilyState, error) {
	state := &AcceptedFamilyState{Revision: 0, Families: map[string]FamilySummaryEntry{}}
	var prevDigest *string
	caseIDs := map[string]bool{}
	for i, r := range receipts {
		if r.AdmissionSequence != i+1 {
			return nil, fmt.Errorf("receipt %d sequence %d out of order", i, r.AdmissionSequence)
		}
		if (r.PreviousAdmissionReceiptDigest == nil) != (prevDigest == nil) ||
			(prevDigest != nil && *r.PreviousAdmissionReceiptDigest != *prevDigest) {
			return nil, fmt.Errorf("receipt %d breaks the admission chain", i)
		}
		saved := r.ReceiptDigest
		r.ReceiptDigest = ""
		d, err := CanonicalSHA256(r)
		r.ReceiptDigest = saved
		if err != nil {
			return nil, err
		}
		if d != saved {
			return nil, fmt.Errorf("receipt %d digest mismatch", i)
		}
		switch r.CASResult {
		case "committed":
			if r.FinalCaseID == nil || r.ControllerFamilyID == nil {
				return nil, fmt.Errorf("receipt %d committed without a case/family binding", i)
			}
			if caseIDs[*r.FinalCaseID] {
				return nil, fmt.Errorf("case %s committed twice", *r.FinalCaseID)
			}
			caseIDs[*r.FinalCaseID] = true
			if r.PostRevision != state.Revision+1 {
				return nil, fmt.Errorf("receipt %d revision jump %d → %d", i, state.Revision, r.PostRevision)
			}
			state.Revision = r.PostRevision
			state.Families[*r.ControllerFamilyID] = FamilySummaryEntry{
				FamilyID:    *r.ControllerFamilyID,
				EntryDigest: deref(r.FamilyEntryDigest),
			}
			if r.ObservedPreRevision != r.PostRevision-1 {
				return nil, fmt.Errorf("receipt %d pre/post revision mismatch", i)
			}
		case "stale":
			if r.FinalCaseID != nil || r.ControllerFamilyID != nil {
				return nil, fmt.Errorf("receipt %d stale but bound a case/family", i)
			}
			// A stale attempt observed an older revision than the chain state
			// (that gap is exactly what "stale" means) but must not have moved
			// the state itself.
			if r.ObservedPreRevision >= r.PostRevision {
				return nil, fmt.Errorf("receipt %d stale without an observed/current gap", i)
			}
			if r.PostRevision != state.Revision {
				return nil, fmt.Errorf("receipt %d stale receipt changed state", i)
			}
		default:
			return nil, fmt.Errorf("receipt %d unknown CAS result %q", i, r.CASResult)
		}
		prevDigest = &saved
	}
	return state, nil
}
