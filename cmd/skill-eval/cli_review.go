package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Support layer for v2 command routing: flag helper, loaders, and the
// production three-CLI mirror-review driver.

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(flagOutput{})
	return fs
}

type flagOutput struct{}

func (flagOutput) Write(p []byte) (int, error) { return len(p), nil } // silence usage duplication

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// LoadDevFamilyIndex strictly parses a frozen index file.
func LoadDevFamilyIndex(path string) (*DevFamilyIndex, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx DevFamilyIndex
	if err := StrictParseClosed(b, &idx); err != nil {
		return nil, fmt.Errorf("family index %s: %w", path, err)
	}
	if idx.Algorithm != DevFamilyIndexAlgorithm {
		return nil, fmt.Errorf("family index algorithm %q != %s", idx.Algorithm, DevFamilyIndexAlgorithm)
	}
	return &idx, nil
}

// LoadPackageValidationReceipt strictly parses a package validation receipt.
func LoadPackageValidationReceipt(path string) (*SkillPackageValidationReceipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r SkillPackageValidationReceipt
	if err := StrictParseClosed(b, &r); err != nil {
		return nil, fmt.Errorf("package validation receipt %s: %w", path, err)
	}
	return &r, nil
}

// CLIReviewConfig carries the frozen three-lane CLI configuration.
type CLIReviewConfig struct {
	Lanes          []string
	ClaudeSettings string
	CodexProvider  string
	CodexModel     string
	OpenCodeModel  string
	PromptFile     string
}

// CLIMirrorReview builds the production MirrorReviewFunc. `content` supplies
// the pair's blind case projections (prompt/turns/seeds only — the frozen
// de-labeled projection, no labels/rules); each lane receives the frozen
// review prompt plus that materialized content in a fresh session and must
// answer strict JSON.
func CLIMirrorReview(cfg CLIReviewConfig, content MirrorContentProvider) MirrorReviewFunc {
	return func(pair MirrorPair, lane string) (bool, string, ToolProvenance, string, error) {
		prompt, err := buildMirrorReviewPrompt(cfg.PromptFile, pair, content)
		if err != nil {
			return false, "", ToolProvenance{}, "", err
		}
		// Transient host failures (CLI crash, lane daemon hiccup) retry with a
		// short backoff; the retried attempt is a full fresh invocation and
		// only its own output is used. Permanent failures still fail closed.
		var raw []byte
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * 2 * time.Second)
			}
			raw, lastErr = runLaneCLI(lane, cfg, prompt)
			if lastErr == nil {
				break
			}
		}
		if lastErr != nil {
			return false, "", ToolProvenance{}, "", lastErr
		}
		decisionBytes, err := extractDecisionJSON(raw)
		if err != nil {
			return false, "", ToolProvenance{}, "", fmt.Errorf("lane %s: %w", lane, err)
		}
		decision := struct {
			SameFamily bool   `json:"same_family"`
			Digest     string `json:"canonical_family_digest"`
		}{}
		if err := json.Unmarshal(decisionBytes, &decision); err != nil {
			return false, "", ToolProvenance{}, "", fmt.Errorf("lane %s decision unparseable: %w", lane, err)
		}
		prov := buildLaneProvenance(lane, cfg)
		return decision.SameFamily, decision.Digest, prov, sha256Hex(raw), nil
	}
}

// buildLaneProvenance assembles the full provenance record for one lane from
// structural facts only: the frozen template, the declared model (claude:
// settings `model` + its env default-model mapping; codex/opencode: the
// explicit config override we pass), and the CLI version probed once. No
// settings value besides model names is read.
func buildLaneProvenance(lane string, cfg CLIReviewConfig) ToolProvenance {
	tmpl, _ := TemplateForHost(lane, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel)
	tmplDigest, _ := tmpl.Digest()
	declared := laneDeclaredModel(lane, cfg)
	if declared == "" {
		declared = ResolvedUnavailable
	}
	prov := ToolProvenance{
		Host: lane, CLIVersion: probeCLIVersion(lane),
		InvocationTemplateID: tmpl.ID, InvocationTemplateDigest: tmplDigest,
		ResolvedModel: declared, BillingClass: BillingAuthorized,
		SourceRevision: "runner", CapturedAt: nowRFC3339(),
	}
	rm := declared
	prov.RequestedModel = &rm
	switch lane {
	case HostClaude:
		p := filepath.Base(cfg.ClaudeSettings)
		prov.RequestedProfile = &p
		sd := LFNormalizedSHA256Bytes([]byte(mustReadFile(cfg.ClaudeSettings)))
		prov.SettingsDigest = &sd
	case HostCodex:
		p := cfg.CodexProvider
		prov.RequestedProfile = &p
	}
	if d, err := ComputeToolIdentityDigest(prov); err == nil {
		prov.ToolIdentityDigest = d
	}
	return prov
}

// laneDeclaredModel resolves the model a lane will request, from
// configuration only. For claude the settings file may map a display model
// ("opus") onto a concrete backend via ANTHROPIC_DEFAULT_<UPPER>_MODEL.
func laneDeclaredModel(lane string, cfg CLIReviewConfig) string {
	switch lane {
	case HostClaude:
		if cfg.ClaudeSettings == "" {
			return ""
		}
		var s struct {
			Model string            `json:"model"`
			Env   map[string]string `json:"env"`
		}
		b, err := os.ReadFile(cfg.ClaudeSettings)
		if err != nil || json.Unmarshal(b, &s) != nil {
			return ""
		}
		if s.Model == "" {
			return ""
		}
		key := "ANTHROPIC_DEFAULT_" + strings.ToUpper(s.Model) + "_MODEL"
		if m := s.Env[key]; m != "" {
			return m
		}
		return s.Model
	case HostCodex:
		return cfg.CodexModel
	case HostOpenCode:
		return cfg.OpenCodeModel
	}
	return ""
}

var (
	cliVersionOnce   sync.Once
	cliVersionByHost map[string]string
)

// probeCLIVersion captures each host CLI's version string once per process.
// A probe failure leaves the field empty — never fabricated.
func probeCLIVersion(host string) string {
	cliVersionOnce.Do(func() {
		cliVersionByHost = map[string]string{}
		bins := map[string]string{HostClaude: "claude", HostCodex: "codex", HostOpenCode: "opencode2"}
		for h, bin := range bins {
			out := runAndCapture([]string{bin, "--version"})
			if out.exitCode != 0 {
				continue
			}
			v := strings.TrimSpace(string(out.stdout))
			if IsSecretLike(v) {
				continue
			}
			if i := strings.IndexByte(v, '\n'); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			cliVersionByHost[h] = v
		}
	})
	return cliVersionByHost[host]
}

// mustReadFile reads a file or returns empty content on error (digests of
// unreadable files surface as zero-value digests at the call site).
func mustReadFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// MirrorContentProvider renders the blind projection of one case for review.
type MirrorContentProvider func(caseID string) (string, error)

// buildMirrorReviewPrompt materializes the frozen prompt + the pair's blind
// projections. Case A/B IDs appear only as position labels.
func buildMirrorReviewPrompt(promptFile string, pair MirrorPair, content MirrorContentProvider) (string, error) {
	b, err := os.ReadFile(promptFile)
	if err != nil {
		return "", err
	}
	a, err := content(pair.A)
	if err != nil {
		return "", fmt.Errorf("case %s: %w", pair.A, err)
	}
	c, err := content(pair.B)
	if err != nil {
		return "", fmt.Errorf("case %s: %w", pair.B, err)
	}
	var sb strings.Builder
	sb.Write(b)
	sb.WriteString("\n\n=== CASE A ===\n")
	sb.WriteString(a)
	sb.WriteString("\n\n=== CASE B ===\n")
	sb.WriteString(c)
	sb.WriteString("\n\nRespond with exactly the JSON object.\n")
	return sb.String(), nil
}

// BlindCaseProjection renders the de-labeled review view of one case:
// prompt/turns and seed memory content only — no IDs, labels, rules, or
// provenance (mirrors the holdout blind-projection discipline).
func BlindCaseProjection(core *CoreDatasetV2) MirrorContentProvider {
	return func(caseID string) (string, error) {
		c, ok := core.Cases[caseID]
		if !ok {
			return "", fmt.Errorf("case %s not found", caseID)
		}
		var sb strings.Builder
		if c.Prompt != nil {
			sb.WriteString("prompt: " + *c.Prompt + "\n")
		}
		for _, t := range c.Turns {
			sb.WriteString(fmt.Sprintf("turn[s%d/%s]: %s\n", t.Session, t.Role, t.Content))
		}
		for _, s := range c.SeedMemories {
			sb.WriteString("memory: " + s.Name + " = " + s.Content + "\n")
		}
		return sb.String(), nil
	}
}

// runLaneCLI invokes one host CLI noninteractively with a prompt. The
// opencode lane runs inside a materialized lane workspace carrying its
// provider config (key flows through {env:MAAS_API_KEY}, never a file).
func runLaneCLI(lane string, cfg CLIReviewConfig, prompt string) ([]byte, error) {
	tmpl, err := TemplateForHost(lane, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel)
	if err != nil {
		return nil, err
	}
	var argv []string
	cwd := ""
	switch lane {
	case HostClaude:
		argv = append(append([]string{}, tmpl.Args...), prompt)
	case HostCodex:
		argv = append(append([]string{}, tmpl.Args...), prompt)
	case HostOpenCode:
		ws, err := materializeOpenCodeLaneWorkspace()
		if err != nil {
			return nil, err
		}
		cwd = ws
		argv = append(append([]string{}, tmpl.Args...), prompt, ws)
	default:
		return nil, fmt.Errorf("unknown lane %q", lane)
	}
	out := runAndCaptureIn(argv, cwd)
	if out.exitCode != 0 {
		return nil, fmt.Errorf("lane %s exited %d", lane, out.exitCode)
	}
	return out.stdout, nil
}

// materializeOpenCodeLaneWorkspace writes a minimal opencode project config
// for the review lane when a custom model is configured. The API key is
// referenced as {env:MAAS_API_KEY} — never written in plaintext.
func materializeOpenCodeLaneWorkspace() (string, error) {
	dir, err := os.MkdirTemp("", "skill-eval-opencode-lane-")
	if err != nil {
		return "", err
	}
	cfg := map[string]any{
		"$schema":     "https://opencode.ai/config.json",
		"autoupdate": false,
	}
	if model := os.Getenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL"); model != "" {
		prov, mid := model, model
		if i := strings.Index(model, "/"); i > 0 {
			prov, mid = model[:i], model[i+1:]
		}
		opts := map[string]any{"apiKey": "{env:MAAS_API_KEY}"}
		if b := os.Getenv("ENGRAM_SKILL_EVAL_OPENCODE_BASE"); b != "" {
			opts["baseURL"] = b
		}
		cfg["provider"] = map[string]any{
			prov: map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    prov,
				"options": opts,
				"models":  map[string]any{mid: map[string]any{"name": mid}},
			},
		}
		cfg["model"] = model
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), b, 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// extractDecisionJSON pulls the lane's final answer out of the three
// stream-json envelopes and returns the embedded decision object. A stream
// wrapper without a same_family decision is an error, never a silent false.
func extractDecisionJSON(raw []byte) ([]byte, error) {
	var candidates []string
	parseJSONLStream(bytes.NewReader(raw), func(obj map[string]any) {
		switch obj["type"] {
		case "result": // claude --output-format stream-json
			if r, ok := obj["result"].(string); ok {
				candidates = append(candidates, r)
			}
		case "item.completed": // codex exec --json
			item, _ := obj["item"].(map[string]any)
			if it, ok := item["type"].(string); ok && it == "agent_message" {
				if t, ok := item["text"].(string); ok {
					candidates = append(candidates, t)
				}
			}
		case "text": // opencode2 --format json
			part, _ := obj["part"].(map[string]any)
			if t, ok := part["text"].(string); ok {
				candidates = append(candidates, t)
			}
		}
	})
	// The final answer is the last candidate; earlier ones may be partial.
	for i := len(candidates) - 1; i >= 0; i-- {
		if d := firstObjectWithKey(candidates[i], "same_family"); d != nil {
			return d, nil
		}
	}
	if d := firstObjectWithKey(string(raw), "same_family"); d != nil {
		return d, nil
	}
	return nil, fmt.Errorf("lane output carries no same_family decision (raw %d bytes)", len(raw))
}

// firstObjectWithKey scans s for the first balanced JSON object that contains
// key at its top level.
func firstObjectWithKey(s, key string) []byte {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		depth := 0
		for j := i; j < len(s); j++ {
			switch s[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					obj := s[i : j+1]
					var probe map[string]json.RawMessage
					if err := json.Unmarshal([]byte(obj), &probe); err == nil {
						if _, ok := probe[key]; ok {
							return []byte(obj)
						}
					}
					i = j
					j = len(s)
				}
			}
		}
	}
	return nil
}
