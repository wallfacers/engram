package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
