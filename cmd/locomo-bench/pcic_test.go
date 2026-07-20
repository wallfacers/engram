package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/wallfacers/engram/embedding"
	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/store"
)

func TestPCICArmMechanismGatesSelector(t *testing.T) {
	baseline := optionsForArm(options{}, "hybrid+rerank")
	pcicArm := optionsForArm(options{}, "hybrid+rerank+pcic")

	if optionBool(t, baseline, "pcic") {
		t.Fatal("hybrid+rerank enabled PCIC selector")
	}
	if !optionBool(t, pcicArm, "pcic") {
		t.Fatal("hybrid+rerank+pcic did not enable PCIC selector")
	}
	if _, err := parseArm("hybrid+rerank+oracle"); err != nil {
		t.Fatalf("oracle arm was not recognized: %v", err)
	}

	pairedBaseline := optionsForRun(options{}, "hybrid+rerank", true)
	if optionBool(t, pairedBaseline, "pcic") {
		t.Fatal("paired hybrid+rerank baseline inherited PCIC selector")
	}
}

func optionBool(t *testing.T, opt options, field string) bool {
	t.Helper()
	v := reflect.ValueOf(opt).FieldByName(field)
	if !v.IsValid() {
		t.Fatalf("options.%s is missing", field)
	}
	if v.Kind() != reflect.Bool {
		t.Fatalf("options.%s has kind %s, want bool", field, v.Kind())
	}
	return v.Bool()
}

func TestPCICSiblingPreservesBaselineRetrievalBytes(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	es := memory.NewEntryStore(st.DB())
	vs := memory.NewVectorStore(st.DB())
	for _, entry := range []*memory.Entry{
		{Name: "chunk-one", Content: "Alice moved to Paris.", CharCount: 21},
		{Name: "chunk-two", Content: "Alice works at Acme.", CharCount: 20},
		{Name: "fact-one", Content: "Alice likes tea.", CharCount: 16},
	} {
		if err := es.Upsert(ctx, entry); err != nil {
			t.Fatalf("upsert %s: %v", entry.Name, err)
		}
	}

	buildRetriever := func(opt options) *memory.Retriever {
		r := memory.NewRetriever(es, vs, nil)
		if opt.rerank {
			r = r.WithReranker(parityReranker{})
		}
		return r
	}

	for _, baselineArm := range []string{"hybrid", "hybrid+rerank"} {
		t.Run(baselineArm, func(t *testing.T) {
			singleOpt := optionsForRun(options{}, baselineArm, false)
			single := callRetrieveWithOptionalSelector(t, ctx, buildRetriever(singleOpt), "Alice", 3, 2)

			arms := []string{baselineArm, "hybrid+rerank+pcic"}
			pairedOpt := optionsForRun(options{}, arms[0], len(arms) > 1)
			paired := callRetrieveWithOptionalSelector(t, ctx, buildRetriever(pairedOpt), "Alice", 3, 2)

			singleJSON, err := json.Marshal(single)
			if err != nil {
				t.Fatal(err)
			}
			pairedJSON, err := json.Marshal(paired)
			if err != nil {
				t.Fatal(err)
			}
			if string(singleJSON) != string(pairedJSON) {
				t.Fatalf("%s retrieval changed with PCIC sibling:\nalone:   %s\nsibling: %s", baselineArm, singleJSON, pairedJSON)
			}
		})
	}
}

func callRetrieveWithOptionalSelector(t *testing.T, ctx context.Context, r *memory.Retriever, query string, topK, quota int) []memory.Result {
	t.Helper()
	fn := reflect.ValueOf(retrieveWithQuotaDiagnostics)
	if fn.Type().NumIn() != 6 {
		t.Fatalf("retrieveWithQuotaDiagnostics has %d inputs, want optional selector as sixth input", fn.Type().NumIn())
	}
	results := fn.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(r),
		reflect.ValueOf(query),
		reflect.ValueOf(topK),
		reflect.ValueOf(quota),
		reflect.Zero(fn.Type().In(5)),
	})
	if !results[2].IsNil() {
		t.Fatalf("retrieve: %v", results[2].Interface())
	}
	return results[0].Interface().([]memory.Result)
}

type parityReranker struct{}

func (parityReranker) Model() string { return "parity" }

func (parityReranker) Rerank(_ context.Context, _ string, documents []string, topN int) ([]embedding.RankedDoc, error) {
	if topN <= 0 || topN > len(documents) {
		topN = len(documents)
	}
	out := make([]embedding.RankedDoc, 0, topN)
	for i := len(documents) - 1; i >= 0 && len(out) < topN; i-- {
		out = append(out, embedding.RankedDoc{Index: i, Score: float64(len(documents) - i)})
	}
	return out, nil
}

func TestPCICSelectLexicographicRoles(t *testing.T) {
	t.Run("anchors survive duplicate collapse", func(t *testing.T) {
		candidates := []memory.Result{
			pcicResult("chunk-a", 1.0, "anchor one"),
			pcicResult("chunk-b", 0.9, "anchor two"),
			pcicResult("chunk-c", 0.8, "filler"),
		}
		meta, turns := pcicFixture(map[string]SpanClaim{
			"D1:1": claim("D1:1", "alice", "job", "engineer", "current"),
			"D1:2": claim("D1:2", "alice", "job", "engineer", "current"),
		})
		turns["chunk-a"] = []string{"D1:1"}
		turns["chunk-b"] = []string{"D1:2"}
		got := pcicSelect(PCICSelectionInput{Candidates: candidates, Budget: 2, Meta: meta, ChunkTurns: turns})
		assertNames(t, got, "chunk-a", "chunk-b")
	})

	t.Run("duplicate collapses but state conflict survives", func(t *testing.T) {
		candidates := []memory.Result{
			pcicResult("chunk-anchor-a", 1.0, "anchor a"),
			pcicResult("chunk-anchor-b", 0.9, "anchor b"),
			pcicResult("chunk-duplicate", 0.8, "duplicate"),
			pcicResult("chunk-conflict", 0.7, "state conflict"),
			pcicResult("chunk-filler", 0.6, "filler"),
		}
		meta, turns := pcicFixture(map[string]SpanClaim{
			"D1:1": claim("D1:1", "alice", "location", "paris", "current"),
			"D1:2": claim("D1:2", "bob", "job", "designer", "current"),
			"D1:3": claim("D1:3", "alice", "location", "paris", "current"),
			"D1:4": claim("D1:4", "alice", "location", "london", "past"),
		})
		turns["chunk-anchor-a"] = []string{"D1:1"}
		turns["chunk-anchor-b"] = []string{"D1:2"}
		turns["chunk-duplicate"] = []string{"D1:3"}
		turns["chunk-conflict"] = []string{"D1:4"}
		got := pcicSelect(PCICSelectionInput{Candidates: candidates, Budget: 4, Meta: meta, ChunkTurns: turns})
		assertNames(t, got, "chunk-anchor-a", "chunk-anchor-b", "chunk-conflict", "chunk-filler")
	})

	t.Run("unmet complement beats higher rerank filler", func(t *testing.T) {
		candidates := []memory.Result{
			pcicResult("chunk-anchor-a", 1.0, "anchor a"),
			pcicResult("chunk-anchor-b", 0.9, "anchor b"),
			pcicResult("chunk-filler", 0.8, "unknown filler"),
			pcicResult("chunk-complement", 0.7, "Alice works at Acme"),
		}
		meta, turns := pcicFixture(map[string]SpanClaim{
			"D1:4": claim("D1:4", "alice", "job", "acme", "current"),
		})
		turns["chunk-complement"] = []string{"D1:4"}
		got := pcicSelect(PCICSelectionInput{
			Candidates:   candidates,
			Budget:       3,
			TokenCeiling: 8,
			Meta:         meta,
			ChunkTurns:   turns,
			DemandAtoms:  []DemandAtom{{Entity: "alice", Slot: "job"}},
		})
		assertNames(t, got, "chunk-anchor-a", "chunk-anchor-b", "chunk-complement")
	})

	t.Run("ambiguous role is not penalized as lure", func(t *testing.T) {
		candidates := []memory.Result{
			pcicResult("chunk-anchor-a", 1.0, "anchor a"),
			pcicResult("chunk-anchor-b", 0.9, "anchor b"),
			pcicResult("chunk-ambiguous", 0.8, "Alice said something"),
			pcicResult("chunk-lure", 0.7, "Alice likes chess"),
		}
		meta, turns := pcicFixture(map[string]SpanClaim{
			"D1:3": claim("D1:3", "alice", "", "unknown", ""),
			"D1:4": claim("D1:4", "alice", "hobby", "chess", "current"),
		})
		turns["chunk-ambiguous"] = []string{"D1:3"}
		turns["chunk-lure"] = []string{"D1:4"}
		got := pcicSelect(PCICSelectionInput{
			Candidates:  candidates,
			Budget:      3,
			Meta:        meta,
			ChunkTurns:  turns,
			DemandAtoms: []DemandAtom{{Entity: "alice", Slot: "job"}},
		})
		assertNames(t, got, "chunk-anchor-a", "chunk-anchor-b", "chunk-ambiguous")
	})
}

func TestPCICClassifyChunkRoles(t *testing.T) {
	selected := []memory.Result{pcicResult("chunk-selected", 1, "selected")}
	meta, turns := pcicFixture(map[string]SpanClaim{
		"D1:1": claim("D1:1", "alice", "job", "engineer", "current"),
		"D1:2": claim("D1:2", "alice", "job", "engineer", "current"),
		"D1:3": claim("D1:3", "alice", "job", "designer", "past"),
		"D1:4": claim("D1:4", "bob", "location", "paris", "current"),
		"D1:5": claim("D1:5", "alice", "hobby", "chess", "current"),
	})
	turns["chunk-selected"] = []string{"D1:1"}
	turns["chunk-duplicate"] = []string{"D1:2"}
	turns["chunk-conflict"] = []string{"D1:3"}
	turns["chunk-complement"] = []string{"D1:4"}
	turns["chunk-lure"] = []string{"D1:5"}
	input := PCICSelectionInput{Meta: meta, ChunkTurns: turns}
	atoms := []DemandAtom{{Entity: "bob", Slot: "location"}, {Entity: "alice", Slot: "job", Satisfied: true}}

	if role := classifyChunkRole(pcicResult("chunk-duplicate", 0.9, "duplicate"), 2, selected, input, atoms); !role.Duplicate || role.StateConflict {
		t.Fatalf("duplicate role = %+v", role)
	}
	if role := classifyChunkRole(pcicResult("chunk-conflict", 0.8, "conflict"), 3, selected, input, atoms); !role.StateConflict || role.Duplicate {
		t.Fatalf("state-conflict role = %+v", role)
	}
	if role := classifyChunkRole(pcicResult("chunk-complement", 0.7, "complement"), 4, selected, input, atoms); !role.Complement || role.Lure {
		t.Fatalf("complement role = %+v", role)
	}
	if role := classifyChunkRole(pcicResult("chunk-lure", 0.6, "lure"), 5, selected, input, atoms); !role.Lure || role.Complement {
		t.Fatalf("lure role = %+v", role)
	}
	if role := classifyChunkRole(pcicResult("chunk-unknown", 0.5, "unknown"), 6, selected, input, atoms); !role.Unknown || role.Lure {
		t.Fatalf("unknown role = %+v", role)
	}
	if role := classifyChunkRole(pcicResult("chunk-selected", 1, "anchor"), 0, nil, input, atoms); !role.Anchor {
		t.Fatalf("anchor role = %+v", role)
	}
}

func TestPCICSelectRespectsSlotAndTokenBudgets(t *testing.T) {
	candidates := make([]memory.Result, 0, 15)
	for i := 0; i < 15; i++ {
		candidates = append(candidates, pcicResult("chunk-"+string(rune('a'+i)), float64(15-i), "one two"))
	}
	got := pcicSelect(PCICSelectionInput{Candidates: candidates, Budget: 20, TokenCeiling: 6})
	if len(got) == 0 {
		t.Fatal("selector returned no chunks")
	}
	if len(got) > 12 {
		t.Fatalf("selected %d chunks, want at most 12", len(got))
	}
	tokens := 0
	for _, hit := range got {
		tokens += len(strings.Fields(hit.Content))
	}
	if tokens > 6 {
		t.Fatalf("selected token cost %d exceeds ceiling 6", tokens)
	}
}

func TestPCICSelectorDegradesToRerankOrder(t *testing.T) {
	candidates := []memory.Result{
		pcicResult("chunk-a", 1.0, "one"),
		pcicResult("chunk-b", 0.9, "two"),
		pcicResult("chunk-c", 0.8, "three"),
		pcicResult("chunk-d", 0.7, "four"),
	}
	meta := &PCICMeta{Header: PCICMetaHeader{Count: 0}, Spans: map[string]SpanClaim{}}
	for _, tc := range []struct {
		name     string
		meta     *PCICMeta
		reranked bool
	}{
		{name: "missing metadata", meta: nil, reranked: true},
		{name: "missing reranker", meta: meta, reranked: false},
		{name: "unannotated spans", meta: meta, reranked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &conversationRuntime{reranked: map[string]bool{"hybrid+rerank+pcic": tc.reranked}}
			selector, _ := selectorForArm(runtime, "hybrid+rerank+pcic", options{pcic: true, pcicMeta: tc.meta}, nil, false)
			got := selector(context.Background(), "Alice", candidates, 3)
			assertNames(t, got, "chunk-a", "chunk-b", "chunk-c")
		})
	}
}

func TestPCICDemandAtomsUsePublicEntitySignals(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, store.Options{DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	es := memory.NewEntryStore(st.DB())
	for _, item := range []struct {
		name, entity string
	}{
		{"chunk-alice", "Alice"},
		{"chunk-bob", "Bob"},
	} {
		if err := es.Upsert(ctx, &memory.Entry{Name: item.name, Content: item.entity, CharCount: len(item.entity)}); err != nil {
			t.Fatal(err)
		}
		if err := es.PutEntities(ctx, item.name, []string{item.entity}); err != nil {
			t.Fatal(err)
		}
	}

	signals, err := derivePCICSignals(ctx, es, "Where does Alice work?", []memory.Result{{Name: "chunk-alice"}, {Name: "chunk-bob"}})
	if err != nil {
		t.Fatalf("derivePCICSignals: %v", err)
	}
	if !reflect.DeepEqual(signals.DemandAtoms, []DemandAtom{{Entity: "alice"}}) {
		t.Fatalf("demand atoms = %#v, want alice from engine query/entity signals", signals.DemandAtoms)
	}
	if !reflect.DeepEqual(signals.CandidateEntities, map[string][]string{"chunk-alice": {"alice"}, "chunk-bob": {"bob"}}) {
		t.Fatalf("candidate entities = %#v", signals.CandidateEntities)
	}
}

func TestPCICOracleGreedyMaxCoverage(t *testing.T) {
	candidates := []memory.Result{
		pcicResult("chunk-two-turns", 1.0, "two turns"),
		pcicResult("chunk-overlap", 0.9, "overlap"),
		pcicResult("chunk-third", 0.8, "third"),
	}
	chunkTurns := map[string][]string{
		"chunk-two-turns": {"D1:1", "D1:2"},
		"chunk-overlap":   {"D1:2"},
		"chunk-third":     {"D2:1"},
	}
	got := pcicOracleSelect(candidates, 2, chunkTurns, []string{"D1:1", "D1:2", "D2:1"})
	assertNames(t, got, "chunk-two-turns", "chunk-third")
}

func pcicResult(name string, score float64, content string) memory.Result {
	return memory.Result{Name: name, Score: score, Content: content}
}

func claim(spanID, entity, slot, value, timeState string) SpanClaim {
	return SpanClaim{SpanID: spanID, Entity: entity, Slot: slot, Value: value, Polarity: PolarityAffirm, TimeState: timeState, SourceTurnIDs: []string{spanID}}
}

func pcicFixture(spans map[string]SpanClaim) (*PCICMeta, map[string][]string) {
	return &PCICMeta{Header: PCICMetaHeader{Count: len(spans)}, Spans: spans}, map[string][]string{}
}

func assertNames(t *testing.T, got []memory.Result, want ...string) {
	t.Helper()
	names := make([]string, len(got))
	for i := range got {
		names[i] = got[i].Name
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("selected names = %v, want %v", names, want)
	}
}
