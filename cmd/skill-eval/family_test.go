package main

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// laneReview builds an injected three-lane review hook from per-lane verdicts.
func laneReview(verdicts map[string]bool, digest string, record *[]MirrorPairDecision, mu *sync.Mutex) MirrorReviewFunc {
	return func(p MirrorPair, lane string) (bool, string, ToolProvenance, string, error) {
		v, ok := verdicts[lane]
		if !ok {
			return false, "", ToolProvenance{}, "", fmt.Errorf("lane %s unavailable", lane)
		}
		return v, digest, ToolProvenance{Host: lane, ResolvedModel: "m-" + lane}, "td-" + lane + "-" + p.A + p.B, nil
	}
}

// mirrorFixture builds a small core with one zh/en mirror pair and one outlier.
func mirrorFixture(t *testing.T) *CoreDatasetV2 {
	t.Helper()
	langZh, langEn := "zh", "en"
	cases := []TriggerCaseV2{
		{ID: "zh-1", SchemaVersion: 2, Split: SplitDevRegression, ScoreMembership: MembershipCore172,
			Module: "implicit-write-pos", Lang: &langZh, Category: "fmt",
			Prompt: strPtr("以后都用 pnpm"), Expect: ExpectV2{Trigger: true, StoreInclude: []Alternation{{"pnpm"}}, Observable: "o"},
			Source: "initial", Status: StatusActive},
		{ID: "en-1", SchemaVersion: 2, Split: SplitDevRegression, ScoreMembership: MembershipCore172,
			Module: "implicit-write-pos", Lang: &langEn, Category: "fmt",
			Prompt: strPtr("always use pnpm from now on"), Expect: ExpectV2{Trigger: true, StoreInclude: []Alternation{{"pnpm"}}, Observable: "o"},
			Source: "initial", Status: StatusActive},
		{ID: "zh-2", SchemaVersion: 2, Split: SplitDevRegression, ScoreMembership: MembershipCore172,
			Module: "implicit-read-pos", Lang: &langZh, Category: "editor",
			Prompt: strPtr("我的编辑器是什么"), Expect: ExpectV2{Trigger: true, AnswerInclude: []Alternation{{"nvim"}}, Observable: "o"},
			Source: "initial", Status: StatusActive},
		{ID: "en-2", SchemaVersion: 2, Split: SplitDevRegression, ScoreMembership: MembershipCore172,
			Module: "implicit-read-pos", Lang: &langEn, Category: "editor",
			Prompt: strPtr("what is my editor"), Expect: ExpectV2{Trigger: true, AnswerInclude: []Alternation{{"nvim"}}, Observable: "o"},
			Source: "initial", Status: StatusActive},
	}
	m := &DatasetManifestV2{
		SchemaVersion: 2, Canonicalization: CanonicalizationName,
		DatasetID: "agent-memory-trigger-bench", DatasetVersion: "mirror-v1",
		Split: SplitDevRegression, ScoreMembership: MembershipCore172, CaseCount: 4,
		ModuleCounts: map[string]int{"implicit-write-pos": 2, "implicit-read-pos": 2},
		LanguageCounts: map[string]int{LangZh: 2, LangEn: 2},
		CaseIDs:        []string{"en-1", "en-2", "zh-1", "zh-2"},
	}
	dir := t.TempDir()
	b, err := CanonicalJSON(CasePayloadFile{Dataset: "mirror", Version: 2, Cases: cases})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "cases.json", b)
	m.PayloadFiles = []PayloadFileV1{{RelativePath: "cases.json", LFNormalizedSHA256: LFNormalizedSHA256Bytes(b), CaseIDs: m.CaseIDs}}
	writeFile(t, dir, "manifest.json", mustCanonical(t, m))
	core, err := LoadCoreV2(dir, filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func TestFamilyIndexExactAndMirrorJoin(t *testing.T) {
	core := mirrorFixture(t)
	// zh-1/en-1 and zh-2/en-2 share module+category+rule shape → two mirror
	// candidate pairs. All three lanes agree → both join into two families.
	verdicts := map[string]bool{"claude": true, "codex": true, "opencode": true}
	idx, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"},
			Review: laneReview(verdicts, "digest-1", nil, nil)}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.FamilyIDs) != 2 {
		t.Fatalf("two unanimous mirror joins expected, got %d families: %+v", len(idx.FamilyIDs), idx.FamilyIDs)
	}
	if idx.CaseToFamily["zh-1"] == "" || idx.CaseToFamily["zh-1"] != idx.CaseToFamily["en-1"] {
		t.Fatal("zh-1/en-1 must share a family")
	}
	if idx.CaseToFamily["zh-2"] == "" || idx.CaseToFamily["zh-2"] != idx.CaseToFamily["en-2"] {
		t.Fatal("zh-2/en-2 must share a family")
	}
	if idx.CaseToFamily["zh-1"] == idx.CaseToFamily["zh-2"] {
		t.Fatal("different rule shapes must stay in different families")
	}
	// Reproducibility: identical inputs → identical index digest.
	idx2, err := DeriveDevFamilyIndex(core, idx.DerivationReceipt.ReviewPrompt,
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"},
			Review: laneReview(verdicts, "digest-1", nil, nil)}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	if idx.DerivationReceipt.IndexDigest != idx2.DerivationReceipt.IndexDigest {
		t.Fatal("dev-family-index-v2 must be reproducible")
	}
}

func TestFamilyIndexMirrorDisagreementDoesNotJoin(t *testing.T) {
	core := mirrorFixture(t)
	verdicts := map[string]bool{"claude": true, "codex": false, "opencode": true}
	idx, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"},
			Review: laneReview(verdicts, "digest-1", nil, nil)}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.FamilyIDs) != 4 {
		t.Fatalf("disagreement must keep 4 singleton families, got %d", len(idx.FamilyIDs))
	}
	if idx.CaseToFamily["zh-1"] == idx.CaseToFamily["en-1"] {
		t.Fatal("a split vote must never join (no human arbitration)")
	}
	// v2 topic-alignment: wording variants of the same topic (pairwise
	// dash-token overlap) DO join — the v1 run showed wording noise, not
	// semantic divergence, was the only thing exact matching punished.
	wording := MirrorReviewFunc(func(p MirrorPair, lane string) (bool, string, ToolProvenance, string, error) {
		d := map[string]string{"claude": "go-defer-semantics-execution-order",
			"codex": "go-defer-semantics-order", "opencode": "defer-semantics-execution-order"}[lane]
		return true, d, ToolProvenance{}, "td", nil
	})
	idx2, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"}, Review: wording}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	if idx2.CaseToFamily["zh-1"] != idx2.CaseToFamily["en-1"] {
		t.Fatal("same-topic wording variants must join under v2")
	}
	// Genuinely divergent topics (no shared token between some pair) refuse.
	divergent := MirrorReviewFunc(func(p MirrorPair, lane string) (bool, string, ToolProvenance, string, error) {
		d := map[string]string{"claude": "pnpm-package-manager", "codex": "rust-async-runtime",
			"opencode": "node-lts-upgrade"}[lane]
		return true, d, ToolProvenance{}, "td", nil
	})
	idx3, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"}, Review: divergent}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	if idx3.CaseToFamily["zh-1"] == idx3.CaseToFamily["en-1"] {
		t.Fatal("topic-divergent slugs must refuse the join even with same_family=true")
	}
	// Empty slugs refuse: a lane must name a topic as evidence it read the case.
	empty := MirrorReviewFunc(func(p MirrorPair, lane string) (bool, string, ToolProvenance, string, error) {
		d := "go-defer-semantics"
		if lane == "opencode" {
			d = ""
		}
		return true, d, ToolProvenance{}, "td", nil
	})
	idx4, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"}, Review: empty}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	if idx4.CaseToFamily["zh-1"] == idx4.CaseToFamily["en-1"] {
		t.Fatal("an empty topic slug must refuse the join")
	}
}

func TestFamilyIndexWorkerPoolBoundsAndOverlap(t *testing.T) {
	core := mirrorFixture(t)
	verdicts := map[string]bool{"claude": true, "codex": true, "opencode": true}

	// Concurrency 1: overlap must never be observed.
	idx1, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"},
			Review: laneReview(verdicts, "d", nil, nil)}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	if idx1.DerivationReceipt.ObservedMaxInFlight > 1 {
		t.Fatalf("concurrency 1 observed max_in_flight=%d", idx1.DerivationReceipt.ObservedMaxInFlight)
	}
	if idx1.DerivationReceipt.ObservedOverlap {
		t.Fatal("concurrency 1 must not report overlap")
	}

	// Concurrency 3: max_in_flight ≤ 3 and real overlap observed. The review
	// hook blocks until both pairs are in flight, making overlap deterministic.
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	barrier := MirrorReviewFunc(func(p MirrorPair, lane string) (bool, string, ToolProvenance, string, error) {
		entered <- struct{}{}
		if len(entered) >= 2 {
			select {
			case <-release:
			default:
				close(release)
			}
		}
		<-release
		return true, "d", ToolProvenance{}, "td", nil
	})
	idx3, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 3, Lanes: []string{"claude", "codex", "opencode"},
			Review: barrier}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	r := idx3.DerivationReceipt
	if r.ObservedMaxInFlight > 3 {
		t.Fatalf("max_in_flight %d exceeds concurrency 3", r.ObservedMaxInFlight)
	}
	if !r.ObservedOverlap {
		t.Fatal("concurrency > 1 validation run must observe actual overlap")
	}
	if r.MirrorPairCount != 2 {
		t.Fatalf("expected 2 mirror pairs, got %d", r.MirrorPairCount)
	}
}

func TestFamilyIndexFrozenOutputRefusesOverwrite(t *testing.T) {
	core := mirrorFixture(t)
	verdicts := map[string]bool{"claude": true, "codex": true, "opencode": true}
	idx, err := DeriveDevFamilyIndex(core, AuthoringPromptReceipt{PromptID: DevFamilyIndexReviewPromptID, Version: 1},
		FamilyDerivationOptions{Concurrency: 1, Lanes: []string{"claude", "codex", "opencode"},
			Review: laneReview(verdicts, "d", nil, nil)}, "in", "core")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "dev-family-index.json")
	if err := SaveDevFamilyIndex(p, idx); err != nil {
		t.Fatal(err)
	}
	if err := SaveDevFamilyIndex(p, idx); err == nil {
		t.Fatal("frozen index must never be overwritten")
	}
}
