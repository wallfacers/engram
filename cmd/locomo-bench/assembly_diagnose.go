package main

// 030 US1 assembly diagnostic (specs/030 US1, quickstart.md). Retrieval must
// run inside the harness (hybrid search needs the embedding sidecar), so this
// mode emits the per-question evidence-assembly audit (chunk_fraction /
// total_tokens / structure / tokens_estimated) that assembly_diagnose.py
// consumes. It is fully offline for answerer/judge (zero paid calls); only the
// local embedding endpoint is used for retrieval, and the exact tokenizer is
// optional (estimate-ledger fallback marks tokens_estimated=true).
//
// The engine is untouched (FR-001).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/wallfacers/engram/embedding"
)

// assemblyJournal is a concurrency-safe writer for
// run-dir/assembly-diagnose.jsonl. Conversations run in parallel, so writes
// are mutex-guarded; one journal per run.
type assemblyJournal struct {
	mu  sync.Mutex
	f   *os.File
	bw  *bufio.Writer
	enc *json.Encoder
}

func openAssemblyJournal(path string) (*assemblyJournal, error) {
	// Owner-only (0o600): the audit carries raw question text and retrieved
	// memory content — sensitive data — so it must not be world/group readable.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriter(f)
	return &assemblyJournal{f: f, bw: bw, enc: json.NewEncoder(bw)}, nil
}

// Write appends one assembly record and flushes, so every completed record is
// durable even if a later write or the process fails.
func (j *assemblyJournal) Write(asm EvidenceAssembly) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j == nil || j.enc == nil {
		return nil
	}
	if err := j.enc.Encode(asm); err != nil {
		return err
	}
	return j.bw.Flush()
}

func (j *assemblyJournal) Close() error {
	if j == nil || j.f == nil {
		return nil
	}
	if j.bw != nil {
		_ = j.bw.Flush()
	}
	return j.f.Close()
}

// runAssemblyDiagnoseCLI emits the per-question assembly audit. It mirrors the
// 029 nav-diagnose runner: same runtime wiring (openAttributionRuntime), same
// per-question retrieval (retriever.Search + finalizeQuestionHits), then the
// 030 assembler. Retrieval-only; no answer/judge/extraction call.
func runAssemblyDiagnoseCLI(ctx context.Context, opt options, convs []conversation, arms []string, embClient embedding.Client, logger *slog.Logger) error {
	if err := validateAssemblyDiagnoseOptions(opt); err != nil {
		return err
	}
	if err := os.MkdirAll(opt.runDir, 0o755); err != nil {
		return fmt.Errorf("create assembly diagnose run dir: %w", err)
	}
	arm := arms[0]
	client := embClient
	if armBackend(arm) != "hybrid" {
		client = nil
	}

	diagnosticCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}

	path := filepath.Join(opt.runDir, "assembly-diagnose.jsonl")
	j, err := openAssemblyJournal(path)
	if err != nil {
		return fmt.Errorf("create assembly-diagnose.jsonl: %w", err)
	}
	defer j.Close() //nolint:errcheck

	counter := opt.assemblyCounter
	records := 0
	for _, conv := range convs {
		conv := conv
		wg.Add(1)
		go func() {
			defer wg.Done()
			if diagnosticCtx.Err() != nil {
				return
			}
			runtime, err := openAttributionRuntime(diagnosticCtx, opt, conv, client, arm)
			if err != nil {
				setErr(err)
				return
			}
			defer runtime.Close()
			retriever := runtime.retrievers[arm]
			for _, selected := range selectQuestions(conv, opt) {
				if diagnosticCtx.Err() != nil {
					return
				}
				qa := selected.QA
				armOpt := optionsForRun(opt, arm, false)
				topK, quota := armOpt.retrievalFor(qa.Category)
				searchK := questionSearchK(topK, quota)

				candidates, err := retriever.Search(diagnosticCtx, qa.Question, searchK)
				if err != nil {
					setErr(fmt.Errorf("assembly diagnose retrieve conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				hits := finalizeQuestionHits(diagnosticCtx, qa.Question, candidates, topK, quota, armOpt)
				asm, _, asmErr := assembleEvidence(diagnosticCtx, qa.Question, hits, qa.Category, assemblyConfig{
					Cap:             defaultAnswerContextCap,
					CurrentDate:     qa.QuestionDate,
					Scaffold:        opt.temporalDateScaffold,
					SystemPrompt:    "",
					QuestionID:      qa.QuestionID,
					RelationEnabled: opt.relationContext, // 031: diagnose the same block the answer path would inject
				}, counter)
				if asmErr != nil {
					setErr(fmt.Errorf("assembly diagnose assemble conv=%d question=%d: %w", conv.ID, selected.Index, asmErr))
					return
				}
				if err := j.Write(asm); err != nil {
					setErr(fmt.Errorf("write assembly diagnostic conv=%d question=%d: %w", conv.ID, selected.Index, err))
					return
				}
				mu.Lock()
				records++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	logger.Info("assembly diagnose complete", "questions", records, "out", path)
	return nil
}

func validateAssemblyDiagnoseOptions(opt options) error {
	if opt.runDir == "" {
		return fmt.Errorf("--run-dir is required with --assembly-diagnose")
	}
	if opt.storeDir == "" {
		return fmt.Errorf("--store-dir is required with --assembly-diagnose (retrieval-only mode never builds a store)")
	}
	if !opt.chunks {
		return fmt.Errorf("--assembly-diagnose requires --chunks so chunk/fact classification is meaningful")
	}
	return nil
}
