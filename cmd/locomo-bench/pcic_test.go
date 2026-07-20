package main

import (
	"context"
	"encoding/json"
	"reflect"
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

	singleOpt := optionsForRun(options{}, "hybrid+rerank", false)
	single := callRetrieveWithOptionalSelector(t, ctx, buildRetriever(singleOpt), "Alice", 3, 2)

	arms := []string{"hybrid+rerank", "hybrid+rerank+pcic"}
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
		t.Fatalf("baseline retrieval changed with PCIC sibling:\nalone:   %s\nsibling: %s", singleJSON, pairedJSON)
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
