package main

// 036 attribution CLI option-validation tests. These mirror the real
// flag-parsed defaults (adjudicationMaxTokens = 512, seed/allow-paid off), so
// they catch the same path a `go run` invocation exercises — the 034/035
// validation convention only rejects mode-foreign *explicit* options, never the
// shared flags' defaults. No provider, no network, no file I/O.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// attributionCLIOptions returns an options value with the 036 attribution mode
// configured exactly as the real flag parser would leave it: DIR + three
// candidates, optional audit dir, and every shared adjudication flag at its
// default (maxTokens = 512, seed empty, allow-paid off).
func attributionCLIOptions(dir string, auditDir string) options {
	opt := options{
		adjudicationAttributionDir: dir,
		adjudicationCandidates:     []string{"c1", "c2", "c3"},
		adjudicationMaxTokens:      512, // flag default
	}
	if auditDir != "" {
		opt.adjudicationAuditDir = auditDir
	}
	return opt
}

func TestAttributionCLIValidationAcceptsFlagDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "034-stage0")
	if err := validateAdjudicationAttributionOptions(attributionCLIOptions(dir, "")); err != nil {
		t.Fatalf("attribution with flag-default maxTokens/seed must validate, got: %v", err)
	}
}

func TestAttributionCLIValidationAcceptsOptionalAuditDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "034-stage0")
	audit := filepath.Join(t.TempDir(), "035-stage0")
	if err := os.MkdirAll(audit, 0o700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	opt := attributionCLIOptions(dir, audit)
	if err := validateAdjudicationAttributionOptions(opt); err != nil {
		t.Fatalf("attribution with an existing audit dir must validate, got: %v", err)
	}
}

func TestAttributionCLIValidationRejectsAuditDirEqualTo034Dir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "034-stage0")
	opt := attributionCLIOptions(dir, dir)
	if err := validateAdjudicationAttributionOptions(opt); err == nil {
		t.Fatal("attribution audit dir equal to the 034 dir must be rejected")
	}
}

func TestAttributionCLIValidationRejectsModeForeignExplicitOptions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "034-stage0")
	cases := []struct {
		name   string
		mutate func(*options)
	}{
		{"explicit seed", func(o *options) { o.adjudicationSeed = "034-stage0-v1" }},
		{"explicit allow-paid", func(o *options) { o.adjudicationAllowPaid = true }},
		{"explicit trace", func(o *options) { o.adjudicationTracePath = "/tmp/trace.jsonl" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt := attributionCLIOptions(dir, "")
			tc.mutate(&opt)
			if err := validateAdjudicationAttributionOptions(opt); err == nil {
				t.Fatal("attribution must reject mode-foreign explicit options")
			} else if !strings.Contains(err.Error(), "attribution accepts only") {
				t.Fatalf("unexpected rejection reason: %v", err)
			}
		})
	}
}

func TestAttributionCLIValidationRequiresExactlyThreeCandidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "034-stage0")
	opt := attributionCLIOptions(dir, "")
	opt.adjudicationCandidates = []string{"c1", "c2"}
	if err := validateAdjudicationAttributionOptions(opt); err == nil {
		t.Fatal("attribution with two candidates must be rejected")
	}
}
