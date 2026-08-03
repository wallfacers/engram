package main

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/pipeline"
	"github.com/wallfacers/engram/store"
)

// sourceIDRE mirrors the extraction prompt's [source_id=<ulid>] marker so the
// mock caller can reference the batch Evidence id the pipeline injected.
var sourceIDRE = regexp.MustCompile(`source_id=([A-Za-z0-9]+)`)

// mockExtractCaller is a deterministic extraction sidecar: it emits one fact
// grounded in the first Evidence id of the batch, plus a second fact grounded
// in a later turn when the prompt contains a second source.
func mockExtractCaller(ctx context.Context, system, user string) (string, error) {
	ids := sourceIDRE.FindAllStringSubmatch(user, -1)
	if len(ids) == 0 {
		return `{"facts":[]}`, nil
	}
	first := ids[0][1]
	var b strings.Builder
	b.WriteString(`{"facts":[`)
	b.WriteString(`{"fact":"Dana maintains a home-lab project called homelab.","source_ids":["` + first + `"],"entities":["Dana"],"event_date":"2026-01-01"}`)
	if len(ids) > 1 {
		b.WriteString(`,`)
		b.WriteString(`{"fact":"Dana bought a used server in May 2026.","source_ids":["` + ids[1][1] + `"],"entities":["Dana"],"event_date":"2026-05-12"}`)
	}
	b.WriteString(`]}`)
	return b.String(), nil
}

func TestCategoryFromType(t *testing.T) {
	cases := map[string]string{
		"direct":    "single-hop",
		"time":      "temporal",
		"multi_hop": "multi-hop",
		"update":    "open-domain",
		"other":     "other",
	}
	for in, want := range cases {
		if got := categoryFromType(in); got != want {
			t.Fatalf("categoryFromType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSessionMessagesTurnIDs(t *testing.T) {
	s := sessionRec{SessionID: "s1", Date: "2026-01-15", Turns: []turnRec{
		{TurnID: "c0-0-0", Speaker: "user", Text: "hello"},
		{TurnID: "c0-0-1", Speaker: "assistant", Text: "hi"},
		{TurnID: "c0-0-2", Speaker: "user", Text: ""}, // empty dropped
	}}
	msgs := sessionMessages(s)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].ExternalSourceID != "c0-0-0" || msgs[0].Ordinal != 0 || msgs[0].Role != "user" {
		t.Fatalf("msg[0]: %#v", msgs[0])
	}
	if msgs[1].ExternalSourceID != "c0-0-1" || msgs[1].Ordinal != 1 || msgs[1].Role != "assistant" {
		t.Fatalf("msg[1]: %#v", msgs[1])
	}
	if msgs[0].OccurredAt == nil || msgs[0].OccurredAt.Format("2006-01-02") != "2026-01-15" {
		t.Fatalf("msg[0].OccurredAt = %v, want 2026-01-15", msgs[0].OccurredAt)
	}
}

func TestReadConvosSkipsNoQuery(t *testing.T) {
	lines := []string{
		`{"conversation_id":"c0","persona":"p","sessions":[],"queries":[{"question_id":"q0","query":"q?","type":"direct","gold_answer":"a","gold_source_turn_ids":["c0-0-0"]}]}`,
		`{"conversation_id":"c1","persona":"p","sessions":[]}`, // no queries → skipped
		`{"conversation_id":"c2","persona":"p","sessions":[],"queries":[]}`, // empty queries → skipped
	}
	path := t.TempDir() + "/convos.jsonl"
	if err := writeLines(path, lines); err != nil {
		t.Fatal(err)
	}
	convos, err := readConvos(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(convos) != 1 || convos[0].ConversationID != "c0" {
		t.Fatalf("got %d convos, want c0 only: %#v", len(convos), convos)
	}
}

func TestBuildCandidateLineIntegration(t *testing.T) {
	ctx := context.Background()
	conv := convoRecord{
		ConversationID: "c0",
		Persona:        "a software engineer named Dana",
		Sessions: []sessionRec{
			{SessionID: "c0-s0", Date: "2026-01-15", Turns: []turnRec{
				{TurnID: "c0-0-0", Speaker: "user", Text: "I started a home-lab project."},
				{TurnID: "c0-0-1", Speaker: "assistant", Text: "Great — I'll track the homelab project."},
			}},
			{SessionID: "c0-s1", Date: "2026-05-12", Turns: []turnRec{
				{TurnID: "c0-1-0", Speaker: "user", Text: "I bought a used server for the homelab."},
			}},
		},
		Queries: []queryRec{
			{
				QuestionID:        "q0",
				Query:             "What project does Dana maintain?",
				Type:              "direct",
				GoldAnswer:        "Dana maintains a home-lab project called homelab.",
				GoldSourceTurnIDs: []string{"c0-0-0", "c0-0-1"},
			},
			{
				QuestionID:        "q1",
				Query:             "When did Dana buy a server?",
				Type:              "time",
				GoldAnswer:        "May 2026.",
				GoldSourceTurnIDs: []string{"c0-1-0"},
			},
		},
	}

	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	es := memory.NewEntryStore(st.DB())
	vectors := memory.NewVectorStore(st.DB())
	embedder := memory.NewEmbedder(es, vectors, nil, memory.DefaultEmbedBuffer)
	pipe := pipeline.New(pipeline.Config{Entries: es, Embedder: embedder, Call: mockExtractCaller, Budgets: memory.DefaultBudgets()})
	for _, s := range conv.Sessions {
		date, _ := parseDate(s.Date)
		if _, err := pipe.Ingest(ctx, date, s.SessionID, sessionMessages(s)); err != nil {
			t.Fatalf("ingest %s: %v", s.SessionID, err)
		}
	}
	if err := embedder.Backfill(ctx); err != nil {
		t.Fatal(err)
	}

	queryDate := lastSessionDate(conv)
	if queryDate != "2026-05-12" {
		t.Fatalf("lastSessionDate = %q, want 2026-05-12", queryDate)
	}

	for qi, q := range conv.Queries {
		line, err := buildCandidateLine(ctx, es, vectors, conv, q, qi, queryDate, 10, "023-btest-r1")
		if err != nil {
			t.Fatalf("query %d: %v", qi, err)
		}
		if line.ID == "" || line.QueryDigest == "" || line.BuildVersion != "023-btest-r1" {
			t.Fatalf("line metadata: %#v", line)
		}
		if line.Category == "" {
			t.Fatalf("query %d: empty category", qi)
		}
		if len(line.Candidates) == 0 {
			t.Fatalf("query %d: no candidates retrieved", qi)
		}
		if len(line.GoldCoverage.GoldSourceEvidenceIDs) == 0 {
			t.Fatalf("query %d: gold evidence not resolved (turn→evidence mapping broken)", qi)
		}
		if !line.GoldCoverage.CandidateCovered {
			t.Fatalf("query %d: gold evidence not covered by frozen candidates (CoveredSourceCount=%d)", qi, line.GoldCoverage.CoveredSourceCount)
		}
		for _, c := range line.Candidates {
			if c.ID == "" || c.TextDigest == "" || len(c.SourceIDs) == 0 {
				t.Fatalf("query %d: malformed candidate: %#v", qi, c)
			}
			for _, sid := range c.SourceIDs {
				if _, ok := line.Sources[sid]; !ok {
					t.Fatalf("query %d: candidate source %q missing from lineage", qi, sid)
				}
			}
		}
		for _, gid := range line.GoldCoverage.GoldSourceEvidenceIDs {
			if _, ok := line.Sources[gid]; !ok {
				t.Fatalf("query %d: gold evidence %q missing from lineage", qi, gid)
			}
		}

		// Round-trip the output as JSON to confirm it stays a valid one-line record.
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("query %d: marshal: %v", qi, err)
		}
		if !strings.Contains(string(raw), "candidates") || !strings.Contains(string(raw), "gold_coverage") {
			t.Fatalf("query %d: output schema missing fields", qi)
		}
	}
}

func parseDate(s string) (t time.Time, err error) {
	return time.Parse("2006-01-02", s)
}

func writeLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			return err
		}
	}
	return nil
}
