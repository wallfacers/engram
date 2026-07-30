package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wallfacers/engram/memory"
	"github.com/wallfacers/engram/memory/evidencecompiler"
	"github.com/wallfacers/engram/store"
)

func TestNarrowProjectionArmsAreIndependent(t *testing.T) {
	tests := []struct {
		name    string
		config  narrowProjectionConfig
		want    narrowProjectionArm
		wantErr bool
	}{
		{name: "disabled", want: narrowProjectionArmNone},
		{name: "scene", config: narrowProjectionConfig{Scene: true}, want: narrowProjectionArmScene},
		{name: "profile", config: narrowProjectionConfig{Profile: true}, want: narrowProjectionArmProfile},
		{name: "graph", config: narrowProjectionConfig{Graph: true}, want: narrowProjectionArmGraph},
		{name: "scene plus profile", config: narrowProjectionConfig{Scene: true, Profile: true}, wantErr: true},
		{name: "profile plus graph", config: narrowProjectionConfig{Profile: true, Graph: true}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.config.arm()
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
					t.Fatalf("arm() = %q, %v; want mutual-exclusion error", got, err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("arm() = %q, %v; want %q, nil", got, err, test.want)
			}
		})
	}
}

func TestSceneShadowOnlyExpandsCrossSessionCandidatesAndIsClearable(t *testing.T) {
	runDir := t.TempDir()
	productDB := filepath.Join(t.TempDir(), "product.db")
	if err := os.WriteFile(productDB, []byte("unchanged-product-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(productDB)
	if err != nil {
		t.Fatal(err)
	}
	candidates := append(projectionTestCandidates(), evidencecompiler.Candidate{
		ID:         "candidate-same-session",
		Kind:       evidencecompiler.CandidateRawTurn,
		Rank:       3,
		Text:       "A different one-session scene",
		TextDigest: evalTextDigest("A different one-session scene"),
		SourceIDs:  []string{"source-same-session"},
		Metadata: map[string]string{
			"scene_key":         "single-session-only",
			"source_session_id": "session-a",
		},
	})
	config := projectionRunConfig{RunDir: runDir, CandidateSetDigest: "sha256:scene-frozen", TokenCap: 2048, CandidateLimit: 3}
	view, err := buildSceneShadow(config, candidates)
	if err != nil {
		t.Fatalf("build scene shadow: %v", err)
	}
	if len(view.Scenes) != 1 {
		t.Fatalf("scene count = %d, want exactly cross-session scene: %+v", len(view.Scenes), view)
	}
	if got, want := view.Scenes[0].CandidateIDs, []string{"candidate-1", "candidate-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scene candidates = %v, want %v", got, want)
	}
	if got, want := view.Scenes[0].SourceIDs, []string{"source-1", "source-2", "source-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scene sources = %v, want full lineage %v", got, want)
	}
	if got, want := view.Scenes[0].SessionIDs, []string{"session-a", "session-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scene sessions = %v, want %v", got, want)
	}
	if _, err := os.Stat(sceneShadowPath(runDir)); err != nil {
		t.Fatalf("scene shadow was not written: %v", err)
	}
	if err := clearSceneShadow(runDir); err != nil {
		t.Fatalf("clear scene shadow: %v", err)
	}
	if _, err := os.Stat(sceneShadowPath(runDir)); !os.IsNotExist(err) {
		t.Fatalf("scene shadow exists after clear: %v", err)
	}
	after, err := os.ReadFile(productDB)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("scene shadow mutated product DB: got %q want %q", after, before)
	}
}

func TestProfileShadowIsLimitedToPreferenceAndCurrentState(t *testing.T) {
	runDir := t.TempDir()
	productDB, before := projectionProductDBSentinel(t)
	config := projectionRunConfig{RunDir: runDir, CandidateSetDigest: "sha256:profile-frozen", TokenCap: 1024, CandidateLimit: 2}
	candidates := append(projectionTestCandidates(), evidencecompiler.Candidate{
		ID:         "candidate-unrelated",
		Kind:       evidencecompiler.CandidateRawTurn,
		Rank:       3,
		Text:       "Alice visited a museum.",
		TextDigest: evalTextDigest("Alice visited a museum."),
		SourceIDs:  []string{"source-unrelated"},
		Metadata: map[string]string{
			"profile_subject": "alice",
			"profile_kind":    "biography",
		},
	})
	view, err := buildProfileShadow(config, candidates)
	if err != nil {
		t.Fatalf("build profile shadow: %v", err)
	}
	if got, want := view.Metadata.SourceIDs, []string{"source-1", "source-2", "source-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("profile shadow included unrelated lineage: got %v want %v", got, want)
	}
	if len(view.Profiles) != 1 {
		t.Fatalf("profile count = %d, want 1: %+v", len(view.Profiles), view)
	}
	profile := view.Profiles[0]
	if profile.Subject != "alice" {
		t.Fatalf("profile subject = %q, want alice", profile.Subject)
	}
	if got, want := profile.PreferenceCandidateIDs, []string{"candidate-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preference candidates = %v, want %v", got, want)
	}
	if got, want := profile.CurrentStateCandidateID, "candidate-2"; got != want {
		t.Fatalf("current-state candidate = %q, want %q", got, want)
	}
	if got, want := profile.SourceIDs, []string{"source-1", "source-2", "source-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("profile sources = %v, want %v", got, want)
	}
	if err := clearProfileShadow(runDir); err != nil {
		t.Fatalf("clear profile shadow: %v", err)
	}
	if _, err := os.Stat(profileShadowPath(runDir)); !os.IsNotExist(err) {
		t.Fatalf("profile shadow exists after clear: %v", err)
	}
	assertProjectionProductDBUnchanged(t, productDB, before)
}

func TestGraphProjectionUses003PublicReadAPIWithoutMutatingGraphData(t *testing.T) {
	var _ graphProjectionReader = (*memory.EntryStore)(nil)
	runDir := t.TempDir()
	productDB, before := projectionProductDBSentinel(t)
	graph := &fakeGraphProjectionReader{
		cues:    []string{"alice"},
		entries: []string{"candidate-gap", "unknown-entry"},
	}
	config := projectionRunConfig{RunDir: runDir, CandidateSetDigest: "sha256:graph-frozen", TokenCap: 1024, CandidateLimit: 2}
	seed := projectionTestCandidates()[:1]
	catalog := append(append([]evidencecompiler.Candidate(nil), seed...), gapSupplementCandidate())
	view, err := buildGraphProjectionShadow(context.Background(), graph, graphProjectionRequest{
		Config:              config,
		Query:               "What links Alice and Avery?",
		SeedCandidates:      seed,
		CandidateCatalog:    catalog,
		MaxBridgeCandidates: 1,
	})
	if err != nil {
		t.Fatalf("build graph projection: %v", err)
	}
	if got, want := graph.queries, []string{"What links Alice and Avery?"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("graph cue queries = %v, want %v", got, want)
	}
	if got, want := graph.seedSets, [][]string{{"alice"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("graph cluster seeds = %v, want %v", got, want)
	}
	if got, want := view.BridgeCandidateIDs, []string{"candidate-gap"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bridge candidates = %v, want %v", got, want)
	}
	if got, want := view.Metadata.SourceIDs, []string{"source-1", "source-2", "source-gap"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("graph view sources = %v, want full source lineage %v", got, want)
	}
	if graph.mutationCalls != 0 {
		t.Fatalf("graph adapter invoked a mutation method %d times", graph.mutationCalls)
	}
	if _, err := os.Stat(graphShadowPath(runDir)); err != nil {
		t.Fatalf("graph shadow was not written: %v", err)
	}
	if err := clearGraphShadow(runDir); err != nil {
		t.Fatalf("clear graph shadow: %v", err)
	}
	if _, err := os.Stat(graphShadowPath(runDir)); !os.IsNotExist(err) {
		t.Fatalf("graph shadow exists after clear: %v", err)
	}
	assertProjectionProductDBUnchanged(t, productDB, before)
}

func TestGraphProjectionLeaves003GraphDataUnchanged(t *testing.T) {
	ctx := context.Background()
	opened, err := store.Open(ctx, store.Options{DSN: filepath.Join(t.TempDir(), "graph.db")})
	if err != nil {
		t.Fatalf("open graph fixture: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	entryStore := memory.NewEntryStore(opened.DB())
	for _, name := range []string{"a-seed-entry", "z-bridge-entry"} {
		if err := entryStore.Upsert(ctx, &memory.Entry{Name: name, Content: name}); err != nil {
			t.Fatalf("upsert graph fixture %q: %v", name, err)
		}
	}
	if err := entryStore.PutEntities(ctx, "a-seed-entry", []string{"alice"}); err != nil {
		t.Fatalf("seed entities: %v", err)
	}
	if err := entryStore.PutEntities(ctx, "z-bridge-entry", []string{"avery"}); err != nil {
		t.Fatalf("bridge entities: %v", err)
	}
	if err := entryStore.UpsertEdges(ctx, []memory.EntityEdge{{A: "alice", B: "avery", Kind: "co", Weight: 1}}); err != nil {
		t.Fatalf("fixture edge: %v", err)
	}
	beforeEdges, err := entryStore.NeighborsOf(ctx, []string{"alice"}, nil)
	if err != nil {
		t.Fatalf("read edges before shadow: %v", err)
	}
	beforeEntries, err := entryStore.EntityClusterEntries(ctx, []string{"alice"})
	if err != nil {
		t.Fatalf("read entries before shadow: %v", err)
	}

	seed := projectionTestCandidates()[:1]
	seed[0].Metadata["entry_name"] = "a-seed-entry"
	bridge := gapSupplementCandidate()
	bridge.Metadata = map[string]string{"entry_name": "z-bridge-entry"}
	shadow, err := buildGraphProjectionShadow(ctx, entryStore, graphProjectionRequest{
		Config:              projectionRunConfig{RunDir: t.TempDir(), CandidateSetDigest: "sha256:graph-data-parity", TokenCap: 1024, CandidateLimit: 2},
		Query:               "How are Alice and Avery linked?",
		SeedCandidates:      seed,
		CandidateCatalog:    append(seed, bridge),
		MaxBridgeCandidates: 1,
	})
	if err != nil {
		t.Fatalf("build graph shadow through 003 public API: %v", err)
	}
	if got, want := shadow.BridgeCandidateIDs, []string{"candidate-gap"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("graph treated seed as bridge: got %v want %v", got, want)
	}

	afterEdges, err := entryStore.NeighborsOf(ctx, []string{"alice"}, nil)
	if err != nil {
		t.Fatalf("read edges after shadow: %v", err)
	}
	afterEntries, err := entryStore.EntityClusterEntries(ctx, []string{"alice"})
	if err != nil {
		t.Fatalf("read entries after shadow: %v", err)
	}
	if !reflect.DeepEqual(afterEdges, beforeEdges) || !reflect.DeepEqual(afterEntries, beforeEntries) {
		t.Fatalf("graph shadow changed existing 003 data:\n edges got=%+v want=%+v\n entries got=%v want=%v", afterEdges, beforeEdges, afterEntries, beforeEntries)
	}
}

type fakeGraphProjectionReader struct {
	cues          []string
	entries       []string
	queries       []string
	seedSets      [][]string
	mutationCalls int
}

func (reader *fakeGraphProjectionReader) EntityCues(_ context.Context, query string) ([]string, error) {
	reader.queries = append(reader.queries, query)
	return append([]string(nil), reader.cues...), nil
}

func (reader *fakeGraphProjectionReader) EntityClusterEntries(_ context.Context, seeds []string) ([]string, error) {
	reader.seedSets = append(reader.seedSets, append([]string(nil), seeds...))
	return append([]string(nil), reader.entries...), nil
}
