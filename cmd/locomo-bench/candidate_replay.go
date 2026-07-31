package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wallfacers/engram/memory"
)

// candidate_replay.go hosts the T114 candidate-replay protocol layer: the
// retrieval product of one question is persisted once and every compiler arm
// reads the same bytes afterwards (post-freeze retrieval = 0). The replay is
// bound to the protocol hash, question ID, and query digest; any drift fails
// closed instead of silently rematerializing different candidates.

const candidateReplaySchema = "022.v1.candidate-replay"
const candidateReplayDirName = "candidate-replay"

// formalCandidateReplay is the immutable per-question retrieval snapshot
// shared by all four compiler arms.
type formalCandidateReplay struct {
	Schema       string          `json:"schema"`
	ProtocolHash string          `json:"protocol_hash"`
	QuestionID   string          `json:"question_id"`
	QueryDigest  string          `json:"query_digest"`
	Hits         []memory.Result `json:"hits"`
	Digest       string          `json:"digest"`
}

func candidateReplayPath(runDir, questionID string) string {
	return filepath.Join(runDir, candidateReplayDirName, questionID+".json")
}

// candidateReplayDigest is deterministic over every binding field and the
// exact hits bytes, so any option/manifest/query drift changes the digest.
func candidateReplayDigest(replay formalCandidateReplay) (string, error) {
	payload, err := json.Marshal(replay.Hits)
	if err != nil {
		return "", fmt.Errorf("marshal candidate replay hits: %w", err)
	}
	raw := strings.Join([]string{replay.Schema, replay.ProtocolHash, replay.QuestionID, replay.QueryDigest, string(payload)}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// writeFormalCandidateReplay persists the retrieval product of one question.
// A write failure is surfaced to the caller; the run must not silently
// continue with an un-replayable candidate set.
func writeFormalCandidateReplay(runDir string, protocol evalProtocol, questionID, query string, hits []memory.Result) error {
	if strings.TrimSpace(runDir) == "" || strings.TrimSpace(questionID) == "" || !isDigest(protocol.ProtocolHash) {
		return fmt.Errorf("candidate replay requires run dir, question ID, and a frozen protocol hash")
	}
	replay := formalCandidateReplay{
		Schema:       candidateReplaySchema,
		ProtocolHash: protocol.ProtocolHash,
		QuestionID:   questionID,
		QueryDigest:  evalTextDigest(query),
		Hits:         hits,
	}
	digest, err := candidateReplayDigest(replay)
	if err != nil {
		return err
	}
	replay.Digest = digest
	path := candidateReplayPath(runDir, questionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create candidate-replay dir: %w", err)
	}
	if err := writeJSON(path, replay); err != nil {
		return fmt.Errorf("write candidate replay: %w", err)
	}
	return nil
}

// loadFormalCandidateReplay reads a previously written replay and rejects any
// identity or digest drift (protocol hash, question ID, query, or hits bytes)
// as a hard error: the four arms must consume byte-identical candidates.
func loadFormalCandidateReplay(runDir, protocolHash, questionID, query string) (formalCandidateReplay, error) {
	var replay formalCandidateReplay
	if err := readJSON(candidateReplayPath(runDir, questionID), &replay); err != nil {
		return formalCandidateReplay{}, err
	}
	if replay.Schema != candidateReplaySchema ||
		replay.ProtocolHash != protocolHash ||
		replay.QuestionID != questionID ||
		replay.QueryDigest != evalTextDigest(query) {
		return formalCandidateReplay{}, fmt.Errorf("candidate replay identity drift for question %q", questionID)
	}
	expected, err := candidateReplayDigest(replay)
	if err != nil {
		return formalCandidateReplay{}, err
	}
	if replay.Digest != expected {
		return formalCandidateReplay{}, fmt.Errorf("candidate replay digest drift for question %q", questionID)
	}
	if len(replay.Hits) == 0 {
		return formalCandidateReplay{}, fmt.Errorf("candidate replay is empty for question %q", questionID)
	}
	return replay, nil
}
