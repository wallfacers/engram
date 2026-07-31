package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wallfacers/engram/memory"
)

func testReplayProtocol(t *testing.T) evalProtocol {
	t.Helper()
	protocol := testFormalProtocolBase(t)
	protocol.Experiment = evalExperimentProtocol{
		Stage: "compiler", Arm: "exact_token", PrimaryCohort: "all",
		MechanismFlags: map[string]bool{"compiler": true}, ControlProtocolHash: "sha256:control",
	}
	frozen, err := freezeEvalProtocolFile(filepath.Join(t.TempDir(), "protocol.json"), protocol, evalRunFormal)
	if err != nil {
		t.Fatalf("freeze protocol: %v", err)
	}
	return frozen
}

func TestFormalCandidateReplayRoundTrip(t *testing.T) {
	runDir := t.TempDir()
	protocol := testReplayProtocol(t)
	hits := []memory.Result{
		{ID: "hit-1", Content: "Alice met Bob in Tokyo.", Score: 0.9, SourceSessionID: "s-1"},
		{ID: "hit-2", Content: "Bob left for Osaka.", Score: 0.4, SourceSessionID: "s-2"},
	}
	if err := writeFormalCandidateReplay(runDir, protocol, "q-1", "where did Bob go?", hits); err != nil {
		t.Fatalf("write replay: %v", err)
	}
	if _, err := os.Stat(candidateReplayPath(runDir, "q-1")); err != nil {
		t.Fatalf("replay file missing: %v", err)
	}
	loaded, err := loadFormalCandidateReplay(runDir, protocol.ProtocolHash, "q-1", "where did Bob go?")
	if err != nil {
		t.Fatalf("load replay: %v", err)
	}
	if !reflect.DeepEqual(loaded.Hits, hits) {
		t.Fatalf("replay hits mismatch: %#v vs %#v", loaded.Hits, hits)
	}
	if !isDigest(loaded.Digest) {
		t.Fatalf("replay digest missing: %#v", loaded)
	}
}

func TestFormalCandidateReplayRejectsDrift(t *testing.T) {
	runDir := t.TempDir()
	protocol := testReplayProtocol(t)
	hits := []memory.Result{{ID: "hit-1", Content: "original evidence", Score: 0.9}}
	if err := writeFormalCandidateReplay(runDir, protocol, "q-1", "original query", hits); err != nil {
		t.Fatalf("write replay: %v", err)
	}
	cases := []struct {
		name       string
		protocol   evalProtocol
		questionID string
		query      string
		mutate     func(path string)
	}{
		{"wrong protocol hash", testReplayProtocol(t), "q-1", "original query", nil},
		{"wrong question id", protocol, "q-2", "original query", nil},
		{"wrong query", protocol, "q-1", "different query", nil},
		{"hits mutated on disk", protocol, "q-1", "original query", func(path string) {
			var replay formalCandidateReplay
			raw, _ := os.ReadFile(path)
			_ = json.Unmarshal(raw, &replay)
			replay.Hits[0].Content = "tampered evidence"
			out, _ := json.MarshalIndent(replay, "", "  ")
			_ = os.WriteFile(path, append(out, '\n'), 0o644)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.mutate != nil {
				tc.mutate(candidateReplayPath(runDir, "q-1"))
			}
			if _, err := loadFormalCandidateReplay(runDir, tc.protocol.ProtocolHash, tc.questionID, tc.query); err == nil {
				t.Fatalf("%s: drift was accepted", tc.name)
			}
		})
	}
}

func TestWriteFormalCandidateReplayRequiresFrozenProtocol(t *testing.T) {
	runDir := t.TempDir()
	if err := writeFormalCandidateReplay(runDir, evalProtocol{}, "q-1", "q", nil); err == nil {
		t.Fatal("replay write without a frozen protocol hash must fail")
	}
}

func TestCandidateReplayQueryDigestIsStable(t *testing.T) {
	if got, want := evalTextDigest("where did Bob go?"), evalTextDigest("where did Bob go?"); got != want {
		t.Fatalf("query digest not stable: %s vs %s", got, want)
	}
}
