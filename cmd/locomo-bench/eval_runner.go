package main

import "fmt"

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
