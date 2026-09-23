package filter

import (
	"context"
	"strings"
	"testing"
)

func TestRouteVocabulary(t *testing.T) {
	if RouteWrite != "write" || RouteSkip != "skip" || RouteDeferToPacket != "defer_to_packet" {
		t.Fatalf("route vocabulary drifted: %q %q %q", RouteWrite, RouteSkip, RouteDeferToPacket)
	}
	if RouteNoGate != "" {
		t.Fatalf("the no-gate (fail-open) route must stay the empty string, got %q", RouteNoGate)
	}
}

func TestReasonKeys(t *testing.T) {
	if ReasonDurable != "is_durable" || ReasonPreference != "is_preference" ||
		ReasonSecret != "is_secret" || ReasonNeedsProbe != "needs_probe" {
		t.Fatalf("reason keys drifted from the contract: %q %q %q %q",
			ReasonDurable, ReasonPreference, ReasonSecret, ReasonNeedsProbe)
	}
}

func TestLooksLikeSecretDetectsApiKeyShapes(t *testing.T) {
	cases := map[string]string{
		"openai-style":  "here is my key sk-abcdefgh12345678",
		"underscore":    "auth sk_abcdefgh12345678",
		"huggingface":   "token hf_abcdefghijklmnop",
		"github-pat":    "ghp_abcdefghijklmnopqrstuvwxyz012345",
		"github-fine":   "github_pat_11ABCDEFG0abcdefghijklmnop",
		"github-oauth":  "gho_abcdefghijklmnopqrstuv",
		"aws":           "AKIAIOSFODNN7EXAMPLE",
		"aws-temp":      "ASIAIOSFODNN7EXAMPLE",
		"google":        "AIzaSyA1234567890abcdefghijklmnopqrs",
		"slack":         "xoxb-1234567890-abcdefghij",
		"slack-enterprise": "xoxe-1234567890-abcdefghij",
		"gitlab":        "glpat-abcdefghij12345",
		"bearer":        "Authorization: Bearer abcdefghijklmnop",
		"jwt":           "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijklmnop",
		"private-key":   "-----BEGIN RSA PRIVATE KEY-----",
		"private-key-json": `"private_key": "-----BEGIN PRIVATE KEY-----"`,
		"private-key-id": "private_key_id = abcdefgh12345678",
		"apikey-assign": "DEEPSEEK_API_KEY=abcdefgh12345678",
		"token-assign":  "my_token: abcdefgh12345678",
		"pwd-assign":    `password = "correct-horse-battery"`,
	}
	for name, text := range cases {
		if !LooksLikeSecret(text) {
			t.Errorf("%s: expected secret shape in %q", name, text)
		}
		if masked := MaskSecrets(text); strings.Contains(masked, "abcdefgh") || strings.Contains(masked, "IOSFODNN7EXAMPLE") {
			t.Errorf("%s: masking left the secret in %q", name, masked)
		}
	}
}

func TestLooksLikeSecretIgnoresOrdinaryText(t *testing.T) {
	for _, text := range []string{
		"the secret sauce is patience",
		"sk-",
		"my monkey ate the key",
		"the token budget is 4000",
		"install pnpm with corepack",
		"this password policy is strict",
	} {
		if LooksLikeSecret(text) {
			t.Errorf("false positive for %q", text)
		}
	}
}

func TestMaskSecretsKeepsReadableStructure(t *testing.T) {
	if got := MaskSecrets("api_key=abcdefgh12345"); got != "api_key=[REDACTED]" {
		t.Fatalf("expected the key name to stay readable, got %q", got)
	}
	if got := MaskSecrets("Authorization: Bearer abcdefghijklmnop"); got != "Authorization: Bearer [REDACTED]" {
		t.Fatalf("expected the Bearer scheme to stay readable, got %q", got)
	}
	if got := MaskSecrets("k=sk-abcdefgh12345678 done"); got != "k=[REDACTED] done" {
		t.Fatalf("expected the whole key shape to be redacted, got %q", got)
	}
	if got := MaskSecrets("no secrets here at all"); got != "no secrets here at all" {
		t.Fatalf("ordinary text must be untouched, got %q", got)
	}
}

func TestWriteGateZeroDecisionMeansNoGate(t *testing.T) {
	// Fail-open is signalled by RouteNoGate (the zero value) plus a non-nil error:
	// callers keep their current write rules in that case.
	var d GateDecision
	if d.Route != RouteNoGate {
		t.Fatalf("zero decision must mean no gate, got %q", d.Route)
	}
	var _ WriteGate = (*stubGate)(nil)
}

type stubGate struct{}

func (stubGate) Gate(_ context.Context, _, _ string) (GateDecision, error) {
	return GateDecision{}, nil
}

func (stubGate) GateWithOptions(_ context.Context, _ GateRequest) (GateDecision, error) {
	return GateDecision{}, nil
}
