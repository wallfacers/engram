package main

// The judge golden fixture guards Mem0-aligned leniency without making normal
// CI call an LLM. Its endpoint-backed layer runs only with LOCOMO_JUDGE_GOLDEN=1;
// an expected-wrong case judged correct is a fatal failure, not a soft mismatch.
import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type judgeGoldenCase struct {
	Question        string `json:"question"`
	Gold            string `json:"gold"`
	Predicted       string `json:"predicted"`
	ExpectedCorrect bool   `json:"expected_correct"`
	Rule            string `json:"rule"`
	Note            string `json:"note"`
}

const strictJudgeSystemPromptBaseline = `You grade a predicted answer against a gold answer for a question about a conversation, aligned with the LoCoMo / mem0 LLM-as-a-judge convention. Output STRICT JSON only: {"correct": true|false}.

Mark "correct": true when the prediction conveys the SAME key fact as the gold answer. Be lenient on form, strict on fact:
- Ignore wording, verbosity, and extra correct detail. A more detailed answer that still contains the gold fact is correct (e.g. gold "reminding herself of her successes" vs prediction "she reminds herself of her successes and progress" → true).
- Accept synonyms and paraphrases of the same fact (e.g. "a trophy" vs "first place" for a contest prize → true).
- Accept a coarser-but-consistent date (gold "May 2023" vs prediction "May 2023" or "8 May 2023" → true); mark false only if the date actually differs.
- Mark false when the prediction contradicts the gold fact, omits it, gives a wrong name/date/number, or says it does not know.`

func TestJudgeMem0AlignedPromptIncludesRules(t *testing.T) {
	prompt := strings.ToLower(judgeSystemPromptFor("mem0-aligned"))
	for _, rule := range []struct {
		name    string
		clauses []string
	}{
		{name: "partial credit", clauses: []string{"at least one correct item"}},
		{name: "paraphrases", clauses: []string{"synonyms and paraphrases"}},
		{name: "extra detail", clauses: []string{"extra details"}},
		{name: "date and duration tolerance", clauses: []string{"within 14 days", "within 50%"}},
		{name: "semantic overlap", clauses: []string{"same emotional valence"}},
		{name: "same referent", clauses: []string{"same named entity"}},
		{name: "fact focus", clauses: []string{"facts rather than wording"}},
	} {
		for _, clause := range rule.clauses {
			if !strings.Contains(prompt, clause) {
				t.Errorf("%s rule missing clause %q", rule.name, clause)
			}
		}
	}
}

func TestParseJudgeVerdictGoldenCases(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{name: "strict true JSON", raw: `{"correct": true}`, want: true},
		{name: "strict false JSON", raw: `{"correct": false}`, want: false},
		{name: "mixed case with prose", raw: `Verdict: {"CoRrEcT": TRUE}`, want: true},
		{name: "false before unrelated true", raw: `{"correct": false, "reviewed": true}`, want: false},
		{name: "true before unrelated false", raw: `{"correct": true, "reviewed": false}`, want: true},
		{name: "missing field", raw: `{"verdict": true}`, want: false},
		{name: "no JSON", raw: "wrong", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseJudgeVerdict(tc.raw); got != tc.want {
				t.Fatalf("parseJudgeVerdict(%q) = %t, want %t", tc.raw, got, tc.want)
			}
		})
	}
}

func TestJudgeSystemPromptForStrictIsUnchanged(t *testing.T) {
	if judgeSystemPrompt != strictJudgeSystemPromptBaseline {
		t.Fatal("strict judge prompt no longer matches the pre-alignment baseline")
	}
	if got := judgeSystemPromptFor("strict"); got != strictJudgeSystemPromptBaseline {
		t.Fatalf("strict judge prompt changed:\n--- got ---\n%s\n--- want ---\n%s", got, strictJudgeSystemPromptBaseline)
	}
}

func TestAnswerRegimeFingerprintSeparatesJudgeModes(t *testing.T) {
	strict := answerRegimeFingerprint(options{})
	aligned := answerRegimeFingerprint(options{judgeMem0Aligned: true})
	if strings.Contains(strict, "judge=mem0-aligned") {
		t.Fatalf("strict fingerprint unexpectedly contains judge alignment: %q", strict)
	}
	if aligned == strict {
		t.Fatalf("judge modes share fingerprint %q", strict)
	}
	if !strings.HasSuffix(aligned, ";judge=mem0-aligned") {
		t.Fatalf("aligned fingerprint = %q, want judge alignment suffix", aligned)
	}
}

func TestJudgeMem0AlignedMatchesGolden(t *testing.T) {
	if os.Getenv("LOCOMO_JUDGE_GOLDEN") != "1" {
		t.Skip("set LOCOMO_JUDGE_GOLDEN=1 to run the endpoint-backed judge golden cases")
	}
	judgeConfig := resolveJudgeConfig(os.Getenv)
	if judgeConfig.APIKey == "" || judgeConfig.BaseURL == "" || judgeConfig.Model == "" {
		t.Skip("JUDGE_API_KEY, JUDGE_BASE_URL, and JUDGE_MODEL (or corresponding LOCOMO_* fallbacks) are required for judge golden cases")
	}

	prov, err := buildBenchProvider(judgeConfig.Provider, judgeConfig.APIKey, judgeConfig.BaseURL, 1024, "JUDGE_PROVIDER")
	if err != nil {
		t.Fatalf("build judge provider: %v", err)
	}
	judgeCall := newModelCaller(prov, judgeConfig.Model, 1024)
	cases := loadJudgeGoldenCases(t)
	ctx := context.Background()
	// The relay judge model is transiently flaky (empty bodies, GOAWAY/EOF). Retry
	// so the guardrail is deterministic; an empty verdict is treated as transient
	// too, otherwise parseJudgeVerdict("")==false would let an expected-wrong case
	// pass for the wrong reason.
	judgeWithRetry := func(t *testing.T, tc judgeGoldenCase) string {
		t.Helper()
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			verdict, err := judgeCall(ctx, judgeSystemPromptFor("mem0-aligned"), buildJudgePrompt(tc.Question, tc.Gold, tc.Predicted))
			if err == nil && strings.TrimSpace(verdict) != "" {
				return verdict
			}
			lastErr = err
		}
		t.Fatalf("judge call failed after 3 attempts: rule=%s err=%v", tc.Rule, lastErr)
		return ""
	}

	for _, tc := range cases {
		t.Run(tc.Rule, func(t *testing.T) {
			verdict := judgeWithRetry(t, tc)
			got := parseJudgeVerdict(verdict)
			if got == tc.ExpectedCorrect {
				return
			}
			if !tc.ExpectedCorrect && got {
				t.Fatalf("expected-wrong case judged CORRECT: rule=%s question=%q gold=%q predicted=%q verdict=%q", tc.Rule, tc.Question, tc.Gold, tc.Predicted, verdict)
			}
			t.Errorf("judge verdict = %t, want %t: rule=%s note=%s raw=%q", got, tc.ExpectedCorrect, tc.Rule, tc.Note, verdict)
		})
	}
}

func loadJudgeGoldenCases(t *testing.T) []judgeGoldenCase {
	t.Helper()
	path := filepath.Join("testdata", "judge_golden.jsonl")
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		t.Fatalf("open golden cases: %v", err)
	}
	defer f.Close() //nolint:errcheck

	var cases []judgeGoldenCase
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var tc judgeGoldenCase
		if err := json.Unmarshal(scanner.Bytes(), &tc); err != nil {
			t.Fatalf("decode golden case line %d: %v", line, err)
		}
		if tc.Question == "" || tc.Gold == "" || tc.Predicted == "" || tc.Rule == "" || tc.Note == "" {
			t.Fatalf("golden case line %d has an empty required field: %#v", line, tc)
		}
		cases = append(cases, tc)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read golden cases: %v", err)
	}
	if len(cases) < 25 || len(cases) > 30 {
		t.Fatalf("golden case count = %d, want 25-30", len(cases))
	}
	return cases
}
