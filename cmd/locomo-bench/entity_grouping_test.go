package main

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func multiHopIDs(hits []memory.Result) []string {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.Name)
	}
	return ids
}

func resultMultiset(hits []memory.Result) []string {
	items := make([]string, 0, len(hits))
	for _, h := range hits {
		items = append(items, h.Name+"\x00"+h.Content)
	}
	sort.Strings(items)
	return items
}

func TestMultiHopCanonicalOrder(t *testing.T) {
	hits := []memory.Result{
		hit("fact-alice", "Alice Smith derived fact", 99),
		hit("chunk-bob", "Bob Jones raw chunk", 3),
		hit("fact-bob", "Bob Jones derived fact", 98),
		hit("chunk-alice", "Alice Smith raw chunk", 4),
		hit("fact-z", "plain derived fact", 97),
		hit("chunk-z", "plain raw chunk", 2),
	}

	got := groupHitsByEntity(hits)
	want := []string{
		"chunk-alice", "chunk-bob", "chunk-z",
		"fact-alice", "fact-bob", "fact-z",
	}
	if ids := multiHopIDs(got); !reflect.DeepEqual(ids, want) {
		t.Fatalf("canonical IDs = %v, want %v", ids, want)
	}
	if !reflect.DeepEqual(resultMultiset(got), resultMultiset(hits)) {
		t.Fatalf("canonical ordering changed candidate multiset")
	}
}

func TestMultiHopStableTieBreak(t *testing.T) {
	a := hit("chunk-a", "Alice Smith raw one", 7)
	b := hit("chunk-b", "Alice Smith raw two", 7)
	c := hit("fact-a", "Alice Smith derived", 7)

	got1 := multiHopIDs(groupHitsByEntity([]memory.Result{b, c, a}))
	got2 := multiHopIDs(groupHitsByEntity([]memory.Result{c, a, b}))
	want := []string{"chunk-a", "chunk-b", "fact-a"}
	if !reflect.DeepEqual(got1, want) || !reflect.DeepEqual(got2, want) {
		t.Fatalf("tie-break outputs = %v and %v, want %v", got1, got2, want)
	}
}

func TestMultiHopDegenerateInputs(t *testing.T) {
	tests := []struct {
		name string
		hits []memory.Result
		want []string
	}{
		{name: "empty", want: []string{}},
		{name: "chunks only", hits: []memory.Result{
			hit("chunk-b", "plain b", 1), hit("chunk-a", "plain a", 2),
		}, want: []string{"chunk-a", "chunk-b"}},
		{name: "facts only", hits: []memory.Result{
			hit("fact-b", "plain b", 1), hit("fact-a", "plain a", 2),
		}, want: []string{"fact-a", "fact-b"}},
		{name: "all ungrouped", hits: []memory.Result{
			hit("fact-a", "plain fact", 9), hit("chunk-a", "plain chunk", 1),
		}, want: []string{"chunk-a", "fact-a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupHitsByEntity(tt.hits)
			if ids := multiHopIDs(got); !reflect.DeepEqual(ids, tt.want) {
				t.Fatalf("IDs = %v, want %v", ids, tt.want)
			}
			if !reflect.DeepEqual(resultMultiset(got), resultMultiset(tt.hits)) {
				t.Fatal("candidate multiset changed")
			}
		})
	}
}

func TestMultiHopPrivateLabelsDoNotAffectAssembly(t *testing.T) {
	answerA, _ := json.Marshal("private answer A")
	answerB, _ := json.Marshal("private answer B")
	qaA := locomoQA{
		Question: "What connects Alice and Bob?", Answer: answerA,
		Evidence: []string{"D1:1"}, Category: assemblyCategoryMultiHop, QuestionID: "q1",
	}
	qaB := qaA
	qaB.Answer = answerB
	qaB.Evidence = []string{"D99:99"}
	hits := []memory.Result{
		hit("fact-a", "Alice Smith derived fact", 9),
		hit("chunk-a", "Alice Smith raw chunk", 1),
	}
	assemble := func(qa locomoQA) (EvidenceAssembly, string) {
		cfg := testAssemblyConfig()
		cfg.QuestionID = qa.QuestionID
		asm, prompt, err := assembleEvidence(
			t.Context(), qa.Question, hits, qa.Category, cfg, nil,
		)
		if err != nil {
			t.Fatalf("assembleEvidence: %v", err)
		}
		return asm, prompt
	}
	asmA, promptA := assemble(qaA)
	asmB, promptB := assemble(qaB)
	if !reflect.DeepEqual(asmA, asmB) || promptA != promptB {
		t.Fatal("private answer/evidence labels changed assembly output")
	}
}
