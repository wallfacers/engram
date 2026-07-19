package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/provider"
	"github.com/wallfacers/engram/store"
)

func TestAssociativeBenchFlagsAreForwardedAndFingerprinted(t *testing.T) {
	opt := options{assoc: true, assocDepth: 1}
	got := retrieverOptionsFor(opt)
	if !got.Associative || got.AssocDepth != 1 {
		t.Fatalf("retriever options = %+v, want associative depth 1", got)
	}
	flags := retrievalFingerprint(opt)
	var line result
	line.RetrievalFlags = flags
	raw, err := json.Marshal(line)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoded["retrieval_flags"] != "assoc=true;assoc_depth=1" {
		t.Fatalf("retrieval fingerprint = %v", decoded["retrieval_flags"])
	}
}

func TestClusterSweepBenchFlagIsForwardedAndFingerprinted(t *testing.T) {
	opt := options{clusterSweep: true}
	got := retrieverOptionsFor(opt)
	if !got.ClusterSweep {
		t.Fatalf("retriever options = %+v, want cluster sweep enabled", got)
	}
	if !strings.Contains(retrievalFingerprint(opt), "cluster_sweep=true") {
		t.Fatalf("cluster sweep retrieval fingerprint = %q", retrievalFingerprint(opt))
	}
	if strings.Contains(retrievalFingerprint(options{}), "cluster_sweep") {
		t.Fatalf("default retrieval fingerprint unexpectedly mentions cluster sweep: %q", retrievalFingerprint(options{}))
	}
}

func TestSweepBudgetGuardAndStatsCountOnlySweepHits(t *testing.T) {
	if !sweepOverBudget(options{}, true, provider.Usage{InputTokens: 7718}) {
		t.Fatal("sweep answer above 1.5x default baseline should be marked")
	}
	if sweepOverBudget(options{}, false, provider.Usage{InputTokens: 9000}) {
		t.Fatal("non-sweep answer should not be marked")
	}
	stats := statsFromRuns([][]result{{
		{QuestionID: "q1", Correct: true, SweepUsed: true, SweepOverBudget: true},
		{QuestionID: "q2", Correct: true, SweepUsed: true},
		{QuestionID: "q3", Correct: true, SweepOverBudget: true},
	}})
	if stats.SweepQuestions != 2 || stats.SweepOverBudget != 1 || stats.SweepOverBudgetRate != 0.5 {
		t.Fatalf("sweep budget stats = %+v, want 2/1/0.5", stats)
	}
}

func TestAnswerAndJudgeTracksSweepHitForBudgetGuard(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	vs := memory.NewVectorStore(st.DB())
	if err := es.Upsert(ctx, &memory.Entry{Name: "seed", Content: "root fact"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := es.PutEntities(ctx, "seed", []string{"root"}); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	if err := es.Upsert(ctx, &memory.Entry{Name: "cluster", Content: "cluster fact"}); err != nil {
		t.Fatalf("cluster: %v", err)
	}
	if err := es.PutEntities(ctx, "cluster", []string{"member"}); err != nil {
		t.Fatalf("cluster entity: %v", err)
	}
	if err := es.UpsertEdges(ctx, []memory.EntityEdge{{A: "root", B: "member", Kind: "co"}}); err != nil {
		t.Fatalf("edge: %v", err)
	}
	retriever := memory.NewRetrieverWithOptions(es, vs, nil, nil, memory.RetrieverOptions{ClusterSweep: true})
	answer := func(context.Context, string, string) (string, provider.Usage, error) {
		return "root fact", provider.Usage{InputTokens: 8000, OutputTokens: 5}, nil
	}
	judge := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"correct":true}`, provider.Usage{}, nil
	}
	noRetry := func(context.Context, string, string) (string, error) { return "", nil }
	correct, _, usage, sweepUsed := answerAndJudgeWithUsage(ctx, retriever, answer, noRetry, noRetry, judge, options{topK: 2, noIDKRetry: true}, locomoQA{
		Question: "What things did root do?",
		Answer:   []byte(`"root fact"`),
		Category: 1,
	}, slog.Default())
	if !correct || !sweepUsed || !sweepOverBudget(options{}, sweepUsed, usage) {
		t.Fatalf("answer result = correct:%v sweep:%v usage:%+v", correct, sweepUsed, usage)
	}
}

func TestTemporalBenchFlagsAreForwardedAndFingerprinted(t *testing.T) {
	anchor := time.Date(2024, time.June, 15, 0, 0, 0, 0, time.UTC)
	opt := options{temporalScore: true, temporalHardFilter: true}
	got := retrieverOptionsForAt(opt, anchor)
	if !got.TemporalScore || !got.TemporalHardFilter || !got.Now.Equal(anchor) {
		t.Fatalf("temporal retriever options = %+v, want enabled flags and anchor", got)
	}
	flags := retrievalFingerprint(opt)
	if !strings.Contains(flags, "temporal_score=true") || !strings.Contains(flags, "temporal_hard_filter=true") {
		t.Fatalf("temporal retrieval fingerprint = %q", flags)
	}
}

func TestTemporalAnchorUsesLatestSessionDate(t *testing.T) {
	want := time.Date(2024, time.June, 15, 0, 0, 0, 0, time.UTC)
	conv := conversation{Sessions: []session{
		{Index: 1, Date: time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC)},
		{Index: 2, Date: want},
	}}
	if got := temporalNowForConversation(conv); !got.Equal(want) {
		t.Fatalf("temporal conversation anchor = %v, want %v", got, want)
	}
}

func TestTemporalAnswerPromptPlanAndForceVariant(t *testing.T) {
	plan := answerPromptForOptionsWithTemporal(2, false, true)
	for _, phrase := range []string{"TEMPORAL REASONING PLAN", "[event: YYYY-MM-DD]", "normalize", "compare", "never ISO"} {
		if !strings.Contains(plan, phrase) {
			t.Fatalf("temporal prompt missing %q: %s", phrase, plan)
		}
	}
	force := answerPromptForOptionsWithTemporal(2, true, true)
	if !strings.Contains(force, "TEMPORAL REASONING PLAN") || !strings.Contains(force, "best guess") {
		t.Fatalf("force temporal prompt lost plan or best-guess instruction: %s", force)
	}
	if strings.Contains(strings.ToLower(force), "i don't know") || strings.Contains(strings.ToLower(force), "do not know") {
		t.Fatalf("force temporal prompt retains refusal outlet: %s", force)
	}
	if answerPromptFor(1) == plan || answerPromptFor(3) == plan {
		t.Fatal("temporal prompt leaked into non-temporal categories")
	}
}

func TestTemporalAnswerPromptIsOptIn(t *testing.T) {
	if answerPromptFor(2) != answerSystemPrompt {
		t.Fatal("default category 2 prompt changed from the pre-temporal baseline")
	}
	if answerPromptForOptions(2, true) != forceAnswerSystemPrompt {
		t.Fatal("default forced category 2 prompt changed from the pre-temporal baseline")
	}
	if answerPromptForOptionsWithTemporal(2, false, true) != temporalAnswerPrompt {
		t.Fatal("temporal answer flag did not select temporal prompt")
	}
	if answerPromptForOptionsWithTemporal(2, true, true) != forceTemporalAnswerPrompt {
		t.Fatal("temporal answer flag did not select force temporal prompt")
	}
}

func TestValidateTemporalStoreRejectsLegacyFacts(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	// A pre-temporal store carries event_date (the old extraction always
	// emitted it) but no event ranges or aliases — that signature is rejected.
	legacyDate := time.Date(2023, 5, 7, 0, 0, 0, 0, time.UTC)
	if err := es.Upsert(ctx, &memory.Entry{Name: "legacy-fact", Content: "The user visited Oslo.", EventDate: &legacyDate}); err != nil {
		t.Fatalf("insert legacy fact: %v", err)
	}
	if err := validateTemporalStore(ctx, st.DB(), 1); err == nil {
		t.Fatal("legacy facts without temporal fields or aliases should be rejected")
	}
}

func TestValidateTemporalStoreAcceptsDatelessExtraction(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	// A store legitimately built with the new pipeline whose extraction found
	// no dates at all must pass — rebuilding would reproduce the same state.
	if err := es.Upsert(ctx, &memory.Entry{Name: "dateless-fact", Content: "The user likes tea."}); err != nil {
		t.Fatalf("insert dateless fact: %v", err)
	}
	if err := validateTemporalStore(ctx, st.DB(), 1); err != nil {
		t.Fatalf("dateless new-pipeline store should pass: %v", err)
	}
}

func TestAssocDepthAboveMaximumIsRejected(t *testing.T) {
	if err := validateAssocDepth(3); err == nil {
		t.Fatal("assoc depth 3 should be rejected at startup")
	}
	if err := validateAssocDepth(2); err != nil {
		t.Fatalf("assoc depth 2 rejected: %v", err)
	}
	if got := retrievalFingerprint(options{assoc: true, assocDepth: 0}); got != "assoc=true;assoc_depth=2" {
		t.Fatalf("zero assoc depth fingerprint = %q", got)
	}
}

func TestParseLoCoMoDate(t *testing.T) {
	cases := map[string]bool{ // input → expect non-zero
		"1:56 pm on 8 May, 2023":  true,
		"7:00 pm on 25 May, 2023": true,
		"8 May, 2023":             true,
		"":                        false,
		"garbage":                 false,
	}
	for in, wantOK := range cases {
		got := parseLoCoMoDate(in)
		if got.IsZero() == wantOK {
			t.Errorf("parseLoCoMoDate(%q) = %v (zero=%v), want non-zero=%v", in, got, got.IsZero(), wantOK)
		}
	}
	// Spot-check a parsed value.
	if d := parseLoCoMoDate("1:56 pm on 8 May, 2023"); d.Year() != 2023 || d.Month() != time.May || d.Day() != 8 {
		t.Errorf("date fields wrong: %v", d)
	}
}

func TestParseJudgeVerdict(t *testing.T) {
	cases := map[string]bool{
		`{"correct": true}`:                        true,
		`{"correct":false}`:                        false,
		"The verdict is correct: true.":            true,
		"correct is false because it contradicts":  false,
		"no verdict token here":                    false,
		`{"correct": true, "note":"ignore false"}`: true,
	}
	for in, want := range cases {
		if got := parseJudgeVerdict(in); got != want {
			t.Errorf("parseJudgeVerdict(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAggregatorAndPct(t *testing.T) {
	a := newAggregator()
	a.add(4, true)
	a.add(4, false)
	a.add(1, true)
	if a.byCategory[4].total != 2 || a.byCategory[4].correct != 1 {
		t.Fatalf("cat 4 stats wrong: %+v", a.byCategory[4])
	}
	if pct(1, 2) != 50 {
		t.Fatalf("pct(1,2)=%v", pct(1, 2))
	}
	if pct(0, 0) != 0 {
		t.Fatalf("pct(0,0) should be 0")
	}
}

func TestRetrievedMemoryLine(t *testing.T) {
	m := retrievedMemory{Content: "moved to Berlin", EventDate: "2019-05-01", Recorded: "2026-07-16"}
	got := m.Line()
	want := "[event: 2019-05-01] [recorded: 2026-07-16] moved to Berlin"
	if got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}

func TestSweepAnswerPromptGroupsMemoriesBySessionAndDate(t *testing.T) {
	memories := []retrievedMemory{
		{Name: "late", Content: "late event", EventDate: "2023-05-03", SourceSessionID: "conv0-sess2"},
		{Name: "early", Content: "early event", EventDate: "2023-05-01", SourceSessionID: "conv0-sess1"},
		{Name: "middle", Content: "middle event", EventDate: "2023-05-02", SourceSessionID: "conv0-sess1"},
	}
	got := buildSweepAnswerPrompt("What happened?", memories)
	want := "RETRIEVED MEMORIES:\n" +
		"[session 1, 2023-05-01]\n" +
		"1. [event: 2023-05-01] early event\n" +
		"2. [event: 2023-05-02] middle event\n" +
		"[session 2, 2023-05-03]\n" +
		"3. [event: 2023-05-03] late event\n" +
		"\nQUESTION: What happened?\n\nAnswer:"
	if got != want {
		t.Fatalf("sweep prompt = %q, want %q", got, want)
	}
}

func TestJournalResume(t *testing.T) {
	dir := t.TempDir()
	j, err := openJournal(dir, "hybrid")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	j.write(result{Conv: 0, Q: 3, Category: 4, Correct: true, Question: "q", Gold: "g", Predicted: "p"})
	j.Close()

	// Reopen: prior result must be visible for resume.
	j2, err := openJournal(dir, "hybrid")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer j2.Close()
	r, ok := j2.lookup(resultKey{Conv: 0, Q: 3})
	if !ok || !r.Correct {
		t.Fatalf("resume lookup failed: r=%+v ok=%v", r, ok)
	}
	// A different retrieval mode has its own file (no cross-contamination).
	if _, err := filepath.Glob(filepath.Join(dir, "results-*.jsonl")); err != nil {
		t.Fatalf("glob: %v", err)
	}
}

func TestArmsFor(t *testing.T) {
	cases := map[string][]string{
		"fts":                 {"fts"},
		"hybrid":              {"hybrid"},
		"both":                {"fts", "hybrid"},
		"hybrid,hybrid+assoc": {"hybrid", "hybrid+assoc"},
		"hybrid+sweep":        {"hybrid+sweep"},
	}
	for in, want := range cases {
		got, err := armsFor(in)
		if err != nil {
			t.Fatalf("armsFor(%q) err: %v", in, err)
		}
		if len(got) != len(want) {
			t.Fatalf("armsFor(%q) = %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("armsFor(%q) = %v, want %v", in, got, want)
			}
		}
	}
	for _, in := range []string{"bogus", "hybrid+", "hybrid+bogus", "hybrid+conflict", "hybrid+abstain", "hybrid,hybrid"} {
		if _, err := armsFor(in); err == nil {
			t.Fatalf("armsFor(%q) should error", in)
		}
	}
	if !hasArm([]string{"fts", "hybrid"}, "hybrid") || hasArm([]string{"fts"}, "hybrid") {
		t.Fatal("hasArm wrong")
	}
}

func TestUnsupportedMechanismSuffixesExplainFuturePhase(t *testing.T) {
	for _, arm := range []string{"hybrid+conflict", "hybrid+abstain"} {
		_, err := armsFor(arm)
		if err == nil || !strings.Contains(err.Error(), "not implemented until US4/US5") {
			t.Fatalf("armsFor(%q) err = %v, want US4/US5 error", arm, err)
		}
	}
	if arms, err := armsFor("hybrid+temporal"); err != nil || len(arms) != 1 || arms[0] != "hybrid+temporal" {
		t.Fatalf("temporal arm should be supported: arms=%v err=%v", arms, err)
	}
}

func TestThreeArmPairingEmitsLimitWarning(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	warnExtraPairedArms(logger, []string{"fts", "hybrid", "hybrid+assoc"})
	if !strings.Contains(logs.String(), "first two") {
		t.Fatalf("pairing warning = %q, want first two arms explanation", logs.String())
	}
}

func TestAnswerRegimeFingerprintIsTraceable(t *testing.T) {
	got := answerRegimeFingerprint(options{forceAnswer: true})
	if !strings.Contains(got, "force_answer=true") || !strings.Contains(got, "abstain_prompt=false") {
		t.Fatalf("answer regime fingerprint = %q", got)
	}
}

func TestArmSuffixOverridesGlobalMechanisms(t *testing.T) {
	global := options{assoc: true, temporalScore: true, conflictResolution: true, abstainPrompt: true}
	plain := optionsForArm(global, "hybrid")
	if plain.assoc || plain.temporalScore || plain.conflictResolution || plain.abstainPrompt {
		t.Fatalf("plain arm should be zero mechanisms when parsed as baseline: %+v", plain)
	}
	assoc := optionsForArm(options{}, "hybrid+assoc")
	if !assoc.assoc || assoc.temporalScore || assoc.conflictResolution || assoc.abstainPrompt {
		t.Fatalf("assoc suffix did not override global mechanisms: %+v", assoc)
	}
	sweep := optionsForArm(options{assoc: true}, "hybrid+sweep")
	if !sweep.clusterSweep || sweep.assoc {
		t.Fatalf("sweep suffix did not override global mechanisms: %+v", sweep)
	}
	single := optionsForRun(global, "hybrid", false)
	if !single.assoc || !single.temporalScore || !single.conflictResolution || !single.abstainPrompt {
		t.Fatalf("single arm lost global mechanisms: %+v", single)
	}
	pairedBaseline := optionsForRun(global, "hybrid", true)
	if pairedBaseline.assoc || pairedBaseline.temporalScore || pairedBaseline.conflictResolution || pairedBaseline.abstainPrompt {
		t.Fatalf("paired baseline leaked global mechanisms: %+v", pairedBaseline)
	}
}

func TestTPlanArmMechanismControlsTemporalAnswerPrompt(t *testing.T) {
	baseline := optionsForArm(options{}, "hybrid")
	tplan := optionsForArm(options{}, "hybrid+tplan")
	if baseline.temporalAnswerPrompt || !tplan.temporalAnswerPrompt {
		t.Fatalf("tplan pairing prompt flags = baseline:%t tplan:%t, want false/true", baseline.temporalAnswerPrompt, tplan.temporalAnswerPrompt)
	}

	global := options{temporalAnswerPrompt: true}
	legacyBaseline := optionsForArm(global, "hybrid")
	legacyTemporal := optionsForArm(global, "hybrid+temporal")
	if !legacyBaseline.temporalAnswerPrompt || !legacyTemporal.temporalAnswerPrompt {
		t.Fatalf("global temporal prompt compatibility flags = baseline:%t temporal:%t, want true/true", legacyBaseline.temporalAnswerPrompt, legacyTemporal.temporalAnswerPrompt)
	}

	combined := optionsForArm(options{}, "hybrid+tplan+temporal")
	if !combined.temporalAnswerPrompt || !combined.temporalScore {
		t.Fatalf("tplan+temporal arm = %+v, want independent prompt and retrieval mechanisms", combined)
	}
}

func TestPairedReportSchemaAndWrite(t *testing.T) {
	a := [][]result{{
		{QuestionID: "q1", Correct: false, Category: 1},
		{QuestionID: "q2", Correct: true, Category: 4},
	}}
	b := [][]result{{
		{QuestionID: "q1", Correct: true, Category: 1},
		{QuestionID: "q2", Correct: true, Category: 4},
	}}
	report, err := pairedReport(a, b)
	if err != nil {
		t.Fatalf("paired report: %v", err)
	}
	if !report.PairedInProcess || report.FlipsAToB != 1 || report.FlipsBToA != 0 || len(report.Questions) != 2 {
		t.Fatalf("paired report = %+v", report)
	}
	path := filepath.Join(t.TempDir(), "paired.json")
	if err := writePaired(path, report); err != nil {
		t.Fatalf("write paired: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read paired: %v", err)
	}
	var decoded struct {
		Questions       []pairedQuestion `json:"questions"`
		McNemarP        float64          `json:"mcnemar_p"`
		PairedInProcess bool             `json:"paired_in_process"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode paired: %v", err)
	}
	if !decoded.PairedInProcess || len(decoded.Questions) != 2 || decoded.McNemarP <= 0 {
		t.Fatalf("paired JSON = %+v", decoded)
	}
}

func TestForceAnswerPromptsRequireBestGuess(t *testing.T) {
	for _, category := range []int{1, 2, 3} {
		defaultPrompt := answerPromptFor(category)
		forced := answerPromptForOptions(category, true)
		if strings.Contains(strings.ToLower(forced), "i don't know") {
			t.Fatalf("category %d force prompt still exposes IDK: %q", category, forced)
		}
		if !strings.Contains(strings.ToLower(forced), "best guess") {
			t.Fatalf("category %d force prompt lacks best-guess instruction: %q", category, forced)
		}
		if category == 2 && !strings.Contains(strings.ToLower(forced), "best supported inference") {
			t.Fatalf("category %d force prompt lost inference instruction: %q", category, forced)
		}
		if category == 3 && !strings.Contains(forced, "COMBINE") {
			t.Fatalf("open-domain force prompt lost COMBINE instruction: %q", forced)
		}
		if got := answerPromptForOptions(category, false); got != defaultPrompt {
			t.Fatalf("category %d default prompt changed", category)
		}
	}
}

func TestForceAnswerAndAbstainPromptAreMutuallyExclusive(t *testing.T) {
	if err := validatePromptModes(options{forceAnswer: true, abstainPrompt: true}); err == nil {
		t.Fatal("expected force-answer and abstain-prompt conflict")
	}
	if err := validatePromptModes(options{forceAnswer: true}); err != nil {
		t.Fatalf("force-answer alone rejected: %v", err)
	}
}

func TestArmJournalsKeepSuffixSpecificResultFiles(t *testing.T) {
	dir := t.TempDir()
	for _, arm := range []string{"hybrid", "hybrid+assoc"} {
		j, err := openJournal(dir, arm)
		if err != nil {
			t.Fatalf("open %s: %v", arm, err)
		}
		j.write(result{Conv: 0, Q: 0, QuestionID: arm, Correct: true})
		j.Close()
	}
	for _, arm := range []string{"hybrid", "hybrid+assoc"} {
		path := filepath.Join(dir, "results-"+arm+".jsonl")
		items, err := readResultsJSONL(path)
		if err != nil || len(items) != 1 || items[0].QuestionID != arm {
			t.Fatalf("results for %s = %+v err=%v", arm, items, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "results.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("unexpected canonical results.jsonl: err=%v", err)
	}
}

func TestPairedReportRejectsUnalignedQuestions(t *testing.T) {
	_, err := pairedReport(
		[][]result{{{QuestionID: "q-a", Correct: true}}},
		[][]result{{{QuestionID: "q-b", Correct: true}}},
	)
	if err == nil {
		t.Fatal("expected unaligned paired report to fail")
	}
}

func TestGateBoundsConcurrency(t *testing.T) {
	sem := make(chan struct{}, 2)
	var mu sync.Mutex
	inflight, peak := 0, 0
	base := func(ctx context.Context, _, _ string) (string, error) {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
		return "ok", nil
	}
	gated := gate(sem, base)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = gated(context.Background(), "", "") }()
	}
	wg.Wait()
	if peak > 2 {
		t.Fatalf("gate allowed %d concurrent, cap was 2", peak)
	}
}

func TestParseDataset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mini.json")
	data := `[
	  {
	    "qa": [
	      {"question":"Where did the user move from?","answer":"Sweden","category":4},
	      {"question":"adversarial one","answer":"n/a","category":5}
	    ],
	    "conversation": {
	      "speaker_a":"Alex","speaker_b":"Sam",
	      "session_1_date_time":"1:56 pm on 8 May, 2023",
	      "session_1":[
	        {"speaker":"Alex","text":"I moved from Sweden."},
	        {"speaker":"Sam","text":"Nice."}
	      ]
	    }
	  }
	]`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	convs, err := loadDataset(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}
	c := convs[0]
	if len(c.Sessions) != 1 || len(c.Sessions[0].Turns) != 2 {
		t.Fatalf("session parse wrong: %+v", c.Sessions)
	}
	if c.Sessions[0].Date.Year() != 2023 {
		t.Fatalf("session date not parsed: %v", c.Sessions[0].Date)
	}
	if len(c.QA) != 2 || c.QA[0].AnswerText() != "Sweden" {
		t.Fatalf("qa parse wrong: %+v", c.QA)
	}
}

func TestAnswerPromptFor(t *testing.T) {
	if answerPromptFor(3) != openDomainAnswerPrompt {
		t.Fatal("category 3 must use the open-domain prompt")
	}
	if answerPromptFor(1) != multiHopAnswerPrompt {
		t.Fatal("category 1 must use the multi-hop aggregation prompt")
	}
	if answerPromptFor(2) != answerSystemPrompt {
		t.Fatal("category 2 must use the pre-temporal extraction prompt by default")
	}
	for _, c := range []int{4} {
		if answerPromptFor(c) != answerSystemPrompt {
			t.Fatalf("category %d must use the extraction prompt", c)
		}
	}
}

func TestIsIDK(t *testing.T) {
	cases := map[string]bool{
		"I don't know":                          true,
		"  i do not know. ":                     true,
		"That is not mentioned in the memories": true,
		"":                                      true,
		"Sweden":                                false,
		"May 2023":                              false,
	}
	for in, want := range cases {
		if got := isIDK(in); got != want {
			t.Fatalf("isIDK(%q) = %v, want %v", in, got, want)
		}
	}
}

type usageTestProvider struct{}

func (usageTestProvider) Name() string { return "usage-test" }

func (usageTestProvider) Stream(context.Context, provider.Request) (<-chan provider.ProviderEvent, error) {
	ch := make(chan provider.ProviderEvent, 3)
	ch <- provider.ProviderEvent{Type: provider.EventTextDelta, TextDelta: "answer"}
	ch <- provider.ProviderEvent{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 12, OutputTokens: 5}}
	ch <- provider.ProviderEvent{Type: provider.EventStop, StopReason: "end_turn"}
	close(ch)
	return ch, nil
}

func TestModelCallerForwardsProviderUsage(t *testing.T) {
	var gotRole, gotModel string
	var gotUsage provider.Usage
	call := newModelCallerWithUsage(usageTestProvider{}, "test-model", 100, "answer", func(role, model string, usage provider.Usage) {
		gotRole, gotModel, gotUsage = role, model, usage
	})
	text, err := call(context.Background(), "system", "question")
	if err != nil || text != "answer" {
		t.Fatalf("call = %q, err=%v", text, err)
	}
	if gotRole != "answer" || gotModel != "test-model" || gotUsage.InputTokens != 12 || gotUsage.OutputTokens != 5 {
		t.Fatalf("usage hook = role=%q model=%q usage=%+v", gotRole, gotModel, gotUsage)
	}
}

func TestRepeatedAnswersReusePreparedConversation(t *testing.T) {
	ctx := context.Background()
	conv := conversation{
		ID: 7,
		Sessions: []session{{
			Index: 1,
			Turns: []turn{{Speaker: "user", Text: "I live in Oslo."}},
		}},
		QA: []locomoQA{{
			Question: "Where do I live?",
			Answer:   []byte(`"Oslo"`),
			Category: 4,
		}},
	}
	var extractCalls, answerCalls, judgeCalls atomic.Int32
	extract := func(context.Context, string, string) (string, error) {
		extractCalls.Add(1)
		return `{"facts":[{"fact":"The user lives in Oslo.","entities":["Oslo"],"category":"user","durability":"evergreen"}]}`, nil
	}
	answer := func(context.Context, string, string) (string, error) {
		answerCalls.Add(1)
		return "Oslo", nil
	}
	judge := func(context.Context, string, string) (string, error) {
		judgeCalls.Add(1)
		return `{"correct":true}`, nil
	}
	opt := options{
		datasetFormat: "locomo",
		retrieval:     "fts",
		topK:          5,
		storeDir:      t.TempDir(),
		noIDKRetry:    true,
	}
	runtime, err := buildConversationRuntime(ctx, opt, conv, extract, nil, []string{"fts"}, slog.Default())
	if err != nil {
		t.Fatalf("build conversation: %v", err)
	}
	defer runtime.Close()

	for repeat := 1; repeat <= 3; repeat++ {
		runDir := t.TempDir()
		journal, err := openJournal(runDir, "fts")
		if err != nil {
			t.Fatalf("open journal: %v", err)
		}
		state := &armState{name: "fts", agg: newAggregator(), journal: journal}
		if err := answerConversation(ctx, opt, conv, runtime, answer, judge, []*armState{state}, slog.Default()); err != nil {
			t.Fatalf("answer repeat %d: %v", repeat, err)
		}
		journal.Close()
	}
	if got := extractCalls.Load(); got != 1 {
		t.Fatalf("extract calls = %d, want 1 across 3 repeats", got)
	}
	if got := answerCalls.Load(); got != 3 || judgeCalls.Load() != 3 {
		t.Fatalf("answer/judge calls = %d/%d, want 3/3", got, judgeCalls.Load())
	}
}

func TestAnswerJournalStoresFinalAnswerUsage(t *testing.T) {
	ctx := context.Background()
	conv := conversation{
		ID:       8,
		Sessions: []session{{Index: 1, Turns: []turn{{Speaker: "user", Text: "I live in Oslo."}}}},
		QA:       []locomoQA{{Question: "Where do I live?", Answer: []byte(`"Oslo"`), Category: 4}},
	}
	opt := options{datasetFormat: "locomo", retrieval: "fts", topK: 5, storeDir: t.TempDir(), noIDKRetry: true}
	extract := func(context.Context, string, string) (string, error) {
		return `{"facts":[{"fact":"The user lives in Oslo.","entities":["Oslo"],"category":"user"}]}`, nil
	}
	runtime, err := buildConversationRuntime(ctx, opt, conv, extract, nil, []string{"fts"}, slog.Default())
	if err != nil {
		t.Fatalf("build conversation: %v", err)
	}
	defer runtime.Close()
	answer := func(context.Context, string, string) (string, provider.Usage, error) {
		return "Oslo", provider.Usage{InputTokens: 11, OutputTokens: 7}, nil
	}
	filter := func(context.Context, string, string) (string, error) { return "", nil }
	judge := func(context.Context, string, string) (string, provider.Usage, error) {
		return `{"correct":true}`, provider.Usage{InputTokens: 13, OutputTokens: 2}, nil
	}
	runDir := t.TempDir()
	journal, err := openJournal(runDir, "fts")
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	state := &armState{name: "fts", agg: newAggregator(), journal: journal}
	if err := answerConversationWithUsage(ctx, opt, conv, runtime, answer, filter, filter, judge, []*armState{state}, slog.Default()); err != nil {
		t.Fatalf("answer: %v", err)
	}
	journal.Close()
	items, err := readResultsJSONL(filepath.Join(runDir, "results-fts.jsonl"))
	if err != nil || len(items) != 1 {
		t.Fatalf("journal items = %d, err=%v", len(items), err)
	}
	if items[0].InputTokens != 11 || items[0].OutputTokens != 7 || items[0].AnswerContextTokens != 11 {
		t.Fatalf("journal usage = %+v, want answer 11/7/context 11", items[0])
	}
	if items[0].RetrievalFlags != "assoc=false;assoc_depth=2" {
		t.Fatalf("journal retrieval flags = %q", items[0].RetrievalFlags)
	}
}
