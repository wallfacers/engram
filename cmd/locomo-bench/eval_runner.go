package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// prepareFrozenEvalOptions returns the only option set a formal 022 run may
// pass to the legacy harness. The formal protocol has one retrieval and one
// answer path per question: no IRIS expansion, no hosted reranker, and no
// answer-dependent IDK retry. Keep this guard at the runner boundary so a
// future call site cannot accidentally make the frozen protocol adaptive.
func prepareFrozenEvalOptions(protocol evalProtocol, requested options) (options, error) {
	if err := validateEvalProtocol(protocol, evalRunFormal); err != nil {
		return options{}, fmt.Errorf("invalid formal evaluation protocol: %w", err)
	}
	if requested.iris {
		return options{}, fmt.Errorf("formal 022 evaluation refuses --iris")
	}
	if requested.rerank {
		return options{}, fmt.Errorf("formal 022 evaluation refuses --rerank")
	}

	requested.iris = false
	requested.irisDepth = 0
	requested.rerank = false
	requested.noIDKRetry = true
	return requested, nil
}

// prepareFormalEvalRun pins an already-frozen manifest into the immutable run
// directory before any model, extraction, or retrieval work begins. A resume
// must present the byte-equivalent protocol fingerprint; a changed answerer,
// cap, dataset, or candidate recipe therefore cannot silently reuse artifacts.
func prepareFormalEvalRun(manifestPath, runDir string, requested options) (evalProtocol, options, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return evalProtocol{}, options{}, fmt.Errorf("formal evaluation requires --eval-protocol")
	}
	if strings.TrimSpace(runDir) == "" {
		return evalProtocol{}, options{}, fmt.Errorf("formal evaluation requires --run-dir")
	}
	protocol, err := readEvalProtocolFile(manifestPath)
	if err != nil {
		return evalProtocol{}, options{}, fmt.Errorf("read --eval-protocol: %w", err)
	}
	prepared, err := prepareFrozenEvalOptions(protocol, requested)
	if err != nil {
		return evalProtocol{}, options{}, err
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return evalProtocol{}, options{}, fmt.Errorf("create formal run dir: %w", err)
	}
	pinnedPath := filepath.Join(runDir, evalProtocolArtifactFile)
	if _, err := os.Stat(pinnedPath); err == nil {
		if err := checkEvalProtocolResume(runDir, protocol, evalRunFormal); err != nil {
			return evalProtocol{}, options{}, err
		}
	} else if os.IsNotExist(err) {
		if err := writeJSON(pinnedPath, protocol); err != nil {
			return evalProtocol{}, options{}, fmt.Errorf("pin formal protocol: %w", err)
		}
	} else {
		return evalProtocol{}, options{}, fmt.Errorf("stat pinned protocol: %w", err)
	}
	return protocol, prepared, nil
}
