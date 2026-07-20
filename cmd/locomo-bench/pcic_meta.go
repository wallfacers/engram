package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wallfacers/engram/memory"
)

func pcicDatasetFingerprint(path string) (string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-selected benchmark dataset
	if err != nil {
		return "", fmt.Errorf("fingerprint PCIC dataset: %w", err)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum), nil
}

type ClaimPolarity string

const (
	PolarityAffirm ClaimPolarity = "affirm"
	PolarityNegate ClaimPolarity = "negate"
)

type SpanClaim struct {
	SpanID        string        `json:"span_id"`
	Entity        string        `json:"entity"`
	Slot          string        `json:"slot"`
	Value         string        `json:"value"`
	Polarity      ClaimPolarity `json:"polarity"`
	TimeState     string        `json:"time_state"`
	SourceTurnIDs []string      `json:"source_turn_ids"`
}

type PCICMetaHeader struct {
	AnnotateModel      string `json:"annotate_model"`
	DatasetFingerprint string `json:"dataset_fingerprint"`
	Count              int    `json:"count"`
}

type PCICMeta struct {
	Header PCICMetaHeader       `json:"header"`
	Spans  map[string]SpanClaim `json:"spans"`
}

func savePCICMeta(path string, meta PCICMeta) error {
	if err := validatePCICMeta(meta); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create pcic_meta directory: %w", err)
	}
	if err := writeJSON(path, meta); err != nil {
		return fmt.Errorf("write pcic_meta: %w", err)
	}
	return nil
}

func loadPCICMeta(path string, expected PCICMetaHeader, logger *slog.Logger) (*PCICMeta, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-selected sidecar
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pcic_meta: %w", err)
	}
	var meta PCICMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("decode pcic_meta: %w", err)
	}
	if err := validatePCICMeta(meta); err != nil {
		return nil, err
	}
	if !pcicHeadersMatch(meta.Header, expected) {
		if logger != nil {
			logger.Warn("pcic_meta header mismatch; selector will use rerank order",
				"annotate_model", meta.Header.AnnotateModel,
				"dataset_fingerprint", meta.Header.DatasetFingerprint)
		}
		return nil, nil
	}
	return &meta, nil
}

func pcicHeadersMatch(got, expected PCICMetaHeader) bool {
	return (expected.AnnotateModel == "" || got.AnnotateModel == expected.AnnotateModel) &&
		(expected.DatasetFingerprint == "" || got.DatasetFingerprint == expected.DatasetFingerprint)
}

// pcicAnnotatePrompt is the bench-local one-time annotation template. It lives
// in the bench package, NOT memory/prompt/, so the offline annotation pass adds
// nothing to the engine contract (Constitution II; plan R3). It extracts one
// typed claim per dialogue turn — the query-time path stays LLM-free.
const pcicAnnotatePrompt = `You extract ONE typed factual claim from a single dialogue turn for offline memory indexing. Read the turn and output a single JSON object with these fields:
- "entity": the normalized subject the claim is about (usually a person's name); "" if the turn asserts no durable fact
- "slot": the attribute or relation name (e.g. "job", "location", "owns_pet"); "" if none
- "value": the slot's value
- "polarity": "affirm" if the turn asserts the value, "negate" if it denies it
- "time_state": a coarse temporal label — "past", "current", or a period key — distinguishing state changes
- "source_turn_ids": array of turn ids the claim is grounded in (usually just this turn's id)

Rules:
- Output ONLY the JSON object, nothing else. No markdown, no explanation.
- If the turn carries no durable factual claim (small talk, a question, an acknowledgement), output {"entity":""}.`

// pcicClaimJSON is the raw shape the annotation model returns per turn.
type pcicClaimJSON struct {
	Entity        string   `json:"entity"`
	Slot          string   `json:"slot"`
	Value         string   `json:"value"`
	Polarity      string   `json:"polarity"`
	TimeState     string   `json:"time_state"`
	SourceTurnIDs []string `json:"source_turn_ids"`
}

// annotatePCICMeta runs the one-time offline annotation pass: one typed claim
// per dialogue turn via the injected model caller. It opens no engine store and
// writes no engine state — the result is a pure sidecar map keyed by dialogue
// id. Turns the model reports as claimless are omitted (an absent span is role
// "unknown" at selection time). concurrency bounds in-flight annotation calls;
// the caller must be safe for concurrent use.
func annotatePCICMeta(ctx context.Context, convs []conversation, model, fingerprint string, call modelCaller, concurrency int, logger *slog.Logger) (PCICMeta, error) {
	type job struct {
		tn turn
	}
	var jobs []job
	for _, conv := range convs {
		for _, sess := range conv.Sessions {
			for _, tn := range sess.Turns {
				if strings.TrimSpace(tn.DiaID) == "" {
					continue
				}
				jobs = append(jobs, job{tn: tn})
			}
		}
	}
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		mu       sync.Mutex
		spans    = make(map[string]SpanClaim, len(jobs))
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, concurrency)
	)
	for _, j := range jobs {
		select {
		case <-ctx.Done():
			mu.Lock()
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			mu.Unlock()
		default:
		}
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(tn turn) {
			defer wg.Done()
			defer func() { <-sem }()
			claim, ok, err := annotateTurn(ctx, call, tn)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("annotate turn %s: %w", tn.DiaID, err)
				}
				return
			}
			if ok {
				spans[tn.DiaID] = claim
			}
		}(j.tn)
	}
	wg.Wait()
	if firstErr != nil {
		return PCICMeta{}, firstErr
	}
	if logger != nil {
		logger.Info("pcic annotation complete", "turns", len(jobs), "claims", len(spans))
	}
	return PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: model, DatasetFingerprint: fingerprint, Count: len(spans)},
		Spans:  spans,
	}, nil
}

// annotateTurn annotates a single dialogue turn. ok=false means the model
// reported no durable claim (or its output could not be parsed) — the span is
// then simply absent from the sidecar.
func annotateTurn(ctx context.Context, call modelCaller, tn turn) (SpanClaim, bool, error) {
	user := fmt.Sprintf("Turn id: %s\nSpeaker: %s\nText: %s", tn.DiaID, tn.Speaker, tn.Text)
	raw, err := call(ctx, pcicAnnotatePrompt, user)
	if err != nil {
		return SpanClaim{}, false, err
	}
	parsed, ok := parsePCICClaim(raw)
	if !ok {
		return SpanClaim{}, false, nil
	}
	entity := memory.EntityNorm(parsed.Entity)
	if entity == "" {
		return SpanClaim{}, false, nil
	}
	polarity := PolarityAffirm
	if strings.EqualFold(strings.TrimSpace(parsed.Polarity), string(PolarityNegate)) {
		polarity = PolarityNegate
	}
	source := parsed.SourceTurnIDs
	if len(source) == 0 {
		source = []string{tn.DiaID}
	}
	return SpanClaim{
		SpanID:        tn.DiaID,
		Entity:        entity,
		Slot:          strings.TrimSpace(parsed.Slot),
		Value:         strings.TrimSpace(parsed.Value),
		Polarity:      polarity,
		TimeState:     strings.TrimSpace(parsed.TimeState),
		SourceTurnIDs: source,
	}, true, nil
}

// parsePCICClaim tolerates markdown code fences and stray prose around the JSON
// object the annotation model returns. ok=false when no claim object is present
// or the entity is empty (claimless turn).
func parsePCICClaim(raw string) (pcicClaimJSON, bool) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return pcicClaimJSON{}, false
	}
	var c pcicClaimJSON
	if err := json.Unmarshal([]byte(s[start:end+1]), &c); err != nil {
		return pcicClaimJSON{}, false
	}
	if strings.TrimSpace(c.Entity) == "" {
		return pcicClaimJSON{}, false
	}
	return c, true
}

func validatePCICMeta(meta PCICMeta) error {
	if meta.Spans == nil {
		return fmt.Errorf("pcic_meta spans must not be null")
	}
	if meta.Header.Count != len(meta.Spans) {
		return fmt.Errorf("pcic_meta header count %d does not match %d spans", meta.Header.Count, len(meta.Spans))
	}
	for key, claim := range meta.Spans {
		if key == "" || claim.SpanID == "" {
			return fmt.Errorf("pcic_meta span id must not be empty")
		}
		if key != claim.SpanID {
			return fmt.Errorf("pcic_meta span key %q does not match span_id %q", key, claim.SpanID)
		}
	}
	return nil
}
