package eventstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validRaw() string {
	return `{"conversation_id":"conv-0","source_ledger_ids":["ev1"],"speaker":"user",` +
		`"fact_entries":[{"text":"Caroline attended Pride parade on 2023-08-11","grounded":true}],` +
		`"relation_entries":[{"relation_type":"co_participation","subject":"Caroline","object":"Melanie",` +
		`"text":"Melanie is interested in Caroline's Pride experience"}],` +
		`"absolute_ts":"2023-08-17T00:00:00Z","relative_ref":"last year"}`
}

func TestValidateOK(t *testing.T) {
	ev, err := Validate([]byte(validRaw()))
	if err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	if ev.ConversationID != "conv-0" || len(ev.FactEntries) != 1 || len(ev.RelationEntries) != 1 {
		t.Fatalf("event fields not populated: %+v", ev)
	}
}

func TestValidateEmptyAndMalformed(t *testing.T) {
	if _, err := Validate(nil); err == nil {
		t.Fatal("nil output should fail closed")
	}
	if _, err := Validate([]byte("not json")); err == nil {
		t.Fatal("malformed JSON should fail closed")
	}
	if _, err := Validate([]byte(`{"conversation_id":123}`)); err == nil {
		t.Fatal("type-mismatched JSON should fail closed")
	}
}

func TestValidateRequiredFields(t *testing.T) {
	base := validRaw()
	cases := map[string]string{
		"empty conversation_id":  `{"conversation_id":"","source_ledger_ids":["e"],"speaker":"u","fact_entries":[{"text":"x"}]}`,
		"empty source_ledger_ids": `{"conversation_id":"c","source_ledger_ids":[],"speaker":"u","fact_entries":[{"text":"x"}]}`,
		"blank source entry":      `{"conversation_id":"c","source_ledger_ids":[""],"speaker":"u","fact_entries":[{"text":"x"}]}`,
		"empty speaker":           `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"","fact_entries":[{"text":"x"}]}`,
		"empty fact_entries":      `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"u","fact_entries":[]}`,
		"empty fact text":         `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"u","fact_entries":[{"text":""}]}`,
	}
	for name, raw := range cases {
		if _, err := Validate([]byte(raw)); err == nil {
			t.Errorf("%s: should fail closed", name)
		}
	}
	if _, err := Validate([]byte(base)); err != nil {
		t.Errorf("base case should pass, got %v", err)
	}
}

func TestValidateRelationEnum(t *testing.T) {
	bad := `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"u",` +
		`"fact_entries":[{"text":"x"}],"relation_entries":[{"relation_type":"banana",` +
		`"subject":"a","object":"b","text":"rel"}]}`
	if _, err := Validate([]byte(bad)); err == nil {
		t.Fatal("unknown relation_type should fail closed")
	}
	incomplete := `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"u",` +
		`"fact_entries":[{"text":"x"}],"relation_entries":[{"relation_type":"causal","subject":"","object":"b","text":"rel"}]}`
	if _, err := Validate([]byte(incomplete)); err == nil {
		t.Fatal("relation missing subject should fail closed")
	}
}

func TestValidateBudget(t *testing.T) {
	long := strings.Repeat("x", MaxTextRunes+1)
	overText := `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"u","fact_entries":[{"text":"` + long + `"}]}`
	if _, err := Validate([]byte(overText)); err == nil {
		t.Fatal("over-budget fact text should fail closed")
	}
	// payload over MaxTotalRunes via many facts
	var facts strings.Builder
	for i := 0; i < 5; i++ {
		facts.WriteString(`{"text":"` + strings.Repeat("y", 500) + `"},`)
	}
	overTotal := `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"u","fact_entries":[` +
		strings.TrimSuffix(facts.String(), ",") + `]}`
	if _, err := Validate([]byte(overTotal)); err == nil {
		t.Fatal("over-budget total payload should fail closed")
	}
}

func TestExtractOneOK(t *testing.T) {
	x := NewExtractor(func(_ context.Context, _, _ string) (string, error) {
		return validRaw(), nil
	})
	ev, err := x.ExtractOne(context.Background(), ExtractInput{
		ConversationID: "conv-0",
		SourceLedgerID: "ev1",
		Speaker:        "user",
		MessageText:    "I went to Pride last year with Melanie",
	})
	if err != nil {
		t.Fatalf("extract should succeed: %v", err)
	}
	if ev.ID == "" || !strings.HasPrefix(ev.ID, "ev-") {
		t.Fatalf("event id not derived: %+v", ev.ID)
	}
	if !ev.FactEntries[0].Grounded {
		t.Fatal("fact should be grounded after extraction")
	}
	st := x.Stats()
	if st.Attempts != 1 || st.Successes != 1 || st.Failures != 0 {
		t.Fatalf("unexpected stats: %+v", st)
	}
}

func TestExtractOneFailClosed(t *testing.T) {
	// model call error
	x := NewExtractor(func(_ context.Context, _, _ string) (string, error) {
		return "", errors.New("model down")
	})
	if _, err := x.ExtractOne(context.Background(), ExtractInput{MessageText: "hi"}); err == nil {
		t.Fatal("model error should fail closed")
	}
	// malformed output
	x2 := NewExtractor(func(_ context.Context, _, _ string) (string, error) {
		return "{\"conversation_id\":\"c\"", nil // truncated JSON
	})
	if _, err := x2.ExtractOne(context.Background(), ExtractInput{MessageText: "hi"}); err == nil {
		t.Fatal("malformed output should fail closed")
	}
	// schema violation (empty facts)
	x3 := NewExtractor(func(_ context.Context, _, _ string) (string, error) {
		return `{"conversation_id":"c","source_ledger_ids":["e"],"speaker":"u","fact_entries":[]}`, nil
	})
	if _, err := x3.ExtractOne(context.Background(), ExtractInput{MessageText: "hi"}); err == nil {
		t.Fatal("schema violation should fail closed")
	}
	// nil caller always fails closed
	nilX := NewExtractor(nil)
	if _, err := nilX.ExtractOne(context.Background(), ExtractInput{MessageText: "hi"}); err == nil {
		t.Fatal("nil caller should fail closed")
	}
}

func TestProjectRoundTrip(t *testing.T) {
	ev, err := Validate([]byte(validRaw()))
	if err != nil {
		t.Fatal(err)
	}
	ev.ID = "ev-1"
	p := BuildProject("conv-0", "hash-v1", []Event{*ev}, []RelationSummary{
		{SummaryID: "s1", EventIDs: []string{"ev-1"}, Text: "Caroline and Melanie discussed Pride plans"},
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "project.json")
	if err := p.Write(path); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := LoadProject(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ConfigHash != "hash-v1" || len(loaded.Events) != 1 || len(loaded.Summaries) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", loaded)
	}
}

func TestProjectRenderBounded(t *testing.T) {
	ev, _ := Validate([]byte(validRaw()))
	ev.ID = "ev-1"
	p := BuildProject("conv-0", "hash-v1", []Event{*ev}, nil)
	out := p.Render(50)
	if len([]rune(out)) > 50+len("…(truncated)") {
		t.Fatalf("render not bounded: %d runes", len([]rune(out)))
	}
	if !strings.Contains(out, "EVENT") && !strings.Contains(out, "…(truncated)") {
		t.Fatalf("render missing content: %q", out)
	}
}

func TestLoadMissingProject(t *testing.T) {
	_, err := LoadProject(filepath.Join(t.TempDir(), "nope.json"))
	if !os.IsNotExist(err) {
		t.Fatalf("missing project should be os.IsNotExist, got %v", err)
	}
}
