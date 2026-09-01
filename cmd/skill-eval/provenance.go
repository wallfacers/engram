package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// 048 provenance model (data-model.md §4): sanitized allowlist-only capture,
// stable tool identity digests for cross-series comparability, and closed
// host-model discipline for holdout author/review batches.

const (
	HostClaude   = "claude"
	HostCodex    = "codex"
	HostOpenCode = "opencode"

	ResolvedUnavailable = "unavailable"
	BillingAuthorized   = "existing-authorized"
	BillingFree         = "free"
	BillingUnknown      = "unknown"
)

var validHosts = map[string]bool{HostClaude: true, HostCodex: true, HostOpenCode: true}

// ToolProvenance is the sanitized per-attempt capture. Any token/key/password,
// raw env value, full endpoint, full settings/config or arbitrary stderr is
// NOT a field of this entity — only digests and names.
type ToolProvenance struct {
	Host                     string  `json:"host"`
	CLIVersion               string  `json:"cli_version"`
	BinarySHA256             string  `json:"binary_sha256"`
	InvocationTemplateID     string  `json:"invocation_template_id"`
	InvocationTemplateDigest string  `json:"invocation_template_digest"`
	RequestedProfile         *string `json:"requested_profile"`
	RequestedModel           *string `json:"requested_model"`
	ResolvedModel            string  `json:"resolved_model"`
	BillingClass             string  `json:"billing_class"`
	SettingsDigest           *string `json:"settings_digest"`
	EndpointDigest           *string `json:"endpoint_digest"`
	SourceRevision           string  `json:"source_revision"`
	CapturedAt               string  `json:"captured_at"`
	ToolIdentityDigest       string  `json:"tool_identity_digest"`
}

// toolIdentityProjection is the exact stable preimage of
// tool_identity_digest: it deliberately excludes captured_at, series/run/case
// and artifact IDs, output paths, skill packages, datasets and docs/specs
// (runner-only source_revision).
type toolIdentityProjection struct {
	Host                     string   `json:"host"`
	CLIVersion               string   `json:"cli_version"`
	BinarySHA256             string   `json:"binary_sha256"`
	InvocationTemplateID     string   `json:"invocation_template_id"`
	InvocationTemplateDigest string   `json:"invocation_template_digest"`
	RequestedProfile         *string  `json:"requested_profile"`
	RequestedModel           *string  `json:"requested_model"`
	ResolvedModel            string   `json:"resolved_model"`
	BillingClass             string   `json:"billing_class"`
	SettingsDigest           *string  `json:"settings_digest"`
	EndpointDigest           *string  `json:"endpoint_digest"`
	SourceRevision           string   `json:"source_revision"`
}

// ComputeToolIdentityDigest derives the stable per-host identity digest.
func ComputeToolIdentityDigest(p ToolProvenance) (string, error) {
	proj := toolIdentityProjection{
		Host: p.Host, CLIVersion: p.CLIVersion, BinarySHA256: p.BinarySHA256,
		InvocationTemplateID: p.InvocationTemplateID, InvocationTemplateDigest: p.InvocationTemplateDigest,
		RequestedProfile: p.RequestedProfile, RequestedModel: p.RequestedModel,
		ResolvedModel: p.ResolvedModel, BillingClass: p.BillingClass,
		SettingsDigest: p.SettingsDigest, EndpointDigest: p.EndpointDigest,
		SourceRevision: p.SourceRevision,
	}
	return CanonicalSHA256(proj)
}

// ---------- frozen invocation templates ----------

// InvocationTemplate is the frozen, nonsecret argv prefix for one host. Only
// the ID and its digest enter provenance; never the prompt or paths.
type InvocationTemplate struct {
	ID   string
	Args []string
}

// Digest binds the template's flag semantics (not prompts/paths/secrets).
func (t InvocationTemplate) Digest() (string, error) { return CanonicalSHA256(t) }

// ClaudeTemplate freezes `claude --settings <aly_qwen_w settings> …`: the
// maintainer-provided settings path is required; ordinary settings.json is
// never a fallback.
func ClaudeTemplate(settingsPath string) InvocationTemplate {
	return InvocationTemplate{
		ID:   "claude-settings-print-v1",
		Args: []string{"claude", "--settings", settingsPath, "--output-format", "stream-json", "--verbose", "-p"},
	}
}

// CodexTemplate freezes `codex -c model_provider=<provider> -c model=<model> --yolo exec --json …`
// (maintainer 2026-09-01: the aq provider on Aliyun Bailian with
// qwen3.8-flash — `-c` config overrides, no profile required).
func CodexTemplate(provider, model string) InvocationTemplate {
	return InvocationTemplate{
		ID:   "codex-exec-yolo-v2",
		Args: []string{"codex", "-c", "model_provider=" + provider, "-c", "model=" + model, "--yolo", "exec", "--json"},
	}
}

// OpenCodeTemplate freezes `opencode2 run --standalone --format json --model <free-model> …`.
func OpenCodeTemplate(freeModel string) InvocationTemplate {
	return InvocationTemplate{
		ID:   "opencode-run-free-v1",
		Args: []string{"opencode2", "run", "--standalone", "--auto", "--format", "json", "--model", freeModel},
	}
}

// TemplateForHost returns the frozen template for a host and configuration.
func TemplateForHost(host, settingsPath, codexProvider, codexModel, opencodeModel string) (InvocationTemplate, error) {
	switch host {
	case HostClaude:
		if settingsPath == "" {
			return InvocationTemplate{}, errors.New("claude requires --claude-settings (aly_qwen_w settings path)")
		}
		return ClaudeTemplate(settingsPath), nil
	case HostCodex:
		if codexProvider == "" || codexModel == "" {
			return InvocationTemplate{}, errors.New("codex requires --codex-provider and --codex-model")
		}
		return CodexTemplate(codexProvider, codexModel), nil
	case HostOpenCode:
		if opencodeModel == "" {
			return InvocationTemplate{}, errors.New("opencode requires an explicit free --opencode-model")
		}
		return OpenCodeTemplate(opencodeModel), nil
	default:
		return InvocationTemplate{}, fmt.Errorf("unknown host %q", host)
	}
}

// ---------- capture / redaction ----------

var secretLikeRE = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|authorization|bearer)[^\n]{0,80}`)

// IsSecretLike reports whether a candidate provenance string looks like a
// credential (used on every free-form field before it may enter a receipt).
func IsSecretLike(s string) bool { return s != "" && secretLikeRE.MatchString(s) }

// SanitizeProvenanceString rejects secret-like free-form input (e.g. CLI
// version strings captured verbatim) before it can reach a receipt.
func SanitizeProvenanceString(field, s string) (string, error) {
	if IsSecretLike(s) {
		return "", fmt.Errorf("%s: value looks like a credential and is rejected", field)
	}
	return s, nil
}

// ValidateProvenance enforces the closed-field rules: known host, closed
// billing classes, unavailable semantics, and no secret-like free text.
func ValidateProvenance(p ToolProvenance) error {
	if !validHosts[p.Host] {
		return fmt.Errorf("host %q invalid", p.Host)
	}
	switch p.BillingClass {
	case BillingAuthorized, BillingFree, BillingUnknown:
	default:
		return fmt.Errorf("billing_class %q invalid", p.BillingClass)
	}
	if p.BillingClass == BillingFree && p.Host != HostOpenCode {
		return fmt.Errorf("billing_class=free is only valid for opencode, not %s", p.Host)
	}
	if p.Host == HostOpenCode {
		if p.BillingClass != BillingFree {
			return fmt.Errorf("opencode authoring/review/formal runs require billing_class=free")
		}
		if p.RequestedModel == nil || *p.RequestedModel == "" {
			return fmt.Errorf("opencode requires an explicit requested free model id")
		}
		if p.ResolvedModel == ResolvedUnavailable || p.ResolvedModel == "" {
			return fmt.Errorf("opencode requires a resolved model id; absent identity cannot be relabeled free")
		}
	}
	for field, s := range map[string]string{"cli_version": p.CLIVersion, "source_revision": p.SourceRevision} {
		if _, err := SanitizeProvenanceString(field, s); err != nil {
			return err
		}
	}
	return nil
}

// ---------- holdout batch model discipline ----------

// CheckBatchModelIdentity enforces the (2026-09-01 revised) seal-blocking
// invariant: across every author/reviewer attempt in the batch, each host
// resolves to exactly one non-unavailable model. Host harnesses must be the
// three distinct ones; the underlying model MAY repeat across hosts by
// explicit maintainer decision (unified Bailian qwen3.8-flash) — reviewer
// independence is carried by host harnesses plus the label-blind envelope,
// not by model diversity. A host name never substitutes for model identity.
func CheckBatchModelIdentity(provenances []ToolProvenance) error {
	perHost := map[string]string{}
	for _, p := range provenances {
		if !validHosts[p.Host] {
			return fmt.Errorf("attempt provenance has invalid host %q", p.Host)
		}
		if p.ResolvedModel == "" || p.ResolvedModel == ResolvedUnavailable {
			return fmt.Errorf("host %s resolved model unavailable in batch: batch cannot seal without waiver-free model identity", p.Host)
		}
		if prev, ok := perHost[p.Host]; ok {
			if prev != p.ResolvedModel {
				return fmt.Errorf("host %s model drifted within batch: %q vs %q", p.Host, prev, p.ResolvedModel)
			}
		} else {
			perHost[p.Host] = p.ResolvedModel
		}
	}
	if len(perHost) != 3 {
		return fmt.Errorf("batch must cover exactly three hosts, got %d", len(perHost))
	}
	for _, h := range []string{HostClaude, HostCodex, HostOpenCode} {
		if _, ok := perHost[h]; !ok {
			return fmt.Errorf("host %s missing from batch", h)
		}
	}
	return nil
}

// RequireDistinctReviewerHosts asserts author ≠ reviewers and reviewers differ.
func RequireDistinctReviewerHosts(author string, reviewers []string) error {
	seen := map[string]bool{author: true}
	for _, r := range reviewers {
		if seen[r] {
			return fmt.Errorf("reviewer host %q repeats the author or the other reviewer", r)
		}
		seen[r] = true
	}
	return nil
}

// UnsafePath reports any candidate filesystem path that escapes containment:
// absolute paths, `..`, NUL, backslashes, or symlink-attracting empties.
func UnsafePath(p string) bool { return !safeRelativePath(p) }

// initialCaps not used; keeps strings import meaningful if regex removed.
var _ = strings.TrimSpace
