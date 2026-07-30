package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
)

func TestEventProjectionArmsAreMutuallyExclusive(t *testing.T) {
	tests := []struct {
		name    string
		config  eventProjectionConfig
		want    eventProjectionArm
		wantErr bool
	}{
		{name: "disabled", want: eventProjectionArmNone},
		{name: "current fields", config: eventProjectionConfig{CurrentFields: true}, want: eventProjectionArmCurrentFields},
		{name: "event object", config: eventProjectionConfig{EventObject: true}, want: eventProjectionArmEventObject},
		{name: "date operator", config: eventProjectionConfig{DateOperator: true}, want: eventProjectionArmDateOperator},
		{name: "source recovery", config: eventProjectionConfig{SourceRecovery: true}, want: eventProjectionArmSourceRecovery},
		{name: "E0 plus E1", config: eventProjectionConfig{CurrentFields: true, EventObject: true}, wantErr: true},
		{name: "E1 plus E2", config: eventProjectionConfig{EventObject: true, DateOperator: true}, wantErr: true},
		{name: "E2 plus E3", config: eventProjectionConfig{DateOperator: true, SourceRecovery: true}, wantErr: true},
		{name: "all arms", config: eventProjectionConfig{CurrentFields: true, EventObject: true, DateOperator: true, SourceRecovery: true}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.config.arm()
			if test.wantErr {
				if err == nil {
					t.Fatalf("arm() = %q, nil error; want mutual-exclusion error", got)
				}
				if !strings.Contains(err.Error(), "mutually exclusive") {
					t.Fatalf("arm() error = %v, want mutually exclusive", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("arm() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("arm() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEventObjectShadowIsRunScopedRebuildableAndComplete(t *testing.T) {
	runDir := t.TempDir()
	productDB := filepath.Join(t.TempDir(), "product.db")
	if err := os.WriteFile(productDB, []byte("product-ledger-must-not-change"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(productDB)
	if err != nil {
		t.Fatal(err)
	}

	config := projectionRunConfig{RunDir: runDir, CandidateSetDigest: "sha256:frozen-candidates", TokenCap: 2048}
	view, err := buildEventObjectShadow(config, projectionTestCandidates())
	if err != nil {
		t.Fatalf("build event shadow: %v", err)
	}
	if got, want := view.Metadata.Arm, string(eventProjectionArmEventObject); got != want {
		t.Fatalf("arm = %q, want %q", got, want)
	}
	if got, want := view.Metadata.CandidateIDs, []string{"candidate-1", "candidate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate IDs = %v, want %v", got, want)
	}
	if got, want := view.Metadata.SourceIDs, []string{"source-1", "source-2", "source-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source IDs = %v, want complete lineage %v", got, want)
	}
	if len(view.Events) != 2 || !reflect.DeepEqual(view.Events[0].SourceIDs, []string{"source-1", "source-2"}) {
		t.Fatalf("event source lineage = %+v, want full candidate lineage", view.Events)
	}

	path := eventObjectShadowPath(runDir)
	rel, err := filepath.Rel(runDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("event shadow path %q escapes run dir %q", path, runDir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("event shadow was not written in run dir: %v", err)
	}
	after, err := os.ReadFile(productDB)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("shadow build mutated product DB: got %q, want %q", after, before)
	}

	if err := clearEventObjectShadow(runDir); err != nil {
		t.Fatalf("clear event shadow: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("event shadow exists after clear: %v", err)
	}
	rebuilt, err := buildEventObjectShadow(config, projectionTestCandidates())
	if err != nil {
		t.Fatalf("rebuild event shadow: %v", err)
	}
	if !reflect.DeepEqual(rebuilt, view) {
		t.Fatalf("rebuild is not deterministic:\n got %+v\nwant %+v", rebuilt, view)
	}
}

func TestDateOperatorShadowPreservesFrozenCandidateSet(t *testing.T) {
	runDir := t.TempDir()
	productDB, before := projectionProductDBSentinel(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	config := projectionRunConfig{RunDir: runDir, CandidateSetDigest: "sha256:same-candidates", TokenCap: 1024}
	view, err := applyDateOperatorShadow(config, dateOperator{Kind: dateOperatorBetween, Start: &start, End: &end}, projectionTestCandidates())
	if err != nil {
		t.Fatalf("apply date operator: %v", err)
	}
	if got, want := view.Metadata.Arm, string(eventProjectionArmDateOperator); got != want {
		t.Fatalf("arm = %q, want %q", got, want)
	}
	if got, want := view.Metadata.CandidateIDs, []string{"candidate-1", "candidate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("date operator changed candidate set: got %v want %v", got, want)
	}
	if len(view.Candidates) != 2 || !reflect.DeepEqual(view.Candidates[1].SourceIDs, []string{"source-3"}) {
		t.Fatalf("date operator lost source lineage: %+v", view.Candidates)
	}
	if _, err := os.Stat(dateOperatorShadowPath(runDir)); err != nil {
		t.Fatalf("date operator shadow was not written: %v", err)
	}
	if err := clearDateOperatorShadow(runDir); err != nil {
		t.Fatalf("clear date operator shadow: %v", err)
	}
	if _, err := os.Stat(dateOperatorShadowPath(runDir)); !os.IsNotExist(err) {
		t.Fatalf("date operator shadow exists after clear: %v", err)
	}
	assertProjectionProductDBUnchanged(t, productDB, before)
}

func TestDateOperatorShadowIsDeterministicAcrossSupportedOperators(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	tests := []struct {
		name        string
		operator    dateOperator
		wantMatches []bool
	}{
		{name: "before", operator: dateOperator{Kind: dateOperatorBefore, End: &end}, wantMatches: []bool{true, false}},
		{name: "after", operator: dateOperator{Kind: dateOperatorAfter, Start: &start}, wantMatches: []bool{false, true}},
		{name: "latest", operator: dateOperator{Kind: dateOperatorLatest}, wantMatches: []bool{false, true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view, err := applyDateOperatorShadow(projectionRunConfig{
				RunDir:             t.TempDir(),
				CandidateSetDigest: "sha256:date-operator-" + test.name,
				TokenCap:           1024,
			}, test.operator, projectionTestCandidates())
			if err != nil {
				t.Fatalf("apply %s: %v", test.name, err)
			}
			for index, want := range test.wantMatches {
				if got := view.Candidates[index].Matches; got != want {
					t.Fatalf("candidate %q match = %t, want %t", view.Candidates[index].CandidateID, got, want)
				}
			}
			if got, want := view.Metadata.CandidateIDs, []string{"candidate-1", "candidate-2"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("%s changed candidate set: got %v want %v", test.name, got, want)
			}
		})
	}
	if err := validateDateOperator(dateOperator{Kind: "not-an-operator"}); err == nil {
		t.Fatal("unknown date operator was accepted")
	}
}

func TestSourceRecoveryShadowOnlyResolvesCandidateLineage(t *testing.T) {
	runDir := t.TempDir()
	productDB, before := projectionProductDBSentinel(t)
	config := projectionRunConfig{RunDir: runDir, CandidateSetDigest: "sha256:source-recovery", TokenCap: 1024}
	var requested []string
	reader := sourceRecoveryReaderFunc(func(_ context.Context, ids []string) (map[string]memory.Evidence, error) {
		requested = append([]string(nil), ids...)
		return map[string]memory.Evidence{
			"source-1": {ID: "source-1", Content: "first", State: memory.EvidenceActive},
			"source-2": {ID: "source-2", Content: "second", State: memory.EvidenceActive},
			"source-3": {ID: "source-3", Content: "third", State: memory.EvidenceActive},
		}, nil
	})
	view, err := buildSourceRecoveryShadow(context.Background(), config, reader, projectionTestCandidates())
	if err != nil {
		t.Fatalf("build source recovery shadow: %v", err)
	}
	if got, want := requested, []string{"source-1", "source-2", "source-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolver IDs = %v, want only frozen candidate lineage %v", got, want)
	}
	if got, want := view.Metadata.SourceIDs, []string{"source-1", "source-2", "source-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recovered sources = %v, want %v", got, want)
	}
	if _, err := os.Stat(sourceRecoveryShadowPath(runDir)); err != nil {
		t.Fatalf("source recovery shadow was not written: %v", err)
	}
	if err := clearSourceRecoveryShadow(runDir); err != nil {
		t.Fatalf("clear source recovery shadow: %v", err)
	}

	badReader := sourceRecoveryReaderFunc(func(_ context.Context, _ []string) (map[string]memory.Evidence, error) {
		return map[string]memory.Evidence{
			"source-1": {ID: "source-1", State: memory.EvidenceActive},
			"source-2": {ID: "source-2", State: memory.EvidenceActive},
			"source-3": {ID: "source-3", State: memory.EvidenceActive},
			"outside":  {ID: "outside", State: memory.EvidenceActive},
		}, nil
	})
	if _, err := buildSourceRecoveryShadow(context.Background(), config, badReader, projectionTestCandidates()); err == nil || !strings.Contains(err.Error(), "outside frozen lineage") {
		t.Fatalf("source recovery accepted an outside source: %v", err)
	}
	assertProjectionProductDBUnchanged(t, productDB, before)
}

type sourceRecoveryReaderFunc func(context.Context, []string) (map[string]memory.Evidence, error)

func (reader sourceRecoveryReaderFunc) GetMany(ctx context.Context, ids []string) (map[string]memory.Evidence, error) {
	return reader(ctx, ids)
}

func projectionTestCandidates() []evidencecompiler.Candidate {
	return []evidencecompiler.Candidate{
		{
			ID:         "candidate-1",
			Kind:       evidencecompiler.CandidateRawTurn,
			Rank:       1,
			Text:       "Alice changed her preference on 2026-07-12.",
			TextDigest: evalTextDigest("Alice changed her preference on 2026-07-12."),
			SourceIDs:  []string{"source-1", "source-2"},
			Metadata: map[string]string{
				"event_time":        "2026-07-12T09:00:00Z",
				"source_session_id": "session-a",
				"scene_key":         "conference",
				"profile_subject":   "alice",
				"profile_kind":      "preference",
			},
		},
		{
			ID:         "candidate-2",
			Kind:       evidencecompiler.CandidateRawTurn,
			Rank:       2,
			Text:       "Alice is currently in Shanghai.",
			TextDigest: evalTextDigest("Alice is currently in Shanghai."),
			SourceIDs:  []string{"source-3"},
			Metadata: map[string]string{
				"event_time":        "2026-08-01T09:00:00Z",
				"source_session_id": "session-b",
				"scene_key":         "conference",
				"profile_subject":   "alice",
				"profile_kind":      "current_state",
			},
		},
	}
}

func projectionProductDBSentinel(t *testing.T) (string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "product.db")
	contents := []byte("product-ledger-must-not-change")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, contents
}

func assertProjectionProductDBUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shadow operation mutated product DB: got %q want %q", got, want)
	}
}
