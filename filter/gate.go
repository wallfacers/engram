package filter

import (
	"context"
	"reflect"
	"regexp"
)

// Write-gate decision routes. write and skip are produced by this iteration;
// defer_to_packet is a named placeholder so the future short-packet line can
// plug in without changing the vocabulary (and without conflicting with it).
const (
	RouteWrite         = "write"
	RouteSkip          = "skip"
	RouteDeferToPacket = "defer_to_packet"
	// RouteNoGate is the zero value: no decision was made (gate unavailable,
	// timed out, or failed). Callers keep their current write rules — the gate
	// never blocks a write just because it could not run.
	RouteNoGate = ""
)

// Reason keys in GateDecision.Reasons: the calibrated probabilities behind the
// route, passed through verbatim for audit.
const (
	ReasonDurable    = "is_durable"
	ReasonPreference = "is_preference"
	ReasonSecret     = "is_secret"
	ReasonNeedsProbe = "needs_probe"
)

// GateDecision is the write-gate verdict. Meta reuses FilterMeta for latency,
// cost and degradation telemetry; Kept/Dropped are retrieval concepts and stay
// unused here.
type GateDecision struct {
	Route   string
	Reasons map[string]float64
	Meta    FilterMeta
}

// GateRequest is the canonical write-gate input (data-model.md): the user turn,
// the draft about to be persisted, and the explicit-request flag from the
// memory_write contract (a user who asks for their own key to be stored
// overrides the secret verdict).
type GateRequest struct {
	UserTurn      string
	Draft         string
	UserRequested bool
}

// WriteGate decides whether a draft memory should be persisted. Gate is the
// shorthand (UserRequested=false); GateWithOptions carries the full canonical
// request. Implementations must fail open: on error the caller treats the turn as
// ungated.
type WriteGate interface {
	Gate(ctx context.Context, userTurn, draft string) (GateDecision, error)
	GateWithOptions(ctx context.Context, req GateRequest) (GateDecision, error)
}

// IsNilWriteGate reports whether gate carries no gate at all, collapsing a
// concrete nil (an unconfigured client) to "no gate" at the adapter boundary.
// Without it, a typed-nil gate would be non-nil as an interface and a write
// would dereference a nil receiver instead of failing open.
func IsNilWriteGate(gate WriteGate) bool {
	if gate == nil {
		return true
	}
	value := reflect.ValueOf(gate)
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan:
		return value.IsNil()
	}
	return false
}

type secretPattern struct {
	name  string
	re    *regexp.Regexp
	group int // 0 masks the whole match, n>0 masks only that capture group
}

// secretPatterns are the local "looks like a credential" shapes. They drive three
// boundary rules: a secret-shaped draft never leaves the machine, any text that
// does go to a remote model is masked first (both the write-gate draft and the
// stored memory text the read-side filter sends), and the detector is the single
// spelling of "what looks like a credential". Accepted residual: a credential
// format this list does not know (a new provider prefix, an opaque blob with no
// recognisable shape, a secret split across fields) is not detected — the list
// covers the formats the design named, and MaskSecrets is the last line of
// defence rather than a proof.
var secretPatterns = []secretPattern{
	{name: "openai-style-key", re: regexp.MustCompile(`\bsk-[A-Za-z0-9_\-]{8,}`)},
	{name: "openai-style-key-underscore", re: regexp.MustCompile(`\bsk_[A-Za-z0-9]{8,}`)},
	{name: "huggingface-token", re: regexp.MustCompile(`\bhf_[A-Za-z0-9]{8,}`)},
	{name: "github-token", re: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{16,}`)},
	{name: "github-fine-grained-pat", re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{16,}`)},
	{name: "aws-access-key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{12,}`)},
	{name: "google-api-key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{16,}`)},
	{name: "slack-token", re: regexp.MustCompile(`\bxox[baprsEe]-[A-Za-z0-9\-]{10,}`)},
	{name: "gitlab-token", re: regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{10,}`)},
	{name: "jwt", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`)},
	{name: "private-key-block", re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{name: "bearer-token", re: regexp.MustCompile(`(?i)\bBearer\s+([A-Za-z0-9._\-]{12,})`), group: 1},
	{name: "credential-assignment", re: regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:api[_-]?key|private[_-]?key(?:_id)?|access[_-]?token|auth[_-]?token|password|passwd|secret|token))\b\s*[:=]\s*["']?([A-Za-z0-9._\-]{8,})`), group: 2},
}

// LooksLikeSecret reports whether text contains a credential shape that must not
// leave the machine. It is deliberately conservative: it matches key formats and
// name=value credential assignments, not the words alone.
func LooksLikeSecret(text string) bool {
	for _, p := range secretPatterns {
		if p.re.MatchString(text) {
			return true
		}
	}
	return false
}

// MaskSecrets replaces credential shapes with "[REDACTED]", keeping the
// surrounding text (and the credential's name, for assignments and Bearer
// schemes) readable. It is the last line of defence before any text is sent to a
// remote model.
func MaskSecrets(text string) string {
	out := text
	for _, p := range secretPatterns {
		out = p.mask(out)
	}
	return out
}

func (p secretPattern) mask(text string) string {
	return p.re.ReplaceAllStringFunc(text, func(m string) string {
		if p.group == 0 {
			return "[REDACTED]"
		}
		loc := p.re.FindStringSubmatchIndex(m)
		if len(loc) <= 2*p.group+1 || loc[2*p.group] < 0 {
			return m
		}
		return m[:loc[2*p.group]] + "[REDACTED]" + m[loc[2*p.group+1]:]
	})
}
