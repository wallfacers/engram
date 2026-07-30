package evidencecompiler

import (
	"strings"
	"testing"
	"time"
)

func TestBuildNeedCapturesExplicitQueryConstraintsDeterministically(t *testing.T) {
	query := "List 3 cities Alice visited after 2024-01-01 and before 2024-12-31; what changed in her current plan?"
	need := BuildNeed(query)

	if !containsString(need.Entities, "Alice") {
		t.Fatalf("BuildNeed().Entities = %v, want Alice", need.Entities)
	}
	if !containsString(need.TimeConstraints, "after") || !containsString(need.TimeConstraints, "before") || !containsString(need.TimeConstraints, "2024-01-01") {
		t.Fatalf("BuildNeed().TimeConstraints = %v, want explicit before/after/date constraints", need.TimeConstraints)
	}
	if !need.ListCardinality.Known || need.ListCardinality.Count != 3 {
		t.Fatalf("BuildNeed().ListCardinality = %+v, want known(3)", need.ListCardinality)
	}
	if !strings.Contains(need.UpdateState, "current") || !strings.Contains(need.UpdateState, "change") {
		t.Fatalf("BuildNeed().UpdateState = %q, want current and change", need.UpdateState)
	}
	if len(need.Operands) < 2 {
		t.Fatalf("BuildNeed().Operands = %+v, want one operand per explicit question clause", need.Operands)
	}

	again := BuildNeed(query)
	if !equalNeed(need, again) {
		t.Fatalf("BuildNeed() was not deterministic:\nfirst=%+v\nnext=%+v", need, again)
	}
}

func TestMergePlannerNeedCannotDeleteExplicitConstraintsOrInventCardinality(t *testing.T) {
	base := BuildNeed("What did Alice do after 2024-01-01?")
	proposal := base
	proposal.Entities = nil
	if _, err := mergePlannerNeed(base, proposal); err == nil {
		t.Fatal("mergePlannerNeed() accepted a proposal that removed Alice")
	}

	proposal = base
	proposal.ListCardinality = Cardinality{Known: true, Count: 2}
	if _, err := mergePlannerNeed(base, proposal); err == nil {
		t.Fatal("mergePlannerNeed() accepted an invented cardinality from an unknown base")
	}

	proposal = base
	proposal.Entities = append(proposal.Entities, "Bob")
	merged, err := mergePlannerNeed(base, proposal)
	if err != nil {
		t.Fatalf("mergePlannerNeed(additive proposal) error = %v", err)
	}
	if !containsString(merged.Entities, "Alice") || !containsString(merged.Entities, "Bob") {
		t.Fatalf("merged entities = %v, want preserved Alice plus Bob", merged.Entities)
	}
}

func TestBuildRelationsOnlyUsesResolvedSourceEvidence(t *testing.T) {
	earlier := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.Add(24 * time.Hour)
	sources := map[string]Source{
		"src-a": {
			ID:            "src-a",
			Content:       "Alice selected the red plan.",
			ContentDigest: sha256Hex("Alice selected the red plan."),
			OccurredAt:    &earlier,
		},
		"src-b": {
			ID:            "src-b",
			Content:       "Alice did not select the red plan.",
			ContentDigest: sha256Hex("Alice did not select the red plan."),
			OccurredAt:    &later,
		},
	}
	relations := buildRelations(EvidenceNeed{Operands: []Operand{{Name: "plan"}}}, sources)
	if len(relations) == 0 {
		t.Fatal("buildRelations() returned no evidence-grounded relation")
	}

	var before, conflict bool
	for _, relation := range relations {
		if relation.LeftSourceID == "" || relation.RightSourceID == "" || sources[relation.LeftSourceID].ID == "" || sources[relation.RightSourceID].ID == "" {
			t.Fatalf("relation %+v is not grounded in resolved sources", relation)
		}
		before = before || relation.Kind == RelationBefore
		conflict = conflict || relation.Kind == RelationConflicts
	}
	if !before || !conflict {
		t.Fatalf("relations = %+v, want grounded before and conflict relations", relations)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
