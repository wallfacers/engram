package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func fixtureDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func testCandidateArtifact() evalCandidateArtifact {
	anchorText := "atomic fact navigation hit"
	renderedText := "Alice moved to Portland in 2023."
	anchors := []evalRankedAnchor{{
		CandidateID: "fact-1",
		Rank:        1,
		Score:       0.91,
		TextDigest:  fixtureDigest(anchorText),
		SourceIDs:   []string{"e-1"},
	}}
	rendered := []evalRenderedCandidate{{
		CandidateID:       "fact-1",
		Kind:              "raw_turn",
		Rank:              1,
		Score:             0.91,
		Text:              renderedText,
		TextDigest:        fixtureDigest(renderedText),
		SourceIDs:         []string{"e-1"},
		ExpandedFrom:      []string{"fact-1"},
		ExpansionCount:    0,
		PreCapInputTokens: 27,
		Truncated:         false,
	}}
	return evalCandidateArtifact{
		Schema:             evalProtocolSchema,
		ProtocolHash:       "sha256:protocol",
		QuestionID:         "locomo:1:2",
		QueryDigest:        fixtureDigest("when did alice move"),
		Mode:               evalCandidateModeAnchorRendering,
		AnchorDigest:       rankedAnchorDigest(anchors),
		CandidateSetDigest: renderedCandidateSetDigest(rendered),
		RetrievalCalls:     1,
		Anchors:            anchors,
		RenderedCandidates: rendered,
		Gold: evalGoldResolution{
			DatasetSourceIDs:       []string{"dataset-turn-7", "dataset-turn-8"},
			ResolvedEvidenceIDs:    []string{"e-1", "e-2"},
			AnchorSourceCoverage:   0.5,
			RenderedSourceCoverage: 0.5,
		},
		CoverageStratum: "[0.500,0.900)",
	}
}

func TestCandidateArtifactRequiresStableRanksDigestsAndCoverage(t *testing.T) {
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	artifact := testCandidateArtifact()
	if err := validateEvalCandidateArtifact(protocol, artifact); err != nil {
		t.Fatalf("valid candidate artifact rejected: %v", err)
	}

	if got := sourceCoverage([]string{"e-1", "e-2"}, []string{"e-1", "e-1", "e-other"}); got != 0.5 {
		t.Fatalf("source coverage = %v, want 0.5 with duplicate/unknown IDs ignored", got)
	}
	if got, want := coverageStratumFor(protocol.CoverageStrata.Boundaries, 0.5), "[0.500,0.900)"; got != want {
		t.Fatalf("coverage stratum = %q, want %q", got, want)
	}

	drifted := testCandidateArtifact()
	drifted.RenderedCandidates[0].Text = "Alice moved to Seattle in 2023."
	if err := validateEvalCandidateArtifact(protocol, drifted); err == nil {
		t.Fatal("rendered candidate text-digest drift unexpectedly accepted")
	}

	badRank := testCandidateArtifact()
	badRank.Anchors[0].Rank = 2
	if err := validateEvalCandidateArtifact(protocol, badRank); err == nil {
		t.Fatal("non-contiguous anchor rank unexpectedly accepted")
	}
}

func TestCandidateArtifactJSONLRoundTripAndDigestTamper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidates.jsonl")
	want := []evalCandidateArtifact{testCandidateArtifact()}
	if err := writeEvalCandidateArtifacts(path, want); err != nil {
		t.Fatalf("write candidate artifacts: %v", err)
	}
	got, err := readEvalCandidateArtifacts(path)
	if err != nil {
		t.Fatalf("read candidate artifacts: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate JSONL round trip = %#v, want %#v", got, want)
	}

	got[0].CandidateSetDigest = "sha256:tampered"
	protocol := testEvalProtocol()
	protocol.ProtocolHash = "sha256:protocol"
	if err := validateEvalCandidateArtifact(protocol, got[0]); err == nil {
		t.Fatal("candidate-set digest tamper unexpectedly accepted")
	}
}
