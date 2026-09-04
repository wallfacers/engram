package main

// T046 tests: the closed v2 failure-class set and the deterministic rejudge
// entries the official scorer relies on. Every fixture here is fictional.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func rjWriteCase() *TriggerCaseV2 {
	return &TriggerCaseV2{ID: "rj-write-1", Module: "implicit-write-pos",
		Expect: ExpectV2{Trigger: true, StoreInclude: []Alternation{{"pnpm"}}, Observable: "o"}}
}

func rjWritePassArtifacts() ([]byte, []byte) {
	events := []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "已记住 pnpm 配置"},
	}
	b, err := json.Marshal(events)
	if err != nil {
		panic(err)
	}
	return b, []byte("pnpm\n")
}

func TestRejudgeClosedFailureClassSet(t *testing.T) {
	// The four semantic classes plus the terminal runner code are the whole
	// v2 set; the legacy v1 `failed` code is deliberately outside it.
	for _, class := range []string{
		FailureFalseNegative, FailureFalsePositive, FailureWrongOp, FailureWrongReport, FailureRunnerError,
	} {
		v := &Verdict{CaseID: "c", Failure: class}
		if err := ValidateVerdict(v); err != nil {
			t.Fatalf("class %q must be inside the closed v2 set: %v", class, err)
		}
	}
	if !IsTerminalRunnerClass(FailureRunnerError) || IsTerminalRunnerClass(FailureWrongOp) {
		t.Fatal("only runner-error is the terminal runner class")
	}
	// An unknown class, the legacy v1 code, and a classless non-pass all fail
	// closed instead of defaulting to a pass.
	for _, class := range []string{"failed", "unknown", ""} {
		if err := ValidateVerdict(&Verdict{CaseID: "c", Failure: class}); err == nil {
			t.Fatalf("failure class %q must be rejected", class)
		}
	}
	if err := ValidateVerdict(&Verdict{CaseID: "c", Pass: true, Failure: FailureWrongReport}); err == nil {
		t.Fatal("a passing verdict carrying a failure class must be rejected")
	}
	if err := ValidateVerdict(&Verdict{CaseID: "", Pass: true}); err == nil {
		t.Fatal("a verdict without a case id must be rejected")
	}
	if err := ValidateVerdict(&Verdict{CaseID: "c", Pass: true}); err != nil {
		t.Fatalf("a clean pass must validate: %v", err)
	}
	if err := ValidateVerdict(nil); err == nil {
		t.Fatal("a nil verdict must be rejected")
	}
}

func TestRejudgeFromArtifactsDeterministic(t *testing.T) {
	c := rjWriteCase()
	events, dump := rjWritePassArtifacts()
	want, err := RejudgeFromArtifacts(events, dump, c)
	if err != nil {
		t.Fatalf("rejudge: %v", err)
	}
	if !want.Pass || want.Failure != "" {
		t.Fatalf("a complete write case must rejudge to a pass: %+v", want)
	}
	// Byte-identical artifacts, byte-identical verdict — that is the whole
	// reason a receipt can be verified after the run.
	for i := 0; i < 3; i++ {
		got, err := RejudgeFromArtifacts(events, dump, c)
		if err != nil {
			t.Fatalf("rejudge %d: %v", i, err)
		}
		if got != want {
			t.Fatalf("rejudge %d is not deterministic: %+v vs %+v", i, got, want)
		}
	}
	// The four semantic classes are reachable from artifacts alone, so a
	// tampered receipt cannot survive a rejudge.
	missingWrite, err := RejudgeFromArtifacts([]byte("[]"), dump, c)
	if err != nil || missingWrite.Pass || missingWrite.Failure != FailureFalseNegative {
		t.Fatalf("no write call must rejudge to false-negative: %+v err=%v", missingWrite, err)
	}
	rogueStore, err := RejudgeFromArtifacts(events, []byte("rogue\n"), c)
	if err != nil || rogueStore.Pass || rogueStore.Failure != FailureWrongOp {
		t.Fatalf("a mutated store must rejudge to wrong-op: %+v err=%v", rogueStore, err)
	}
	noAck := append([]Event{}, []Event{
		{Kind: EventEngramCall, Op: "write", Via: "mcp"},
		{Kind: EventText, Text: "处理好了"},
	}...)
	nb, err := json.Marshal(noAck)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := RejudgeFromArtifacts(nb, dump, c); err != nil || v.Pass || v.Failure != FailureWrongReport {
		t.Fatalf("a silent write must rejudge to wrong-report: %+v err=%v", v, err)
	}
	if v, err := RejudgeFromArtifacts([]byte("[]"), []byte(""), &TriggerCaseV2{
		ID: "rj-neg", Module: "implicit-write-neg",
		Expect: ExpectV2{Trigger: false, StoreExclude: []string{"token"}},
	}); err != nil || !v.Pass {
		t.Fatalf("a quiet negative case must pass: %+v err=%v", v, err)
	}
}

func TestRejudgeTraceUnobservableFailsClosed(t *testing.T) {
	// Answer-only, empty and store-less evidence are classified non-passes —
	// never promoted to a pass by whatever the answer text claims.
	read := &TriggerCaseV2{ID: "rj-read", Module: "implicit-read-pos",
		Expect: ExpectV2{Trigger: true, AnswerInclude: []Alternation{{"记得"}}, Observable: "o"}}
	answerOnly, err := json.Marshal([]Event{{Kind: EventText, Text: "我记得"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		events  []byte
		dump    []byte
		c       *TriggerCaseV2
		failure string
	}{
		"answer-only":  {events: answerOnly, c: read, failure: FailureFalseNegative},
		"empty-stream": {events: []byte("[]"), c: read, failure: FailureFalseNegative},
		"no-store-evidence": {events: mustJSON(t, []Event{{Kind: EventEngramCall, Op: "write", Via: "mcp"}}),
			c:    rjWriteCase(),
			dump: nil, failure: FailureWrongOp},
	} {
		v, err := RejudgeFromArtifacts(tc.events, tc.dump, tc.c)
		if err != nil {
			t.Fatalf("%s: rejudge: %v", name, err)
		}
		if v.Pass || v.Failure != tc.failure {
			t.Fatalf("%s: unobservable trace must be a %s, got %+v", name, tc.failure, v)
		}
	}
	// Unparseable artifacts fail closed with an error, not with a verdict.
	if _, err := RejudgeFromArtifacts([]byte("{not json"), nil, read); err == nil {
		t.Fatal("a malformed normalized-events artifact must fail closed")
	}
	if _, err := RejudgeFromArtifacts(nil, nil, nil); err == nil {
		t.Fatal("a rejudge without a frozen case spec must fail closed")
	}
}

func TestRejudgeRunnerErrorIsTerminal(t *testing.T) {
	c := rjWriteCase()
	events, dump := rjWritePassArtifacts()
	// Even a fully passing artifact stream cannot rescue a child that died:
	// the recorded terminal status stays terminal.
	v, err := RejudgeFromRecordedCase(CaseStatusRunnerError, events, dump, c)
	if err != nil {
		t.Fatalf("rejudge: %v", err)
	}
	if v.Pass || !IsTerminalRunnerClass(v.Failure) {
		t.Fatalf("a recorded runner-error must stay terminal, got %+v", v)
	}
	if !strings.Contains(v.Detail, ErrTerminalRunner.Error()) {
		t.Fatalf("the terminal verdict should name the terminal condition, got %q", v.Detail)
	}
	// An out-of-set terminal status fails closed.
	if _, err := RejudgeFromRecordedCase("retrying", events, dump, c); err == nil {
		t.Fatal("an unknown terminal status must be rejected")
	}
	// And the rejudged terminal verdict agrees with what a runner recorded,
	// so the score-side comparison is exact.
	if err := verdictAgrees(c.ID, v, v); err != nil {
		t.Fatalf("identical verdicts must agree: %v", err)
	}
	if err := verdictAgrees(c.ID, Verdict{CaseID: c.ID, Pass: true}, v); err == nil {
		t.Fatal("a recorded pass must disagree with a terminal rejudge")
	}
	if err := verdictAgrees(c.ID, Verdict{CaseID: "other"}, v); err == nil {
		t.Fatal("a receipt verdict naming another case must disagree")
	}
	if !errors.Is(ErrTerminalRunner, ErrTerminalRunner) {
		t.Fatal("the terminal sentinel must be usable for errors.Is")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
