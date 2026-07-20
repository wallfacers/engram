package main

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPCICMetaRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pcic_meta.json")
	want := PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: "sha256:fixture", Count: 2},
		Spans: map[string]SpanClaim{
			"D1:1": {SpanID: "D1:1", Entity: "alice", Slot: "job", Value: "engineer", Polarity: PolarityAffirm, TimeState: "current", SourceTurnIDs: []string{"D1:1"}},
			"D1:2": {SpanID: "D1:2", Entity: "alice", Slot: "location", Value: "paris", Polarity: PolarityAffirm, TimeState: "past", SourceTurnIDs: []string{"D1:2"}},
		},
	}
	if err := savePCICMeta(path, want); err != nil {
		t.Fatalf("savePCICMeta: %v", err)
	}
	got, err := loadPCICMeta(path, want.Header, slog.Default())
	if err != nil {
		t.Fatalf("loadPCICMeta: %v", err)
	}
	if got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestPCICMetaHeaderMismatchDegrades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pcic_meta.json")
	meta := PCICMeta{
		Header: PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: "sha256:old", Count: 1},
		Spans: map[string]SpanClaim{
			"D1:1": {SpanID: "D1:1", Entity: "alice", Slot: "job", Value: "engineer", Polarity: PolarityAffirm, SourceTurnIDs: []string{"D1:1"}},
		},
	}
	if err := savePCICMeta(path, meta); err != nil {
		t.Fatalf("savePCICMeta: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	got, err := loadPCICMeta(path, PCICMetaHeader{AnnotateModel: "gpt-5.6-luna", DatasetFingerprint: "sha256:new"}, logger)
	if err != nil {
		t.Fatalf("mismatch must not fail the run: %v", err)
	}
	if got != nil {
		t.Fatalf("mismatch loaded stale metadata: %#v", got)
	}
	if !strings.Contains(logs.String(), "header mismatch") {
		t.Fatalf("mismatch warning missing: %q", logs.String())
	}
}
