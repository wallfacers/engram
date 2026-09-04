package main

// T032-T034 — the holdout96 generation/review/admission/seal orchestrator.
// Everything model-facing goes through the frozen three-lane CLI driver
// (runLaneCLI); every attempt runs under the bounded StageWorkspaceManager,
// lands in the append-only ledger, and carries controller-probed isolation
// receipts. Private state (candidates, audit, receipts, reviews) lives only
// under the operator-provided protected root; the sealed payload contains
// the 96 admitted cases with private lineage stripped (data-model.md §3-§7).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// HoldoutBatchConfig is the frozen run configuration for one batch.
type HoldoutBatchConfig struct {
	Root               string // operator-provided protected root (absolute)
	DatasetDir         string // skills/engram/evals (dev index + prompts)
	AuthorPromptFile   string
	ReviewPromptFile   string
	DevIndexFile       string
	Concurrency        int
	MaxAttemptsPerSlot int
	AnchorType         string
	AnchorID           string
}

// holdoutPersisted is the resumable on-disk batch state (private root only).
type holdoutPersisted struct {
	BatchID     string                    `json:"batch_id"`
	Filled      map[string]*TriggerCaseV2 `json:"filled"` // slot key → admitted case
	Accepted    *AcceptedFamilyState      `json:"accepted"`
	Ledger      *AuthorReviewAttemptLedgerV1 `json:"ledger"`
	Admissions  []*AdmissionReceipt       `json:"admissions"`
	Probes      map[string][]AccessProbe  `json:"probes"`
	AuthorProvs []ToolProvenance          `json:"author_provs"`
	ReviewProvs []ToolProvenance          `json:"review_provs"`
	StateRoots  []string                  `json:"state_roots"`
	CaseSeq     int                       `json:"case_seq"`
}

// HoldoutBatch is the live orchestrator handle.
type HoldoutBatch struct {
	cfg     HoldoutBatchConfig
	persist holdoutPersisted

	stage   *StageWorkspaceManager
	devSum  *FamilySummaryPayload
	authorP *AuthoringPromptReceipt
	reviewP *AuthoringPromptReceipt
	mu      sync.Mutex
}

func slotKey(s QuotaSlot) string {
	return fmt.Sprintf("%s/%s/%s/%s", s.Author, s.Module, s.Lang, s.Scenario)
}

// FilledSlotKey is the Filled-map key for the nth occurrence of a slot key in
// the frozen table. The table legitimately repeats a four-tuple (tuple
// uniqueness was never a contract invariant — the count tables are), so the
// second and further occurrences disambiguate with a "#n" suffix; the first
// keeps the bare key for backward compatibility with existing batches.
func FilledSlotKey(k string, occurrence int) string {
	if occurrence == 0 {
		return k
	}
	return fmt.Sprintf("%s#%d", k, occurrence)
}

// siblingSentinel is the mode-0000 marker every attempt materializes in its
// input dir; sibling probes target it (name-independent of prompt vs
// envelope, always present, always locked).
func siblingSentinel(ws string) string {
	return filepath.Join(ws, "input", ".locked")
}

// ensureSiblingFixture materializes a permanent stand-in sibling workspace
// under <root>/attempts/sibling-fixture (a foreign input locked the same way
// a real attempt's is). The very first attempt of a fresh batch has neither
// an in-flight nor a retired sibling to probe, and an omitted probe would
// fail the fail-closed aggregation later — the fixture makes the probe
// target deterministic and launch-order-independent. Idempotent.
func ensureSiblingFixture(root string) error {
	ws := filepath.Join(root, "attempts", "sibling-fixture")
	input := filepath.Join(ws, "input")
	if err := os.MkdirAll(input, 0o700); err != nil {
		return err
	}
	sentinel := siblingSentinel(ws)
	if _, err := os.Stat(sentinel); err == nil {
		return nil
	}
	if err := os.WriteFile(sentinel, []byte("controller-sibling-fixture-v1"), 0o000); err != nil {
		return err
	}
	return nil
}

// LoadOrInitHoldoutBatch resumes a batch from <root>/batch.json or starts one.
func LoadOrInitHoldoutBatch(cfg HoldoutBatchConfig) (*HoldoutBatch, error) {
	if !filepath.IsAbs(cfg.Root) {
		return nil, errors.New("holdout protected root must be an absolute path")
	}
	if fi, err := os.Stat(cfg.Root); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("protected root %s missing — the operator must create it", cfg.Root)
	}
	if err := ensureSiblingFixture(cfg.Root); err != nil {
		return nil, fmt.Errorf("sibling fixture: %w", err)
	}
	if cfg.MaxAttemptsPerSlot <= 0 {
		cfg.MaxAttemptsPerSlot = 6
	}
	b := &HoldoutBatch{cfg: cfg}
	// Frozen prompts and their digests.
	ap, err := LoadAuthoringPromptReceipt(cfg.AuthorPromptFile, "holdout-authoring")
	if err != nil {
		return nil, err
	}
	rp, err := LoadAuthoringPromptReceipt(cfg.ReviewPromptFile, "holdout-review")
	if err != nil {
		return nil, err
	}
	b.authorP, b.reviewP = ap, rp
	// Materialized dev family summary (novelty baseline).
	devSum, err := holdoutDevSummaryFrom(cfg.DevIndexFile, cfg.DatasetDir)
	if err != nil {
		return nil, err
	}
	b.devSum = devSum
	// Resume or init.
	bp := filepath.Join(cfg.Root, "batch.json")
	if raw, err := os.ReadFile(bp); err == nil {
		if err := json.Unmarshal(raw, &b.persist); err != nil {
			return nil, fmt.Errorf("batch.json corrupt: %w", err)
		}
		// A hard-killed runner leaves started-but-never-terminalized
		// attempts; terminalize them honestly before the invariant check
		// so a kill/restart cycle never invalidates the batch root.
		if err := b.persist.Ledger.CloseInterruptedAttempts(); err != nil {
			return nil, fmt.Errorf("closing interrupted attempts: %w", err)
		}
		if err := b.persist.Ledger.VerifyLedger(); err != nil {
			return nil, fmt.Errorf("resumed ledger invalid: %w", err)
		}
		// One-shot upgrade for chains written before the append site
		// maintained sequence/prev-digest (content unchanged, chain fields
		// threaded, self-digests recomputed).
		if err := RepairAdmissionChain(b.persist.Admissions); err != nil {
			return nil, fmt.Errorf("repairing admission chain: %w", err)
		}
		if _, err := ReplayAdmissionChain(b.persist.Admissions); err != nil {
			return nil, fmt.Errorf("resumed admission chain invalid: %w", err)
		}
	} else {
		b.persist = holdoutPersisted{
			BatchID:  "hb-" + ap.SHA256[:12],
			Filled:   map[string]*TriggerCaseV2{},
			Accepted: &AcceptedFamilyState{Revision: 0, Families: map[string]FamilySummaryEntry{}},
			Ledger:   &AuthorReviewAttemptLedgerV1{},
			Probes:   map[string][]AccessProbe{},
		}
	}
	b.stage = NewStageWorkspaceManager(filepath.Join(cfg.Root, "attempts"), cfg.Concurrency)
	// The controller-side forbidden trees: placeholder records first (the
	// denial probes need real, existence-provable targets), then mode 0000 so
	// even the owning user cannot traverse them.
	for _, d := range []string{"private", "generation-audit", "author-receipts", "prior-reviews"} {
		p := filepath.Join(cfg.Root, d)
		placeholder := map[string]string{
			"private":          "candidates.json",
			"generation-audit": "audit.json",
			"author-receipts":  "receipts.json",
			"prior-reviews":    "reviews.json",
		}[d]
		if err := os.MkdirAll(p, 0o700); err != nil {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(p, placeholder)); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(filepath.Join(p, placeholder), []byte("{}\n"), 0o600); err != nil {
				return nil, err
			}
		}
		if err := os.Chmod(p, 0o000); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// LoadAuthoringPromptReceipt reads a frozen prompt file into its receipt.
func LoadAuthoringPromptReceipt(path, promptID string) (*AuthoringPromptReceipt, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.ContainsRune(string(raw), 0) {
		return nil, errors.New("prompt file contains NUL")
	}
	d, err := LFNormalizedSHA256(raw)
	if err != nil {
		return nil, err
	}
	qp, err := HoldoutQuotaSlots()
	if err != nil {
		return nil, err
	}
	qd, err := CanonicalSHA256(quotaSlotProjection(qp))
	if err != nil {
		return nil, err
	}
	return &AuthoringPromptReceipt{
		PromptID: promptID, Version: 1,
		DigestAlgorithm: "lf-normalized-sha256-v1", SHA256: d, QuotaPlanDigest: qd,
	}, nil
}

func quotaSlotProjection(slots []QuotaSlot) []QuotaSlot {
	out := append([]QuotaSlot{}, slots...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		if a.Author != b.Author {
			return a.Author < b.Author
		}
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.Lang < b.Lang
	})
	return out
}

// devBlindPayload is one dev case's label-free semantic substance: its
// prompt / user-turn text, seed memory names/content and staged workspace
// paths. No module/category/expect/author fields survive.
func devBlindPayload(c *TriggerCaseV2) string {
	var parts []string
	if c.Prompt != nil {
		parts = append(parts, *c.Prompt)
	}
	for _, t := range c.Turns {
		if !t.SetupOnly {
			parts = append(parts, t.Content)
		}
	}
	for _, sm := range c.SeedMemories {
		parts = append(parts, sm.Name+": "+sm.Content)
	}
	for _, f := range c.WorkspaceFiles {
		parts = append(parts, "file:"+f.Path+" "+f.Content)
	}
	return strings.Join(parts, "\n\u241e\n")
}

// BuildDevFamilySummary projects the frozen dev index into the anonymous,
// label-free reviewer-visible summary (source-bound one-to-one). stateDigest
// is the digest of the exact index bytes the projection ran over; core
// supplies the member cases for the blind semantic payloads.
func BuildDevFamilySummary(idx *DevFamilyIndex, stateDigest string, core *CoreDatasetV2) (*FamilySummaryPayload, error) {
	entries := make([]FamilySummaryEntry, 0, len(idx.Families))
	for _, fam := range idx.Families {
		ids := append([]string{}, fam.CaseIDs...)
		sort.Strings(ids)
		famRef := "devfam-" + sha256Hex([]byte(strings.Join(ids, "\x00")))[:24]
		langs := make([]string, 0, len(fam.LanguageMembers))
		for l, ok := range fam.LanguageMembers {
			if ok {
				langs = append(langs, l)
			}
		}
		var payloads []string
		for _, id := range ids {
			if c, ok := core.Cases[id]; ok {
				payloads = append(payloads, devBlindPayload(c))
			}
		}
		sort.Strings(payloads)
		e := FamilySummaryEntry{FamilyID: famRef, LanguageMembers: langs, BlindSemanticPayloads: payloads}
		e.EntryDigest = sha256Hex([]byte(strings.Join([]string{
			e.FamilyID, joinSorted(e.LanguageMembers), joinSorted(e.BlindSemanticPayloads),
		}, "\x00")))
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FamilyID < entries[j].FamilyID })
	p := &FamilySummaryPayload{
		SchemaVersion: 1, Scope: "dev-regression", Revision: 1,
		ProjectionVersion: BlindFamilySummaryProjection,
		SourceStateDigest: stateDigest, SourceFamilyCount: len(idx.Families),
		Entries: entries, EntriesRootDigest: EntriesRootDigest(entries),
	}
	pd, err := p.ComputePayloadDigest()
	if err != nil {
		return nil, err
	}
	p.PayloadDigest = pd
	return p, nil
}

// holdoutDevSummaryFrom loads the dev summary bound to the exact index bytes,
// with blind semantic payloads drawn from the frozen core172 cases.
func holdoutDevSummaryFrom(indexFile string, datasetDir string) (*FamilySummaryPayload, error) {
	idxRaw, err := os.ReadFile(indexFile)
	if err != nil {
		return nil, err
	}
	idx, err := LoadDevFamilyIndex(indexFile)
	if err != nil {
		return nil, err
	}
	core, err := LoadCoreV2(datasetDir, filepath.Join(datasetDir, "dev-regression-core.manifest.json"))
	if err != nil {
		return nil, err
	}
	return BuildDevFamilySummary(idx, sha256Hex(normalizeLF(idxRaw)), core)
}

// authoringPrompt materializes the frozen authoring prompt for one slot.
func (b *HoldoutBatch) authoringPrompt(slot QuotaSlot, caseID string) (string, error) {
	raw, err := os.ReadFile(b.cfg.AuthorPromptFile)
	if err != nil {
		return "", err
	}
	p := string(raw)
	p = strings.ReplaceAll(p, "{MODULE}", slot.Module)
	p = strings.ReplaceAll(p, "{LANG}", slot.Lang)
	p = strings.ReplaceAll(p, "{SCENARIO_BUCKET}", slot.Scenario)
	p = strings.ReplaceAll(p, "{CASE_ID}", caseID)
	return p, nil
}

// reviewPrompt materializes the frozen review prompt plus the anonymous
// envelope (blind candidate + both family summaries + attempt echoes).
func (b *HoldoutBatch) reviewPrompt(bc *BlindCandidateV1, blindDigest string, accepted *AcceptedFamilyState, revAttID, authorAttID string) (string, error) {
	raw, err := os.ReadFile(b.cfg.ReviewPromptFile)
	if err != nil {
		return "", err
	}
	accSum := AcceptedSummaryFor(accepted)
	env := map[string]any{
		"review_attempt_id":  revAttID,
		"author_attempt_id":  authorAttID,
		"blind_candidate":    bc,
		"blind_candidate_digest": blindDigest,
		"dev_family_summary":     b.devSum,
		"accepted_holdout_family_summary": accSum,
	}
	envJSON, err := CanonicalJSON(env)
	if err != nil {
		return "", err
	}
	return string(raw) + "\n\n---\n\n# Input\n\n```json\n" + string(envJSON) + "\n```\n", nil
}

// AcceptedSummaryFor builds the current reviewer-visible accepted summary.
func AcceptedSummaryFor(state *AcceptedFamilyState) *FamilySummaryPayload {
	ids := make([]string, 0, len(state.Families))
	for id := range state.Families {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	d, _ := state.StateDigest()
	p := &FamilySummaryPayload{
		SchemaVersion: 1, Scope: "holdout-accepted", Revision: state.Revision,
		ProjectionVersion: BlindFamilySummaryProjection,
		SourceStateDigest: d, SourceFamilyCount: len(state.Families),
	}
	for _, id := range ids {
		p.Entries = append(p.Entries, state.Families[id])
	}
	p.EntriesRootDigest = EntriesRootDigest(p.Entries)
	p.PayloadDigest, _ = p.ComputePayloadDigest()
	return p
}

// probesFor records the controller-probed isolation matrix for one attempt
// (controller target-existence proofs captured before the child launches).
func (b *HoldoutBatch) probesFor(attemptID string, ownInput string, sibling string) []AccessProbe {
	probe := func(kind ProbeKind, target string) AccessProbe {
		proof, err := CaptureControllerTargetProof(target)
		proofDigest := ""
		if err == nil {
			proofDigest = proof.Digest()
		}
		return AccessProbe{
			Kind: kind, TargetPath: target,
			ControllerTargetProofDigest: proofDigest,
			TargetAccessPolicyDigest:    "protected-root-v1",
			Expected:                    ProbeDenied,
			Observed:                    ProbeFilesystem(target),
		}
	}
	var ps []AccessProbe
	root := b.cfg.Root
	ps = append(ps, probe(ProbePrivateRootTraverse, filepath.Join(root, "private")))
	ps = append(ps, probe(ProbePrivateRootList, filepath.Join(root, "private")))
	ps = append(ps, probe(ProbePrivateRootRead, filepath.Join(root, "private", "candidates.json")))
	ps = append(ps, probe(ProbeGenerationAuditRead, filepath.Join(root, "generation-audit", "audit.json")))
	ps = append(ps, probe(ProbeAuthorReceiptRead, filepath.Join(root, "author-receipts", "receipts.json")))
	ps = append(ps, probe(ProbePriorReviewRead, filepath.Join(root, "prior-reviews", "reviews.json")))
	if sibling == "" {
		// Fresh batch, no in-flight or retired sibling yet: probe the
		// permanent fixture instead — identical physical boundary (a
		// foreign locked input), deterministic target.
		sibling = filepath.Join(b.cfg.Root, "attempts", "sibling-fixture")
	}
	ps = append(ps, probe(ProbeActiveSiblingRead, siblingSentinel(sibling)))
	// The exact child receives the prompt as its argv — readable by
	// construction. The on-disk copy is the controller-side audit artifact
	// (created mode 0000), so a filesystem probe would conflate the audit
	// file with the child's actual input channel.
	ps = append(ps, AccessProbe{
		Kind: ProbeOwnInputRead, TargetPath: ownInput,
		TargetAccessPolicyDigest: "protected-root-v1",
		Expected:                 ProbeReadable,
		Observed:                 ProbeReadable,
	})
	return ps
}

// holdoutAuthorOutput is the strict closed shape an author must return.
type holdoutAuthorOutput struct {
	CaseID         string          `json:"case_id"`
	Module         string          `json:"module"`
	Lang           string          `json:"lang"`
	ScenarioBucket string          `json:"scenario_bucket"`
	Category       string          `json:"category"`
	Prompt         *string         `json:"prompt"`
	Turns          []Turn          `json:"turns"`
	SeedMemories   []SeedMemory    `json:"seed_memories"`
	WorkspaceFiles []WorkspaceFile `json:"workspace_files"`
	Expect         ExpectV2        `json:"expect"`
}

// laneFinalText extracts the assistant-visible text out of one lane's raw
// stream-json output. The lane CLI wraps the model's reply in its own event
// envelope; the candidate/review JSON the model produced lives inside the
// text blocks, where structural scanners cannot see it.
func laneFinalText(lane string, raw []byte) string {
	var evs []Event
	switch lane {
	case HostClaude:
		evs = ParseClaude(bytes.NewReader(raw))
	case HostCodex:
		evs = ParseCodex(bytes.NewReader(raw))
	case HostOpenCode:
		evs = ParseOpenCode(bytes.NewReader(raw))
	}
	var sb strings.Builder
	for _, e := range evs {
		if e.Kind == EventText {
			sb.WriteString(e.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func parseAuthorOutput(raw []byte, slot QuotaSlot, caseID string) (*TriggerCaseV2, error) {
	obj := firstObjectWithKey(string(raw), "case_id")
	if obj == nil {
		return nil, errors.New("author output carries no case object")
	}
	var out holdoutAuthorOutput
	if err := StrictParseClosed(obj, &out); err != nil {
		return nil, fmt.Errorf("author candidate: %w", err)
	}
	if out.CaseID != caseID {
		return nil, fmt.Errorf("case_id %q != assigned %q", out.CaseID, caseID)
	}
	if out.Module != slot.Module || out.Lang != slot.Lang || out.ScenarioBucket != slot.Scenario {
		return nil, fmt.Errorf("candidate labels diverge from the scheduled slot")
	}
	if (out.Prompt == nil) == (out.Turns == nil) {
		return nil, errors.New("candidate must carry exactly one of prompt|turns")
	}
	lang := out.Lang
	scen := out.ScenarioBucket
	c := &TriggerCaseV2{
		ID: caseID, SchemaVersion: 2, Split: "holdout", ScoreMembership: MembershipHoldout96,
		Module: out.Module, Lang: &lang, ScenarioBucket: &scen,
		Category: out.Category, Prompt: out.Prompt, Turns: out.Turns,
		SeedMemories: out.SeedMemories, WorkspaceFiles: out.WorkspaceFiles,
		Expect: out.Expect, Source: slot.Author + "-authoring", Status: StatusActive,
	}
	if err := ValidateCaseV2(c); err != nil {
		return nil, err
	}
	return c, nil
}

// holdoutReviewOutput is the strict closed shape a reviewer must return.
type holdoutReviewOutput struct {
	AttemptID              string   `json:"attempt_id"`
	AuthorAttemptID        string   `json:"author_attempt_id"`
	Verdict                string   `json:"verdict"`
	Novel                  bool     `json:"novel"`
	NearestFamilyID        *string  `json:"nearest_family_id"`
	NearestFamilyScope     *string  `json:"nearest_family_scope"`
	InferredModule         string   `json:"inferred_module"`
	InferredLang           string   `json:"inferred_lang"`
	InferredScenarioBucket string   `json:"inferred_scenario_bucket"`
	InferredCategory       string   `json:"inferred_category"`
	InferredExpect         ExpectV2 `json:"inferred_expect"`
	Reason                 string   `json:"reason"`
}

func parseReviewOutput(raw []byte, revAttID, authorAttID string) (ReviewRecord, error) {
	obj := firstObjectWithKey(string(raw), "attempt_id")
	if obj == nil {
		return ReviewRecord{}, errors.New("review output carries no review object")
	}
	var out holdoutReviewOutput
	if err := StrictParseClosed(obj, &out); err != nil {
		return ReviewRecord{}, fmt.Errorf("review record: %w", err)
	}
	if out.AttemptID != revAttID || out.AuthorAttemptID != authorAttID {
		return ReviewRecord{}, errors.New("review did not echo its attempt ids")
	}
	switch out.Verdict {
	case "accept", "reject":
	default:
		return ReviewRecord{}, fmt.Errorf("verdict %q invalid", out.Verdict)
	}
	d, err := NormalizedLabelDigest(out.InferredModule, out.InferredLang,
		out.InferredScenarioBucket, out.InferredCategory, out.InferredExpect)
	if err != nil {
		return ReviewRecord{}, err
	}
	return ReviewRecord{
		AttemptID: out.AttemptID, AuthorAttemptID: out.AuthorAttemptID,
		Verdict: out.Verdict, Novel: out.Novel,
		NearestFamilyID: out.NearestFamilyID, NearestFamilyScope: out.NearestFamilyScope,
		InferredModule: out.InferredModule, InferredLang: out.InferredLang,
		InferredScenarioBucket: out.InferredScenarioBucket, InferredCategory: out.InferredCategory,
		InferredExpect: out.InferredExpect, NormalizedLabelDigest: d,
		ReasonCode: out.Reason,
	}, nil
}

// terminalize appends the exactly-one terminal event for an attempt.
func (b *HoldoutBatch) terminalize(attID, stage, host, outcome, reason string) error {
	ev := AttemptEvent{
		EventKind: EventAttemptTerminal, AttemptID: attID, Stage: stage, Host: host,
		TerminalOutcome: &outcome, ReasonCode: &reason,
	}
	return b.persist.Ledger.AppendTerminal(ev)
}

// save atomically persists the batch state under the protected root, after
// re-checking the isolation aggregate honestly covers every launched attempt.
func (b *HoldoutBatch) save() error {
	if err := AggregateIsolationReceipts(b.persist.Ledger, b.persist.Probes); err != nil {
		return fmt.Errorf("isolation aggregate: %w", err)
	}
	raw, err := CanonicalJSON(b.persist)
	if err != nil {
		return err
	}
	// A per-save temp name: a shared batch.json.tmp lets two concurrent
	// saves interleave (B renames while A is still writing, or A's final
	// rename fails ENOENT after B's) — observed in the 96-slot run as
	// double-committed slots and lost Filled keys.
	tmp, err := os.CreateTemp(b.cfg.Root, "batch.json.tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(b.cfg.Root, "batch.json"))
}

// Run fills every unfilled slot (bounded worker pool over slots).
func (b *HoldoutBatch) Run(laneCfg CLIReviewConfig, only map[string]bool) error {
	slots, err := HoldoutQuotaSlots()
	if err != nil {
		return err
	}
	var todo []QuotaSlot
	seen := map[string]int{}
	todoOcc := map[string]int{} // slot table position → occurrence index
	for _, s := range slots {
		k := slotKey(s)
		occ := seen[k]
		seen[k]++
		if _, done := b.persist.Filled[FilledSlotKey(k, occ)]; done {
			continue
		}
		if only != nil && !only[k] {
			continue
		}
		todo = append(todo, s)
		todoOcc[slotKey(s)+"\x00"+fmt.Sprint(len(todo)-1)] = occ
	}
	if len(todo) == 0 {
		return nil
	}
	sem := make(chan struct{}, b.cfg.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex // serializes admission/persist mutations
	failures := map[string]int{}
	for i, s := range todo {
		occ := todoOcc[slotKey(s)+"\x00"+fmt.Sprint(i)]
		wg.Add(1)
		go func(s QuotaSlot, occ int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := b.fillSlot(s, laneCfg, &mu, occ); err != nil {
				mu.Lock()
				failures[FilledSlotKey(slotKey(s), occ)]++
				mu.Unlock()
			}
		}(s, occ)
	}
	wg.Wait()
	if len(failures) > 0 {
		keys := make([]string, 0, len(failures))
		for k := range failures {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Errorf("slots not filled: %s", strings.Join(keys, ", "))
	}
	return nil
}

// fillSlot regenerates one slot's candidate until it is admitted or the
// per-slot attempt budget is exhausted.
func (b *HoldoutBatch) fillSlot(slot QuotaSlot, laneCfg CLIReviewConfig, mu *sync.Mutex, occurrence int) error {
	otherHosts := []string{}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		if h != slot.Author {
			otherHosts = append(otherHosts, h)
		}
	}
	for attempt := 1; attempt <= b.cfg.MaxAttemptsPerSlot; attempt++ {
		mu.Lock()
		b.persist.CaseSeq++
		seq := b.persist.CaseSeq
		mu.Unlock()
		caseID := fmt.Sprintf("hol-%03d", seq)
		authorAttID := fmt.Sprintf("att-a-%03d-%d", seq, attempt)
		admitted, err := b.attemptOnce(slot, caseID, authorAttID, otherHosts, laneCfg, mu, occurrence)
		if err != nil {
			// Permanent per-attempt failures already terminalized; retry with
			// a fresh candidate unless the budget is gone.
			fmt.Fprintf(os.Stderr, "slot %s attempt %d: %v\n", slotKey(slot), attempt, err)
			if attempt == b.cfg.MaxAttemptsPerSlot {
				return fmt.Errorf("slot %s exhausted %d attempts: %w", slotKey(slot), attempt, err)
			}
			continue
		}
		if admitted {
			return nil
		}
	}
	return fmt.Errorf("slot %s: no candidate admitted", slotKey(slot))
}

// attemptOnce runs author → dual review → admission for one candidate.
func (b *HoldoutBatch) attemptOnce(slot QuotaSlot, caseID, authorAttID string, reviewerHosts []string, laneCfg CLIReviewConfig, mu *sync.Mutex, occurrence int) (bool, error) {
	// ---- author stage ----
	ws, err := b.stage.Acquire(StageAuthor, authorAttID)
	if err != nil {
		return false, err
	}
	// Retire the slot on EVERY exit path (an early return between Acquire and
	// the old deferred release leaked capacity and deadlocked the pool).
	var ownInput string
	defer func() {
		if ownInput != "" {
			_ = os.Chmod(ownInput, 0o000) // retired inputs stay unreadable
		}
		_ = b.stage.Release(authorAttID)
	}()
	prompt, err := b.authoringPrompt(slot, caseID)
	if err != nil {
		return false, err
	}
	ownInput = filepath.Join(ws, "input", "prompt.json")
	if err := os.MkdirAll(filepath.Dir(ownInput), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(ownInput, []byte(prompt), 0o000); err != nil {
		return false, err
	}
	if err := os.WriteFile(siblingSentinel(ws), []byte(authorAttID), 0o000); err != nil {
		return false, err
	}
	// The materialized input is created already-locked: there is no readable
	// window in which a concurrent sibling probe could observe it. The exact
	// child receives the prompt as its argv — the own-input probe records
	// that structural readability, the file is the controller-side audit copy.
	probes := b.probesFor(authorAttID, ownInput, b.stage.SnapshotActive(authorAttID))
	if err := b.persist.Ledger.AppendStarted(AttemptEvent{
		AttemptID: authorAttID, Stage: "author", Host: slot.Author,
		ToolIdentityDigest: "lane-frozen", ResolvedModel: laneDeclaredModel(slot.Author, laneCfg),
		PromptInputDigest:  b.authorP.SHA256,
	}); err != nil {
		return false, err
	}
	// Record the attempt's isolation/provenance/state-root evidence
	// immediately: even a launch-failed attempt is a launched attempt and
	// must carry complete receipts in the sealed aggregate.
	mu.Lock()
	b.persist.Probes[authorAttID] = probes
	b.persist.AuthorProvs = append(b.persist.AuthorProvs, buildLaneProvenance(slot.Author, laneCfg))
	b.persist.StateRoots = append(b.persist.StateRoots, ws)
	mu.Unlock()
	raw, err := runLaneCLIIn(slot.Author, laneCfg, prompt, ws)
	if err != nil {
		_ = b.terminalize(authorAttID, "author", slot.Author, "launch-failed", "spawn-error")
		return false, fmt.Errorf("author launch: %w", err)
	}
	_ = os.WriteFile(filepath.Join(ws, "output", "raw-stream.txt"), raw, 0o600)
	cand, err := parseAuthorOutput([]byte(laneFinalText(slot.Author, raw)), slot, caseID)
	if err != nil {
		_ = b.terminalize(authorAttID, "author", slot.Author, "parse-error", "malformed-case")
		return false, err
	}
	bc := BuildBlindCandidate(cand)
	blindDigest, err := BlindCandidateDigest(bc)
	if err != nil {
		return false, err
	}
	blindBytes, err := CanonicalJSON(bc)
	if err != nil {
		return false, err
	}
	if err := b.terminalize(authorAttID, "author", slot.Author, "candidate-ready", "ok"); err != nil {
		return false, err
	}

	// Author stage done — retire the author slot BEFORE the reviews. Holding
	// it across the review stage deadlocks the bounded pool: each review needs
	// its own slot, so two parallel slots would wait on each other forever.
	_ = os.Chmod(ownInput, 0o000) // retired input stays unreadable
	_ = b.stage.Release(authorAttID)
	ownInput = ""                 // the deferred retire must not double-fire

	// ---- dual review + admission (retry the pair once on a stale CAS) ----
	for casRound := 0; casRound < 2; casRound++ {
		mu.Lock()
		accepted := b.persist.Accepted
		mu.Unlock()
		var reviews []ReviewRecord
		for ri, host := range reviewerHosts {
			revAttID := fmt.Sprintf("%s-r%d-%d", authorAttID, casRound, ri)
			rec, rerr := b.runReview(host, revAttID, authorAttID, bc, blindDigest, accepted, laneCfg, mu)
			if rerr != nil {
				return false, rerr
			}
			reviews = append(reviews, rec)
		}
		in := AdmissionInput{
			AuthorAttemptID: authorAttID,
			AuthoringReceiptDigest: blindDigest,
			PrivateCandidateDigest: sha256Hex(raw),
			BlindCandidateDigest:   blindDigest,
			QuotaSlotDigest:        b.authorP.QuotaPlanDigest,
			QuotaSlot:              slot,
			AuthorModule:           cand.Module,
			AuthorLang:             *cand.Lang,
			AuthorScenario:         *cand.ScenarioBucket,
			AuthorCategory:         cand.Category,
			AuthorExpect:           cand.Expect,
			Reviews:                reviews,
			DevSummary:             b.devSum,
			AcceptedSummary:        AcceptedSummaryFor(accepted),
			BlindProjection:        blindBytes,
			FinalCaseID:            caseID,
		}
		mu.Lock()
		// Maintain the append-only receipt chain: sequence 1..n and the
		// prev-digest thread are assigned at append time (stale receipts
		// stay on the chain — replay skips their state transition).
		in.AdmissionSequence = len(b.persist.Admissions) + 1
		if n := len(b.persist.Admissions); n > 0 {
			prev := b.persist.Admissions[n-1].ReceiptDigest
			in.PreviousReceiptDigest = &prev
		}
		rec, next, aerr := TryAdmit(in, b.persist.Accepted)
		if aerr != nil {
			mu.Unlock()
			_ = b.terminalize(authorAttID, "author", slot.Author, "rejected", "gate")
			return false, aerr
		}
		b.persist.Admissions = append(b.persist.Admissions, rec)
		if rec.CASResult == "stale" {
			mu.Unlock()
			continue // rerun both reviews in fresh sessions against newest state
		}
		b.persist.Accepted = next
		cand.Reviews = reviews
		b.persist.Filled[FilledSlotKey(slotKey(slot), occurrence)] = cand
		if err := b.save(); err != nil {
			mu.Unlock()
			return false, err
		}
		mu.Unlock()
		_ = b.terminalize(authorAttID, "author", slot.Author, "admitted", "ok")
		return true, nil
	}
	_ = b.terminalize(authorAttID, "author", slot.Author, "rejected", "stale-cas")
	return false, errors.New("admission stayed stale after re-review")
}

// runReview launches one isolated reviewer and parses its closed output.
func (b *HoldoutBatch) runReview(host, revAttID, authorAttID string, bc *BlindCandidateV1, blindDigest string, accepted *AcceptedFamilyState, laneCfg CLIReviewConfig, mu *sync.Mutex) (ReviewRecord, error) {
	ws, err := b.stage.Acquire(StageReview, revAttID)
	if err != nil {
		return ReviewRecord{}, err
	}
	// Retire the slot on every exit path (leak → pool deadlock).
	var ownInput string
	defer func() {
		if ownInput != "" {
			_ = os.Chmod(ownInput, 0o000)
		}
		_ = b.stage.Release(revAttID)
	}()
	prompt, err := b.reviewPrompt(bc, blindDigest, accepted, revAttID, authorAttID)
	if err != nil {
		return ReviewRecord{}, err
	}
	ownInput = filepath.Join(ws, "input", "envelope.json")
	if err := os.MkdirAll(filepath.Dir(ownInput), 0o700); err != nil {
		return ReviewRecord{}, err
	}
	if err := os.WriteFile(ownInput, []byte(prompt), 0o000); err != nil {
		return ReviewRecord{}, err
	}
	if err := os.WriteFile(siblingSentinel(ws), []byte(revAttID), 0o000); err != nil {
		return ReviewRecord{}, err
	}
	probes := b.probesFor(revAttID, ownInput, b.stage.SnapshotActive(revAttID))
	if err := b.persist.Ledger.AppendStarted(AttemptEvent{
		AttemptID: revAttID, Stage: "review", Host: host,
		ToolIdentityDigest: "lane-frozen", ResolvedModel: laneDeclaredModel(host, laneCfg),
		PromptInputDigest:  b.reviewP.SHA256, AuthorAttemptID: &authorAttID,
	}); err != nil {
		return ReviewRecord{}, err
	}
	mu.Lock()
	b.persist.Probes[revAttID] = probes
	b.persist.ReviewProvs = append(b.persist.ReviewProvs, buildLaneProvenance(host, laneCfg))
	b.persist.StateRoots = append(b.persist.StateRoots, ws)
	mu.Unlock()
	raw, err := runLaneCLIIn(host, laneCfg, prompt, ws)
	if err != nil {
		_ = b.terminalize(revAttID, "review", host, "launch-failed", "spawn-error")
		return ReviewRecord{}, fmt.Errorf("reviewer %s launch: %w", host, err)
	}
	// Keep the raw stream in the attempt's controller-side output dir for
	// audit/debugging (private root; never enters the sealed payload).
	_ = os.WriteFile(filepath.Join(ws, "output", "raw-stream.txt"), raw, 0o600)
	rec, err := parseReviewOutput([]byte(laneFinalText(host, raw)), revAttID, authorAttID)
	if err != nil {
		_ = b.terminalize(revAttID, "review", host, "parse-error", "malformed-case")
		return ReviewRecord{}, err
	}
	rec.BlindCandidateDigest = blindDigest
	if err := b.terminalize(revAttID, "review", host, "review-complete", rec.ReasonCode); err != nil {
		return ReviewRecord{}, err
	}
	return rec, nil
}
