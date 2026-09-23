package main

import (
	"strings"
	"testing"
)

func TestFrozenInvocationTemplates(t *testing.T) {
	cl := ClaudeTemplate("/home/u/.claude/settings.json.aly_qwen_w")
	joined := strings.Join(cl.Args, " ")
	if !strings.Contains(joined, "--settings /home/u/.claude/settings.json.aly_qwen_w") {
		t.Fatalf("claude template must pin the aly_qwen_w settings: %s", joined)
	}
	if strings.Contains(joined, "--settings /home/u/.claude/settings.json ") {
		t.Fatalf("ordinary settings.json must never be a fallback: %s", joined)
	}
	cx := CodexTemplate("aq", "qwen3.8-flash")
	jc := strings.Join(cx.Args, " ")
	if !strings.Contains(jc, "-c model_provider=aq -c model=qwen3.8-flash --yolo") {
		t.Fatalf("codex template must freeze the aq/qwen3.8-flash overrides: %s", jc)
	}
	oc := OpenCodeTemplate("free-provider/free-model")
	jo := strings.Join(oc.Args, " ")
	if !strings.Contains(jo, "--model free-provider/free-model") {
		t.Fatalf("opencode template must carry the explicit free model: %s", jo)
	}
	// Missing configuration is a construction error, not a silent default.
	if _, err := TemplateForHost(HostClaude, "", "aq", "qwen3.8-flash", "m"); err == nil {
		t.Fatal("claude without settings path must fail")
	}
	if _, err := TemplateForHost(HostCodex, "s", "", "qwen3.8-flash", "m"); err == nil {
		t.Fatal("codex without provider must fail")
	}
	if _, err := TemplateForHost(HostOpenCode, "s", "aq", "qwen3.8-flash", ""); err == nil {
		t.Fatal("opencode without free model must fail")
	}
	// Template digests are stable and prompt-free.
	d1, _ := cx.Digest()
	d2, _ := CodexTemplate("aq", "qwen3.8-flash").Digest()
	if d1 != d2 {
		t.Fatal("identical templates must digest identically")
	}
}

func TestToolIdentityDigestStability(t *testing.T) {
	p := ToolProvenance{
		Host: HostCodex, CLIVersion: "0.149.1", BinarySHA256: strings.Repeat("a", 64),
		InvocationTemplateID: "codex-exec-yolo-v1", InvocationTemplateDigest: strings.Repeat("b", 64),
		RequestedProfile: strPtr("tf"), ResolvedModel: "model-x", BillingClass: BillingAuthorized,
		SourceRevision: "runner-rev-1", CapturedAt: "2026-09-01T00:00:00Z",
	}
	d1, err := ComputeToolIdentityDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	// captured_at may differ across runs; the stable digest must not change.
	p.CapturedAt = "2030-01-01T12:00:00Z"
	d2, _ := ComputeToolIdentityDigest(p)
	if d1 != d2 {
		t.Fatal("tool_identity_digest must exclude captured_at")
	}
	// Any real identity drift changes it.
	p.ResolvedModel = "model-y"
	if d3, _ := ComputeToolIdentityDigest(p); d3 == d1 {
		t.Fatal("resolved-model drift must change the stable digest")
	}
}

func TestBatchModelIdentityDiscipline(t *testing.T) {
	mk := func(host, model string) ToolProvenance {
		return ToolProvenance{Host: host, ResolvedModel: model, BillingClass: BillingAuthorized}
	}
	// Free opencode provenance requires explicit resolved identity.
	oc := ToolProvenance{Host: HostOpenCode, BillingClass: BillingFree, RequestedModel: strPtr("free/m"), ResolvedModel: "free/m"}
	if err := ValidateProvenance(oc); err != nil {
		t.Fatalf("valid free opencode provenance rejected: %v", err)
	}
	if err := ValidateProvenance(ToolProvenance{Host: HostOpenCode, BillingClass: BillingFree, ResolvedModel: ResolvedUnavailable}); err == nil {
		t.Fatal("absent opencode identity cannot be relabeled free")
	}
	if err := ValidateProvenance(ToolProvenance{Host: HostClaude, BillingClass: BillingFree}); err == nil {
		t.Fatal("billing_class=free is invalid for claude")
	}
	// Batch: stable per host; hosts MAY share a model (2026-09-01 unification).
	good := []ToolProvenance{mk(HostClaude, "m-claude"), mk(HostCodex, "m-codex"), mk(HostOpenCode, "m-open")}
	if err := CheckBatchModelIdentity(good); err != nil {
		t.Fatalf("well-formed batch rejected: %v", err)
	}
	unified := []ToolProvenance{mk(HostClaude, "qwen3.8-flash"), mk(HostCodex, "qwen3.8-flash"), mk(HostOpenCode, "qwen3.8-flash")}
	if err := CheckBatchModelIdentity(unified); err != nil {
		t.Fatalf("unified-model batch must seal (maintainer 2026-09-01): %v", err)
	}
	// Host-internal drift still blocks sealing.
	drift := []ToolProvenance{mk(HostClaude, "m1"), mk(HostClaude, "m2"), mk(HostCodex, "m3"), mk(HostOpenCode, "m4")}
	if err := CheckBatchModelIdentity(drift); err == nil {
		t.Fatal("host-internal model drift must block sealing")
	}
	// Unavailable anywhere blocks the batch.
	un := []ToolProvenance{mk(HostClaude, "a"), mk(HostCodex, "b"), mk(HostOpenCode, ResolvedUnavailable)}
	if err := CheckBatchModelIdentity(un); err == nil {
		t.Fatal("an unavailable lane model must block the batch (no host-brand substitution)")
	}
}

func TestReviewerHostDistinctness(t *testing.T) {
	if err := RequireDistinctReviewerHosts("claude", []string{"codex", "opencode"}); err != nil {
		t.Fatalf("valid distinct trio rejected: %v", err)
	}
	if err := RequireDistinctReviewerHosts("claude", []string{"claude", "opencode"}); err == nil {
		t.Fatal("author self-review must be rejected")
	}
	if err := RequireDistinctReviewerHosts("claude", []string{"codex", "codex"}); err == nil {
		t.Fatal("duplicate reviewer host must be rejected")
	}
}

func TestSecretLikeRejection(t *testing.T) {
	for _, s := range []string{"api_key=sk-123", "Bearer abc.def", "password: hunter2", "AUTH_TOKEN"} {
		if !IsSecretLike(s) {
			t.Errorf("secret-like %q not detected", s)
		}
	}
	if _, err := SanitizeProvenanceString("cli_version", "AUTH_TOKEN abc"); err == nil {
		t.Fatal("secret-like provenance value must be rejected")
	}
	if _, err := SanitizeProvenanceString("cli_version", "2.1.251"); err != nil {
		t.Fatalf("benign version string rejected: %v", err)
	}
}
