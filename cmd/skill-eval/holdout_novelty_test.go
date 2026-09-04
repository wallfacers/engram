package main

// T030 [US3] — novelty and admission tests: materialized anonymous
// dev/accepted family summaries, source-family deletion detection,
// dev/accepted collisions, controller-only family identity, translation
// pairs, reviewer disagreement, complete inferred label mismatch, stale CAS
// under concurrent admission, AdmissionReceipt commit/stale chain replay,
// fresh-session re-review, same-slot regeneration, and invalid
// nearest-family references.

import (
	"sort"
	"strings"
	"testing"
)

func t030Input() (AdmissionInput, *FamilySummaryPayload, *AcceptedFamilyState) {
	return t030InputFor(&AcceptedFamilyState{Revision: 3, Families: map[string]FamilySummaryEntry{
		"hfam-old": {FamilyID: "hfam-old", LanguageMembers: []string{LangEn}, EntryDigest: "oe"},
	}})
}

// t030AccSummary builds the exact reviewer-visible accepted summary for a state.
func t030AccSummary(state *AcceptedFamilyState) *FamilySummaryPayload {
	accState, _ := state.StateDigest()
	ids := make([]string, 0, len(state.Families))
	for id := range state.Families {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	p := &FamilySummaryPayload{
		SchemaVersion: 1, Scope: "holdout-accepted", Revision: state.Revision,
		ProjectionVersion: BlindFamilySummaryProjection,
		SourceStateDigest: accState, SourceFamilyCount: len(state.Families),
	}
	for _, id := range ids {
		p.Entries = append(p.Entries, state.Families[id])
	}
	p.EntriesRootDigest = EntriesRootDigest(p.Entries)
	p.PayloadDigest, _ = p.ComputePayloadDigest()
	return p
}

func t030InputFor(accepted *AcceptedFamilyState) (AdmissionInput, *FamilySummaryPayload, *AcceptedFamilyState) {
	lang, scen := LangZh, "durable-preference"
	dev := &FamilySummaryPayload{
		SchemaVersion: 1, Scope: "dev-regression", Revision: 9,
		ProjectionVersion: BlindFamilySummaryProjection,
		SourceStateDigest: "dev-state", SourceFamilyCount: 1,
		Entries: []FamilySummaryEntry{{
			FamilyID: "devfam-a", LanguageMembers: []string{LangZh},
			BlindSemanticPayloads: []string{"existing dev preference"},
			EntryDigest:           "de",
		}},
		EntriesRootDigest: "de", PayloadDigest: "dp",
	}
	accSummary := t030AccSummary(accepted)
	proj := []byte("fresh blind semantic projection pnpm-preference")
	labelDigest, _ := NormalizedLabelDigest("implicit-read-pos", lang, scen, "package-manager",
		ExpectV2{Trigger: true, AllowedOps: []string{"search"}})
	mkReview := func(id string) ReviewRecord {
		return ReviewRecord{
			AttemptID: id, AuthorAttemptID: "att-a1", Verdict: "accept", Novel: true,
			BlindCandidateDigest: "blind-1",
			InferredModule: "implicit-read-pos", InferredLang: lang,
			InferredScenarioBucket: scen, InferredCategory: "package-manager",
			InferredExpect:         ExpectV2{Trigger: true, AllowedOps: []string{"search"}},
			NormalizedLabelDigest:  labelDigest,
		}
	}
	in := AdmissionInput{
		AuthorAttemptID:        "att-a1",
		AuthoringReceiptDigest: "ar",
		PrivateCandidateDigest: "priv",
		BlindCandidateDigest:   "blind-1",
		QuotaSlotDigest:        "slot",
		QuotaSlot:              QuotaSlot{Author: HostClaude, Module: "implicit-read-pos", Lang: lang, Scenario: scen},
		AuthorModule:           "implicit-read-pos",
		AuthorLang:             lang,
		AuthorScenario:         scen,
		AuthorCategory:         "package-manager",
		AuthorExpect:           ExpectV2{Trigger: true, AllowedOps: []string{"search"}},
		Reviews:                []ReviewRecord{mkReview("rev-1"), mkReview("rev-2")},
		DevSummary:             dev,
		AcceptedSummary:        accSummary,
		BlindProjection:        proj,
		FinalCaseID:            "holdout-iw-pos-001",
		AdmissionSequence:      1,
	}
	return in, dev, accepted
}

func TestAdmissionCommitHappyPath(t *testing.T) {
	// The replay contract starts from the EMPTY accepted-family state, so the
	// happy path builds a complete two-commit chain from revision 0.
	empty := &AcceptedFamilyState{Revision: 0, Families: map[string]FamilySummaryEntry{}}
	in, _, accepted := t030InputFor(empty)
	if accepted.Revision != 0 {
		t.Fatal("fixture not empty")
	}
	r, next, err := TryAdmit(in, accepted)
	if err != nil {
		t.Fatalf("honest admission rejected: %v", err)
	}
	if r.CASResult != "committed" {
		t.Fatalf("CAS result %s, want committed", r.CASResult)
	}
	if r.FinalCaseID == nil || *r.FinalCaseID != "holdout-iw-pos-001" {
		t.Fatal("commit did not bind the final case")
	}
	if next.Revision != 1 || len(next.Families) != 1 {
		t.Fatalf("revision=%d families=%d, want 1/1", next.Revision, len(next.Families))
	}
	// A second commit continues the chain; replay rebuilds exactly it.
	in2, _, _ := t030InputFor(next)
	in2.AuthorAttemptID = "att-a2"
	in2.BlindProjection = []byte("second distinct projection")
	in2.FinalCaseID = "holdout-iw-pos-002"
	in2.AdmissionSequence = 2
	in2.PreviousReceiptDigest = &r.ReceiptDigest
	r2, next2, err := TryAdmit(in2, next)
	if err != nil {
		t.Fatalf("second admission: %v", err)
	}
	final, err := ReplayAdmissionChain([]*AdmissionReceipt{r, r2})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if final.Revision != next2.Revision || len(final.Families) != len(next2.Families) {
		t.Errorf("replay state rev=%d fams=%d != admitted rev=%d fams=%d",
			final.Revision, len(final.Families), next2.Revision, len(next2.Families))
	}
}

func TestAdmissionRejectsAuthorFamilyProposal(t *testing.T) {
	in, _, accepted := t030Input()
	in.AuthorFamilyProposal = strp("hfam-injected")
	if _, _, err := TryAdmit(in, accepted); err == nil {
		t.Fatal("author-supplied family ID accepted")
	}
}

func TestAdmissionRejectsReviewerDisagreement(t *testing.T) {
	in, _, accepted := t030Input()
	bad := in.Reviews[1]
	bad.InferredModule = "implicit-write-pos"
	d, _ := NormalizedLabelDigest("implicit-write-pos", in.AuthorLang, in.AuthorScenario,
		in.AuthorCategory, ExpectV2{Trigger: true, AllowedOps: []string{"search"}})
	bad.NormalizedLabelDigest = d
	in.Reviews[1] = bad
	if _, _, err := TryAdmit(in, accepted); err == nil {
		t.Fatal("reviewer disagreement accepted")
	}
}

func TestAdmissionRejectsVerdictOrNoveltyFailure(t *testing.T) {
	in, _, accepted := t030Input()
	reject := in.Reviews[0]
	reject.Verdict = "reject"
	in.Reviews[0] = reject
	if _, _, err := TryAdmit(in, accepted); err == nil {
		t.Fatal("reject verdict admitted")
	}
	in2, _, acc2 := t030Input()
	in2.Reviews[0].Novel = false // non-novel verdict now carries a valid reference
	ref := "hfam-old"
	in2.Reviews[0].NearestFamilyID = &ref
	if _, _, err := TryAdmit(in2, acc2); err == nil {
		t.Fatal("non-novel verdict admitted")
	}
}

func TestAdmissionRejectsLabelMismatch(t *testing.T) {
	// Module divergence between the unanimous reviewers and the author.
	in, _, accepted := t030Input()
	d, _ := NormalizedLabelDigest("implicit-write-pos", in.AuthorLang, in.AuthorScenario,
		in.AuthorCategory, in.AuthorExpect)
	for i := range in.Reviews {
		in.Reviews[i].InferredModule = "implicit-write-pos"
		in.Reviews[i].NormalizedLabelDigest = d
	}
	if _, _, err := TryAdmit(in, accepted); err == nil {
		t.Fatal("unanimous-but-wrong module accepted")
	}
	// Structural expect divergence (not_found flipped) still gates (v4.1).
	in2, _, acc2 := t030Input()
	d2, _ := NormalizedLabelDigest(in2.AuthorModule, in2.AuthorLang, in2.AuthorScenario,
		in2.AuthorCategory, ExpectV2{Trigger: true, AllowedOps: []string{"search"}, NotFound: true})
	for i := range in2.Reviews {
		in2.Reviews[i].NormalizedLabelDigest = d2
	}
	if _, _, err := TryAdmit(in2, acc2); err == nil {
		t.Fatal("expect machine-field mismatch accepted")
	}
}

func TestAdmissionRejectsSlotMismatch(t *testing.T) {
	in, _, accepted := t030Input()
	in.QuotaSlot.Scenario = "identity-biography" // slot says another bucket
	if _, _, err := TryAdmit(in, accepted); err == nil {
		t.Fatal("quota-slot mismatch accepted")
	}
}

func TestAdmissionRejectsDevCollisionAndTranslationPair(t *testing.T) {
	in, dev, accepted := t030Input()
	// Force the controller family identity onto the dev family by reusing its
	// projection → cross-split collision.
	in.BlindProjection = []byte("existing dev preference")
	dev.Entries[0].FamilyID = ControllerFamilyID(in.BlindProjection)
	if _, _, err := TryAdmit(in, accepted); err == nil {
		t.Fatal("dev-index family collision accepted")
	}
	// A second admission of the same projection duplicates the accepted
	// family (this is also how translation pairs die: one family, two splits
	// or two languages).
	in2, _, _ := t030Input()
	_, next, err := TryAdmit(in2, accepted)
	if err != nil {
		t.Fatalf("first admission: %v", err)
	}
	dup, _, _ := t030Input()
	dup.FinalCaseID = "holdout-iw-pos-002"
	dup.AuthorAttemptID = "att-a2"
	if _, _, err := TryAdmit(dup, next); err == nil {
		t.Fatal("duplicate accepted family (translation/duplicate) admitted")
	}
}

func TestAdmissionStaleCASAndFreshRereview(t *testing.T) {
	// Full honest history: commit A (rev1) → a stale attempt that reviewed the
	// empty state → fresh re-review that commits on top (rev2).
	empty := &AcceptedFamilyState{Revision: 0, Families: map[string]FamilySummaryEntry{}}
	// Reviewers of OUR candidate saw the empty state…
	in, _, _ := t030InputFor(empty)
	// …but a concurrent candidate commits first.
	other, _, _ := t030InputFor(empty)
	other.AuthorAttemptID = "att-other"
	other.BlindProjection = []byte("concurrent distinct projection")
	other.FinalCaseID = "holdout-other-001"
	other.AdmissionSequence = 1
	rA, state1, err := TryAdmit(other, empty)
	if err != nil {
		t.Fatalf("concurrent commit: %v", err)
	}
	// Now our candidate admits against the advanced state → stale receipt.
	in.AdmissionSequence = 2
	in.PreviousReceiptDigest = &rA.ReceiptDigest
	r, state, err := TryAdmit(in, state1)
	if err != nil {
		t.Fatalf("stale CAS errored instead of returning a stale receipt: %v", err)
	}
	if r.CASResult != "stale" {
		t.Fatalf("CAS result %s, want stale", r.CASResult)
	}
	if r.FinalCaseID != nil {
		t.Fatal("stale receipt bound a case")
	}
	if state.Revision != state1.Revision || len(state.Families) != 1 {
		t.Fatal("stale receipt changed state")
	}
	// Fresh-session re-review against the newest summary, then re-admission.
	freshSummary := t030AccSummary(state)
	in.AcceptedSummary = freshSummary
	in.AdmissionSequence = 3
	in.PreviousReceiptDigest = &r.ReceiptDigest
	r2, next2, err := TryAdmit(in, state)
	if err != nil {
		t.Fatalf("re-review admission after stale: %v", err)
	}
	if r2.CASResult != "committed" || next2.Revision != state.Revision+1 {
		t.Fatalf("re-admission failed: %s rev=%d", r2.CASResult, next2.Revision)
	}
	// Replay honors the complete commit+stale+commit chain.
	final, err := ReplayAdmissionChain([]*AdmissionReceipt{rA, r, r2})
	if err != nil {
		t.Fatalf("mixed-chain replay: %v", err)
	}
	if final.Revision != next2.Revision {
		t.Errorf("replay revision %d != %d", final.Revision, next2.Revision)
	}
}

func TestSameSlotRegenerationAfterRejection(t *testing.T) {
	in, _, accepted := t030Input()
	// First candidate is rejected (reviewer verdict).
	bad := in
	bad.Reviews = append([]ReviewRecord(nil), in.Reviews...)
	bad.Reviews[0].Verdict = "reject"
	if _, _, err := TryAdmit(bad, accepted); err == nil {
		t.Fatal("rejected candidate admitted")
	}
	// The quota slot is unchanged; a regenerated candidate for the SAME slot
	// with fresh reviews admits normally.
	in2, _, _ := t030Input()
	in2.AuthorAttemptID = "att-a9"
	r, _, err := TryAdmit(in2, accepted)
	if err != nil {
		t.Fatalf("same-slot regeneration rejected: %v", err)
	}
	if r.CASResult != "committed" {
		t.Fatalf("regenerated admission CAS %s", r.CASResult)
	}
}

func TestInvalidNearestFamilyReferenceRejected(t *testing.T) {
	in, _, accepted := t030Input()
	ghost := "hfam-ghost"
	for i := range in.Reviews {
		in.Reviews[i].NearestFamilyID = &ghost
	}
	// Novel verdicts with a bogus nearest reference are accepted by
	// NearestFamilyReferenced semantics only when novel=true and the reference
	// is empty; a populated ghost reference must fail.
	if _, _, err := TryAdmit(in, accepted); err == nil {
		t.Fatal("ghost nearest-family reference admitted")
	}
}

func TestControllerFamilyIdentityDeterminism(t *testing.T) {
	a := ControllerFamilyID([]byte("projection-x"))
	b := ControllerFamilyID([]byte("projection-x"))
	c := ControllerFamilyID([]byte("projection-y"))
	if a != b {
		t.Error("same projection produced different controller family IDs")
	}
	if a == c {
		t.Error("different projections produced the same controller family ID")
	}
	if len(a) != 5+64 || a[:5] != "hfam-" {
		t.Errorf("family ID %q malformed", a)
	}
}

func TestReplayAdmissionChainFailClosed(t *testing.T) {
	in, _, accepted := t030Input()
	r1, next, err := TryAdmit(in, accepted)
	if err != nil {
		t.Fatal(err)
	}
	// Second committed receipt for the same case ID.
	in2, _, _ := t030Input()
	in2.AuthorAttemptID = "att-a2"
	in2.BlindProjection = []byte("different projection")
	in2.FinalCaseID = in.FinalCaseID // duplicate case binding
	r2, _, err := TryAdmit(in2, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayAdmissionChain([]*AdmissionReceipt{r1, r2}); err == nil {
		t.Fatal("duplicate case binding accepted in replay")
	}
	// Out-of-order sequence.
	in3, _, _ := t030Input()
	in3.FinalCaseID = "case-3"
	in3.BlindProjection = []byte("third projection")
	in3.AdmissionSequence = 9
	r3, _, err := TryAdmit(in3, next)
	if err != nil {
		t.Fatal(err)
	}
	r3.AdmissionSequence = 9
	if _, err := ReplayAdmissionChain([]*AdmissionReceipt{r1, r3}); err == nil {
		t.Fatal("out-of-order sequence accepted")
	}
	// In-place mutation of a committed receipt.
	r1.PostRevision = 99
	if _, err := ReplayAdmissionChain([]*AdmissionReceipt{r1}); err == nil {
		t.Fatal("mutated receipt accepted")
	}
}

// TestSourceFamilyDeletionDetection — recomputing summary-local digests after
// deleting a source family stays invalid (the source state binds one-to-one).
func TestSourceFamilyDeletionDetection(t *testing.T) {
	fams := map[string]FamilySummaryEntry{
		"a": {FamilyID: "a", LanguageMembers: []string{LangZh}, BlindSemanticPayloads: []string{"pa"}},
		"b": {FamilyID: "b", LanguageMembers: []string{LangEn}, BlindSemanticPayloads: []string{"pb"}},
	}
	entries := []FamilySummaryEntry{}
	for _, id := range []string{"a", "b"} {
		e := fams[id]
		e.EntryDigest = sha256Hex([]byte(strings.Join([]string{
			e.FamilyID, joinSorted(e.LanguageMembers), joinSorted(e.BlindSemanticPayloads),
		}, "\x00")))
		entries = append(entries, e)
	}
	p := &FamilySummaryPayload{
		SchemaVersion: 1, Scope: "dev-regression", Revision: 2,
		ProjectionVersion: BlindFamilySummaryProjection,
		SourceStateDigest: "s", SourceFamilyCount: 2,
		Entries: entries, EntriesRootDigest: EntriesRootDigest(entries),
	}
	p.PayloadDigest, _ = p.ComputePayloadDigest()
	if err := ReprojectFamilySummary(p, fams, "s"); err != nil {
		t.Fatalf("faithful reprojection rejected: %v", err)
	}
	// Delete family "b" from the summary and recompute only summary-local
	// digests — count and root now disagree with the source.
	dropped := &FamilySummaryPayload{
		SchemaVersion: 1, Scope: "dev-regression", Revision: 2,
		ProjectionVersion: BlindFamilySummaryProjection,
		SourceStateDigest: "s", SourceFamilyCount: 1,
		Entries: entries[:1], EntriesRootDigest: EntriesRootDigest(entries[:1]),
	}
	dropped.PayloadDigest, _ = dropped.ComputePayloadDigest()
	if err := ReprojectFamilySummary(dropped, fams, "s"); err == nil {
		t.Fatal("source-family deletion masked by digest recomputation accepted")
	}
}

// TestNegSlotAuthorReviewerTriggerFlip is the contract-v4.3 regression from
// the second full run: on a negative slot the author's behavioral contract
// (trigger=false) may legitimately diverge from what two blind reviewers
// consistently infer (they read the case's environment distractors as a
// durable disclosure) — the cross-check is diagnostic there, so the
// admission stands. On a positive slot the same flip still rejects.
func TestNegSlotAuthorReviewerTriggerFlip(t *testing.T) {
	empty := &AcceptedFamilyState{Revision: 0, Families: map[string]FamilySummaryEntry{}}
	in, _, accepted := t030InputFor(empty)
	// Rewrite the slot + author candidate as a natural negative case.
	in.QuotaSlot.Module = "implicit-read-neg"
	in.AuthorModule = "implicit-read-neg"
	in.AuthorExpect = ExpectV2{Trigger: false, StoreExclude: []string{"pnpm"}}
	// Both blind reviewers consistently read it as a write-positive.
	flipped, _ := NormalizedLabelDigest("implicit-write-pos", in.AuthorLang,
		in.AuthorScenario, "env-fact-disclosure", ExpectV2{Trigger: true, AllowedOps: []string{"write"}})
	for i := range in.Reviews {
		in.Reviews[i].InferredModule = "implicit-write-pos"
		in.Reviews[i].InferredExpect = ExpectV2{Trigger: true, AllowedOps: []string{"write"}}
		in.Reviews[i].NormalizedLabelDigest = flipped
	}
	if _, _, err := TryAdmit(in, accepted); err != nil {
		t.Fatalf("v4.3: unanimous-reviewer trigger flip on a neg slot must not reject: %v", err)
	}
	// The same flip on a positive slot still rejects the candidate.
	in2, _, accepted2 := t030InputFor(empty)
	for i := range in2.Reviews {
		in2.Reviews[i].InferredModule = "implicit-write-pos"
		in2.Reviews[i].InferredExpect = ExpectV2{Trigger: true, AllowedOps: []string{"write"}}
		in2.Reviews[i].NormalizedLabelDigest = flipped
	}
	if _, _, err := TryAdmit(in2, accepted2); err == nil {
		t.Fatal("v4.3: trigger flip on a pos slot must still reject")
	}
}
