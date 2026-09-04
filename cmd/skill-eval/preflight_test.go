package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T043 preflight/parity: the frozen three-host argv/profile contract, the
// codex cwd parity, and the exact-child probe + workspace-canary invalidation
// rules. Everything here is offline: stub binaries on a private PATH and
// t.TempDir trees, never a real claude/codex/opencode invocation.

const t043Settings = "/home/u/.claude/settings.json.aly_qwen_w"

func t043Config() CLIReviewConfig {
	return CLIReviewConfig{
		ClaudeSettings: t043Settings,
		CodexProvider:  "aq",
		CodexModel:     "qwen3.8-flash",
		OpenCodeModel:  "free-provider/free-model",
	}
}

// ---------- argv / profile templates ----------

// TestArgvProfileBindingPerHost pins the exact frozen argv prefix per host and
// the profile each lane binds: claude pins the maintainer settings path, codex
// pins the -c provider/model overrides, opencode pins the explicit free model.
func TestArgvProfileBindingPerHost(t *testing.T) {
	cfg := t043Config()
	wantArgs := map[string][]string{
		HostClaude: {"claude", "--settings", cfg.ClaudeSettings, "--output-format", "stream-json", "--verbose", "-p"},
		HostCodex:  {"codex", "-c", "model_provider=aq", "-c", "model=qwen3.8-flash", "--yolo", "exec", "--json"},
		HostOpenCode: {"opencode2", "run", "--standalone", "--auto", "--format", "json",
			"--model", "free-provider/free-model"},
	}
	templates := map[string]InvocationTemplate{}
	for _, host := range []string{HostClaude, HostCodex, HostOpenCode} {
		tmpl, err := TemplateForHost(host, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if tmpl.ID == "" {
			t.Errorf("%s: template id empty", host)
		}
		if strings.Join(tmpl.Args, "\x00") != strings.Join(wantArgs[host], "\x00") {
			t.Errorf("%s argv drifted:\n got %v\nwant %v", host, tmpl.Args, wantArgs[host])
		}
		templates[host] = tmpl
	}
	// Profile binding is carried by the frozen argv itself.
	if filepath.Base(templates[HostClaude].Args[2]) != filepath.Base(cfg.ClaudeSettings) {
		t.Errorf("claude profile = %s, want the aly_qwen_w settings basename", templates[HostClaude].Args[2])
	}
	cx := templates[HostCodex].Args
	if cx[2] != "model_provider="+cfg.CodexProvider || cx[4] != "model="+cfg.CodexModel {
		t.Errorf("codex profile = %v, want provider %q model %q", cx[2:5], cfg.CodexProvider, cfg.CodexModel)
	}
	oc := templates[HostOpenCode].Args
	if oc[len(oc)-2] != "--model" || oc[len(oc)-1] != cfg.OpenCodeModel {
		t.Errorf("opencode profile = %v, want the explicit free model %q", oc[len(oc)-2:], cfg.OpenCodeModel)
	}
	// An unknown host is a construction error, never a silent fallback.
	if _, err := TemplateForHost("cursor", cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel); err == nil {
		t.Error("unknown host must fail template construction")
	}
}

// t043TemplateDigest builds a host template and returns its digest, failing
// the test on any construction or digest error.
func t043TemplateDigest(t *testing.T, host, settings, provider, model, ocModel string) string {
	t.Helper()
	tmpl, err := TemplateForHost(host, settings, provider, model, ocModel)
	if err != nil {
		t.Fatalf("%s: %v", host, err)
	}
	d, err := tmpl.Digest()
	if err != nil {
		t.Fatalf("%s digest: %v", host, err)
	}
	return d
}

// TestTemplateDigestStabilityAndDrift proves the template digest is a usable
// freeze marker: identical configuration digests identically, any change to
// the bound configuration or to the argv itself drifts it.
func TestTemplateDigestStabilityAndDrift(t *testing.T) {
	cfg := t043Config()
	digest := map[string]string{}
	for _, host := range []string{HostClaude, HostCodex, HostOpenCode} {
		d1 := t043TemplateDigest(t, host, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel)
		if len(d1) != 64 || strings.ToLower(d1) != d1 {
			t.Errorf("%s digest %q is not 64 lowercase hex", host, d1)
		}
		if again := t043TemplateDigest(t, host, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel); again != d1 {
			t.Errorf("%s digest unstable: %s vs %s", host, d1, again)
		}
		digest[host] = d1
	}
	if digest[HostClaude] == digest[HostCodex] || digest[HostCodex] == digest[HostOpenCode] {
		t.Error("distinct hosts must not share a template digest")
	}
	// Configuration drift: every profile input feeds the digest.
	if d := t043TemplateDigest(t, HostClaude, "/home/u/.claude/settings.json.other", cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel); d == digest[HostClaude] {
		t.Error("claude settings change must drift the digest")
	}
	if d := t043TemplateDigest(t, HostCodex, cfg.ClaudeSettings, "other", cfg.CodexModel, cfg.OpenCodeModel); d == digest[HostCodex] {
		t.Error("codex provider change must drift the digest")
	}
	if d := t043TemplateDigest(t, HostCodex, cfg.ClaudeSettings, cfg.CodexProvider, "other-model", cfg.OpenCodeModel); d == digest[HostCodex] {
		t.Error("codex model change must drift the digest")
	}
	if d := t043TemplateDigest(t, HostOpenCode, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, "other/free"); d == digest[HostOpenCode] {
		t.Error("opencode model change must drift the digest")
	}
	// Structural drift: editing the frozen argv changes the digest.
	tmpl, err := TemplateForHost(HostCodex, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.Args[3] = "model_provider=spoofed"
	if d, _ := tmpl.Digest(); d == digest[HostCodex] {
		t.Error("edited argv must drift the template digest")
	}
}

// TestPreflightLaneDeclaredModelProfileBinding checks the declared-model half
// of the claude profile: a settings display model resolves through its env
// default-model mapping, configuration only, never by probing a CLI.
func TestPreflightLaneDeclaredModelProfileBinding(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mapped := write("mapped.json", `{"model":"opus","env":{"ANTHROPIC_DEFAULT_OPUS_MODEL":"qwen3.8-flash"}}`)
	plain := write("plain.json", `{"model":"sonnet"}`)
	noModel := write("nomodel.json", `{"env":{"ANTHROPIC_DEFAULT_OPUS_MODEL":"qwen3.8-flash"}}`)
	cfg := t043Config()
	if got := laneDeclaredModel(HostClaude, CLIReviewConfig{ClaudeSettings: mapped}); got != "qwen3.8-flash" {
		t.Errorf("mapped claude model = %q, want qwen3.8-flash", got)
	}
	if got := laneDeclaredModel(HostClaude, CLIReviewConfig{ClaudeSettings: plain}); got != "sonnet" {
		t.Errorf("unmapped claude model = %q, want sonnet", got)
	}
	if got := laneDeclaredModel(HostClaude, CLIReviewConfig{ClaudeSettings: noModel}); got != "" {
		t.Errorf("model-less settings = %q, want empty", got)
	}
	if got := laneDeclaredModel(HostClaude, CLIReviewConfig{ClaudeSettings: filepath.Join(dir, "absent.json")}); got != "" {
		t.Errorf("unreadable settings = %q, want empty", got)
	}
	if got := laneDeclaredModel(HostCodex, cfg); got != cfg.CodexModel {
		t.Errorf("codex declared model = %q, want %q", got, cfg.CodexModel)
	}
	if got := laneDeclaredModel(HostOpenCode, cfg); got != cfg.OpenCodeModel {
		t.Errorf("opencode declared model = %q, want %q", got, cfg.OpenCodeModel)
	}
}

// ---------- codex cwd parity ----------

// t043RecordingStub installs a fake host binary that records its cwd, argv and
// environment, then returns the artifact paths. Only fakes run here.
func t043RecordingStub(t *testing.T, binName string) (cwdOut, argvOut, envOut string) {
	t.Helper()
	bin := t.TempDir()
	cwdOut = filepath.Join(bin, binName+".cwd")
	argvOut = filepath.Join(bin, binName+".argv")
	envOut = filepath.Join(bin, binName+".env")
	script := "#!/bin/sh\n" +
		"pwd > " + cwdOut + "\n" +
		"pwd\n" + // the child's own view of its cwd, on stdout
		"printf '%s\\n' \"$@\" > " + argvOut + "\n" +
		"env > " + envOut + "\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(bin, binName), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return cwdOut, argvOut, envOut
}

func t043ReadLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestCodexCwdParityCmdDirIsWorkspace proves the codex lane's workspace
// parity: `codex exec` is launched with cmd.Dir pinned to the exact child
// workspace (the cmd.Dir equivalent of `codex exec -C <workspace>`), with the
// frozen argv plus the prompt as the final argument, and no -C flag baked into
// the template — with no override the child simply inherits the controller cwd.
func TestCodexCwdParityCmdDirIsWorkspace(t *testing.T) {
	ws := t.TempDir()
	_, argvOut, _ := t043RecordingStub(t, "codex")
	cfg := t043Config()
	out, err := runLaneCLIIn(HostCodex, cfg, "review prompt", ws)
	if err != nil {
		t.Fatalf("codex lane: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != ws {
		t.Fatalf("codex child ran in %q, want the exact child workspace %q", got, ws)
	}
	// "$@" excludes argv[0], so the recorded arguments start after the binary.
	want := append(append([]string{}, CodexTemplate(cfg.CodexProvider, cfg.CodexModel).Args[1:]...), "review prompt")
	if got := t043ReadLines(t, argvOut); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("codex argv = %v, want %v", got, want)
	}
	// Parity comes from cmd.Dir, not from a flag in the frozen template.
	inherit, err := runLaneCLIIn(HostCodex, cfg, "second prompt", "")
	if err != nil {
		t.Fatalf("codex lane without override: %v", err)
	}
	controllerCWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(inherit)); got != controllerCWD {
		t.Errorf("codex child without override ran in %q, want the inherited controller cwd %q", got, controllerCWD)
	}
}

// TestCodexCwdOpencodeLaneStaysInConfigWorkspace pins the opencode lane's
// cwd contract after the config-link rework: the child runs in the prepared
// attempt workspace (its cwd is observable — the canary/case contract), and
// the lane's provider config is linked INTO that workspace so provider
// resolution still finds it (bisected 2026-09-02: opencode2 reads its config
// from its cwd). The inherited ANTHROPIC_* markers are still stripped by the
// whitelist env.
func TestCodexCwdOpencodeLaneStaysInConfigWorkspace(t *testing.T) {
	ws := t.TempDir()
	_, argvOut, envOut := t043RecordingStub(t, "opencode2")
	t.Setenv("ANTHROPIC_API_KEY", "poison-key")
	t.Setenv("ENGRAM_SKILL_EVAL_OPENCODE_MODEL", "maas/deepseek-v4-flash-0731")
	out, err := runLaneCLIIn(HostOpenCode, t043Config(), "author prompt", ws)
	if err != nil {
		t.Fatalf("opencode lane: %v", err)
	}
	cwd := strings.TrimSpace(string(out))
	if cwd != ws {
		t.Fatalf("opencode lane must run in the prepared workspace %q, ran in %q", ws, cwd)
	}
	if fi, err := os.Lstat(filepath.Join(ws, "opencode.json")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the lane config must be linked into the prepared workspace: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "opencode.json")); err != nil || !strings.Contains(string(b), "deepseek-v4-flash-0731") {
		t.Fatalf("the linked config must carry the lane model: %v", err)
	}
	args := t043ReadLines(t, argvOut)
	if strings.Contains(strings.Join(args, "\x00"), "skill-eval-opencode-lane-") {
		t.Errorf("the config scratch dir must not reach the opencode child: %v", args)
	}
	if got := strings.Join(t043ReadLines(t, envOut), "\n"); strings.Contains(got, "ANTHROPIC_API_KEY") {
		t.Error("opencode lane env whitelist leaked ANTHROPIC_API_KEY into the child")
	}
}

// ---------- exact-child probe matrix on the real filesystem ----------

// t043ProtectedTree materializes the primary-stage access landscape one exact
// child faces: its own readable workspace plus every forbidden neighbour —
// protected root, audit trail, author state, prior-case state, retired
// workspace and a concurrently active sibling workspace. Forbidden targets
// carry mode 000 so even the owning user is denied; they are restored so
// t.TempDir can clean up.
func t043ProtectedTree(t *testing.T) (root string, targets map[FormalProbeKind]string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission-bit denial is meaningless when running as root")
	}
	root = t.TempDir()
	mkdir := func(rel string) string {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	write := func(dir, name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var locked []string
	lock := func(p string) {
		if err := os.Chmod(p, 0o000); err != nil {
			t.Fatal(err)
		}
		locked = append(locked, p)
	}
	t.Cleanup(func() {
		for _, p := range locked {
			_ = os.Chmod(p, 0o700)
		}
	})

	own := mkdir("workspaces/claude-slot-1")
	sibling := mkdir("workspaces/claude-slot-2")
	protected := mkdir("protected")
	audit := mkdir("audit")
	authorState := mkdir("author-state")
	priorState := mkdir("prior-case-state")
	retired := mkdir("retired/case-041")

	write(own, "envelope.json", `{"case":"own"}`)
	siblingInput := write(sibling, "envelope.json", `{"case":"sibling"}`)
	auditTrail := write(audit, "trail.jsonl", "audit\n")
	authorFile := write(authorState, "state.json", `{"split":"core172"}`)
	priorFile := write(priorState, "state.json", `{"case":"040"}`)
	write(retired, "state.json", `{"case":"041"}`)

	lock(protected) // root itself: traverse and list are both denials
	lock(siblingInput)
	lock(auditTrail)
	lock(authorFile)
	lock(priorFile)
	lock(retired)

	targets = map[FormalProbeKind]string{
		FProbeProtectedRootTraverse: protected,
		FProbeProtectedRootList:     protected,
		FProbeProtectedRootRead:     filepath.Join(protected, "seed.json"),
		FProbeAuditRead:             auditTrail,
		FProbeAuthorStateRead:       authorFile,
		FProbePriorCaseStateRead:    priorFile,
		FProbeRetiredWorkspaceRead:  retired,
		FProbeActiveSiblingRead:     siblingInput,
		FProbeOwnWorkspaceRead:      filepath.Join(own, "envelope.json"),
	}
	return root, targets
}

// t043TargetDigest digests a probe target the way the controller records it:
// content digest for a readable file, path-derived digest for a target the
// child must never read. Digests only — never a raw path.
func t043TargetDigest(target string) string {
	if b, err := os.ReadFile(target); err == nil {
		return LFNormalizedSHA256Bytes(b)
	}
	return sha256Hex([]byte("unreachable:" + target))
}

func t043Outcome(observed string) string {
	switch observed {
	case ProbeReadable:
		return "readable"
	case ProbeNotFound:
		return "not-found"
	default:
		return "permission-denied"
	}
}

// t043ProbeFromObservations runs the real filesystem probe per kind and turns
// the observations into the formal record the controller seals, with the
// controller target-existence proof attached to every not-found denial.
func t043ProbeFromObservations(host string, slot int, targets map[FormalProbeKind]string) ProtectedWorkerProbe {
	tmpl, err := TemplateForHost(host, t043Settings, "aq", "qwen3.8-flash", "free-provider/free-model")
	if err != nil {
		panic(err)
	}
	tmplDigest, _ := tmpl.Digest()
	probes := make([]FormalAccessProbe, 0, len(targets))
	for kind, target := range targets {
		pr := FormalAccessProbe{
			Kind:                     kind,
			TargetDigest:             t043TargetDigest(target),
			TargetAccessPolicyDigest: sha256Hex([]byte(filepath.Dir(target))),
			Expected:                 "denied",
			Outcome:                  t043Outcome(ProbeFilesystem(target)),
		}
		if kind == FProbeOwnWorkspaceRead {
			pr.Expected = "readable"
		}
		if pr.Outcome == "not-found" {
			pr.ControllerTargetProofDigest = sha256Hex([]byte("controller-proof:" + target))
		}
		probes = append(probes, pr)
	}
	return ProtectedWorkerProbe{
		Host:                    host,
		WorkerSlot:              slot,
		ChildIdentityDigest:     sha256Hex([]byte(host + "\x00" + fmt.Sprint(slot))),
		ExecutionTemplateDigest: tmplDigest,
		AccessBoundaryDigest:    sha256Hex([]byte("boundary:" + host)),
		Probes:                  probes,
	}
}

// TestProbeMatrixRealTreeDeniesForbiddenRoots is the real-filesystem half of
// the preflight: every forbidden neighbour is denied, the child's own
// workspace (file and directory) reads, and a never-materialized target is
// not-found rather than silently denied.
func TestProbeMatrixRealTreeDeniesForbiddenRoots(t *testing.T) {
	_, targets := t043ProtectedTree(t)
	for kind, target := range targets {
		want := ProbeDenied
		if kind == FProbeOwnWorkspaceRead {
			want = ProbeReadable
		}
		if got := ProbeFilesystem(target); got != want {
			t.Errorf("probe %s on %s observed %q, want %q", kind, target, got, want)
		}
	}
	ownDir := filepath.Dir(targets[FProbeOwnWorkspaceRead])
	if got := ProbeFilesystem(ownDir); got != ProbeReadable {
		t.Errorf("own workspace directory observed %q, want readable", got)
	}
	absent := filepath.Join(filepath.Dir(ownDir), "never-materialized")
	if got := ProbeFilesystem(absent); got != ProbeNotFound {
		t.Errorf("missing target observed %q, want not-found", got)
	}
}

// TestProbeMatrixNineKindValidationFromRealObservations seals the observed
// real-filesystem outcomes into the formal nine-kind matrix for every host ×
// worker slot and validates it — own workspace readable, everything else
// denied. It also accepts the fresh-run variant where the prior-case and
// retired targets do not exist yet, but only behind a controller proof.
func TestProbeMatrixNineKindValidationFromRealObservations(t *testing.T) {
	root, targets := t043ProtectedTree(t)
	fresh := map[FormalProbeKind]string{}
	for k, v := range targets {
		fresh[k] = v
	}
	// On a fresh run these neighbours were never materialized.
	fresh[FProbePriorCaseStateRead] = filepath.Join(root, "prior-case-state", "case-042", "state.json")
	fresh[FProbeRetiredWorkspaceRead] = filepath.Join(root, "retired", "case-042")

	for _, host := range []string{HostClaude, HostCodex, HostOpenCode} {
		for slot := 1; slot <= 3; slot++ {
			p := t043ProbeFromObservations(host, slot, targets)
			if got := len(p.Probes); got != 9 {
				t.Fatalf("%s slot %d: matrix holds %d probes, want 9", host, slot, got)
			}
			if err := ValidateWorkerProbe(p, nil); err != nil {
				t.Fatalf("%s slot %d: %v", host, slot, err)
			}
			if err := ValidateWorkerProbe(t043ProbeFromObservations(host, slot, fresh), nil); err != nil {
				t.Fatalf("%s slot %d fresh run: %v", host, slot, err)
			}
		}
	}
	// The same fresh-run matrix without controller proofs is not a denial.
	noProof := t043ProbeFromObservations(HostClaude, 1, fresh)
	for i := range noProof.Probes {
		noProof.Probes[i].ControllerTargetProofDigest = ""
	}
	if err := ValidateWorkerProbe(noProof, nil); err == nil || !strings.Contains(err.Error(), "not-found without controller proof") {
		t.Fatalf("unproven not-found denial accepted: %v", err)
	}
}

// TestProbeMatrixInvalidationPaths enumerates the closed invalidation rules of
// a formal child probe matrix: completeness per kind, the readable-only
// own-workspace exception, denial outcomes, controller proofs and child
// identity/boundary presence.
func TestProbeMatrixInvalidationPaths(t *testing.T) {
	_, targets := t043ProtectedTree(t)
	base := t043ProbeFromObservations(HostClaude, 2, targets)
	if err := ValidateWorkerProbe(base, nil); err != nil {
		t.Fatalf("baseline matrix must validate: %v", err)
	}
	clone := func() ProtectedWorkerProbe {
		p := base
		p.Probes = append([]FormalAccessProbe(nil), base.Probes...)
		return p
	}
	find := func(kind FormalProbeKind) int {
		for i, pr := range base.Probes {
			if pr.Kind == kind {
				return i
			}
		}
		return -1
	}
	cases := []struct {
		name   string
		want   string
		mutate func(*ProtectedWorkerProbe)
	}{
		{"own-workspace read removed", "own-workspace-read appears 0 times", func(p *ProtectedWorkerProbe) {
			kept := p.Probes[:0]
			for _, pr := range p.Probes {
				if pr.Kind != FProbeOwnWorkspaceRead {
					kept = append(kept, pr)
				}
			}
			p.Probes = kept
		}},
		{"protected-root read duplicated", "protected-root-read appears 2 times, want exactly 1", func(p *ProtectedWorkerProbe) {
			i := find(FProbeProtectedRootRead)
			p.Probes = append(p.Probes, p.Probes[i])
		}},
		{"sibling workspace expected readable", "must not expect readable", func(p *ProtectedWorkerProbe) {
			p.Probes[find(FProbeActiveSiblingRead)].Expected = "readable"
		}},
		{"own workspace not readable", "own-workspace probe on claude/2 observed", func(p *ProtectedWorkerProbe) {
			i := find(FProbeOwnWorkspaceRead)
			p.Probes[i].Outcome = "permission-denied"
		}},
		{"forbidden target observed readable", "forbidden outcome", func(p *ProtectedWorkerProbe) {
			p.Probes[find(FProbeActiveSiblingRead)].Outcome = "readable"
		}},
		{"not-found without controller proof", "not-found without controller proof", func(p *ProtectedWorkerProbe) {
			i := find(FProbeActiveSiblingRead)
			p.Probes[i].Outcome = "not-found"
			p.Probes[i].ControllerTargetProofDigest = ""
		}},
		{"child identity missing", "identity incomplete", func(p *ProtectedWorkerProbe) {
			p.ChildIdentityDigest = ""
		}},
		{"access boundary missing", "identity incomplete", func(p *ProtectedWorkerProbe) {
			p.AccessBoundaryDigest = ""
		}},
		{"worker slot zero", "identity incomplete", func(p *ProtectedWorkerProbe) {
			p.WorkerSlot = 0
		}},
		{"target digest missing", "lacks target/policy digests", func(p *ProtectedWorkerProbe) {
			p.Probes[find(FProbeAuditRead)].TargetDigest = ""
		}},
		{"access policy digest missing", "lacks target/policy digests", func(p *ProtectedWorkerProbe) {
			p.Probes[find(FProbeAuditRead)].TargetAccessPolicyDigest = ""
		}},
		{"unknown expectation", "unknown expectation", func(p *ProtectedWorkerProbe) {
			p.Probes[find(FProbeAuditRead)].Expected = "maybe"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := clone()
			tc.mutate(&p)
			err := ValidateWorkerProbe(p, nil)
			if err == nil {
				t.Fatalf("invalid matrix accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not report %q", err, tc.want)
			}
		})
	}
}

// ---------- workspace canaries ----------

func t043Canary() WorkspaceCanaryReceipt {
	return WorkspaceCanaryReceipt{
		SeriesID:                "series-048",
		Host:                    HostClaude,
		SkillDigest:             "skill-digest",
		ToolIdentityDigest:      "tool-identity-digest",
		ExecutionTemplateDigest: "template-digest",
		WorkerSlot:              2,
		ChildIdentityDigest:     "child-identity-digest",
		AccessBoundaryDigest:    "access-boundary-digest",
		CanaryWorkspaceDigest:   "workspace-digest",
		ExpectedFileDigest:      "canary-file-digest",
		ObservedCWDDigest:       "workspace-digest",
		ObservedFileDigest:      "canary-file-digest",
		Status:                  "pass",
		ReceiptDigest:           "receipt-digest",
	}
}

// TestCanaryValidateRejectionPaths enumerates every rejection path of the
// workspace canary: identity mismatch against the frozen series/skill/tool
// identity/template, slot mismatch, observation mismatch, failed status and an
// unsealed receipt. Any single mismatch invalidates.
func TestCanaryValidateRejectionPaths(t *testing.T) {
	const (
		series = "series-048"
		skill  = "skill-digest"
		tool   = "tool-identity-digest"
		tmpl   = "template-digest"
		slot   = 2
	)
	if err := ValidateWorkspaceCanary(nil, series, skill, tool, tmpl, slot); err == nil {
		t.Fatal("nil canary accepted")
	}
	if err := ValidateWorkspaceCanary(&WorkspaceCanaryReceipt{}, series, skill, tool, tmpl, slot); err == nil ||
		!strings.Contains(err.Error(), "canary series") {
		t.Fatalf("empty canary must fail on series identity, got %v", err)
	}
	cases := []struct {
		name   string
		want   string
		mutate func(*WorkspaceCanaryReceipt)
	}{
		{"series mismatch", "canary series", func(c *WorkspaceCanaryReceipt) { c.SeriesID = "series-047" }},
		{"skill digest drift", "canary skill digest drift", func(c *WorkspaceCanaryReceipt) { c.SkillDigest = "other" }},
		{"tool identity drift", "canary tool identity drift", func(c *WorkspaceCanaryReceipt) { c.ToolIdentityDigest = "other" }},
		{"template drift", "canary execution template drift", func(c *WorkspaceCanaryReceipt) { c.ExecutionTemplateDigest = "other" }},
		{"slot mismatch", "canary slot 3, want 2", func(c *WorkspaceCanaryReceipt) { c.WorkerSlot = 3 }},
		{"child identity missing", "canary identity incomplete", func(c *WorkspaceCanaryReceipt) { c.ChildIdentityDigest = "" }},
		{"access boundary missing", "canary identity incomplete", func(c *WorkspaceCanaryReceipt) { c.AccessBoundaryDigest = "" }},
		{"cwd observation mismatch", "canary observation mismatch", func(c *WorkspaceCanaryReceipt) { c.ObservedCWDDigest = "other" }},
		{"file observation mismatch", "canary observation mismatch", func(c *WorkspaceCanaryReceipt) { c.ObservedFileDigest = "other" }},
		{"failed status", `canary status "fail"`, func(c *WorkspaceCanaryReceipt) { c.Status = "fail" }},
		{"receipt digest empty", "canary receipt digest empty", func(c *WorkspaceCanaryReceipt) { c.ReceiptDigest = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := t043Canary()
			tc.mutate(&c)
			err := ValidateWorkspaceCanary(&c, series, skill, tool, tmpl, slot)
			if err == nil {
				t.Fatal("invalid canary accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not report %q", err, tc.want)
			}
		})
	}
	valid := t043Canary()
	if err := ValidateWorkspaceCanary(&valid, series, skill, tool, tmpl, slot); err != nil {
		t.Fatalf("valid canary rejected: %v", err)
	}
}

// TestCanaryHostSlotStagedFilesAfterFreeze stages a real canary file per
// host × worker slot after the skill/template freeze, seals its digests into a
// canary receipt per slot, and proves the mismatches that must invalidate:
// slot swap, template re-freeze, tool identity drift and a staged file edited
// after the canary ran.
func TestCanaryHostSlotStagedFilesAfterFreeze(t *testing.T) {
	cfg := t043Config()
	stageRoot := t.TempDir()
	settings := filepath.Join(stageRoot, filepath.Base(cfg.ClaudeSettings))
	if err := os.WriteFile(settings, []byte(`{"model":"opus"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const (
		series      = "series-048"
		skillDigest = "skill-snapshot-digest"
	)
	digests := map[string]string{}
	for _, host := range []string{HostClaude, HostCodex, HostOpenCode} {
		tmpl, err := TemplateForHost(host, cfg.ClaudeSettings, cfg.CodexProvider, cfg.CodexModel, cfg.OpenCodeModel)
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		tmplDigest, err := tmpl.Digest()
		if err != nil {
			t.Fatalf("%s digest: %v", host, err)
		}
		toolIdentity := sha256Hex([]byte("tool-identity:" + host))
		digests[host] = tmplDigest
		for slot := 1; slot <= 3; slot++ {
			content := fmt.Sprintf("canary %s slot %d\n", host, slot)
			ws := filepath.Join(stageRoot, "staged", host+"-"+fmt.Sprint(slot))
			if err := os.MkdirAll(ws, 0o700); err != nil {
				t.Fatal(err)
			}
			canaryFile := filepath.Join(ws, "canary.txt")
			if err := os.WriteFile(canaryFile, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			fileDigest := LFNormalizedSHA256Bytes([]byte(content))
			c := WorkspaceCanaryReceipt{
				SeriesID:                series,
				Host:                    host,
				SkillDigest:             skillDigest,
				ToolIdentityDigest:      toolIdentity,
				ExecutionTemplateDigest: tmplDigest,
				WorkerSlot:              slot,
				ChildIdentityDigest:     sha256Hex([]byte(host + "\x00" + fmt.Sprint(slot))),
				AccessBoundaryDigest:    sha256Hex([]byte("boundary:" + host)),
				CanaryWorkspaceDigest:   sha256Hex([]byte(ws)),
				ExpectedFileDigest:      fileDigest,
				ObservedCWDDigest:       sha256Hex([]byte(ws)),
				ObservedFileDigest:      fileDigest,
				Status:                  "pass",
				ReceiptDigest:           sha256Hex([]byte("receipt:" + host + fmt.Sprint(slot))),
			}
			if err := ValidateWorkspaceCanary(&c, series, skillDigest, toolIdentity, tmplDigest, slot); err != nil {
				t.Fatalf("%s slot %d: %v", host, slot, err)
			}
			if host == HostClaude && slot == 2 {
				// Slot swap: a canary sealed for slot 2 cannot stand in for slot 3.
				if err := ValidateWorkspaceCanary(&c, series, skillDigest, toolIdentity, tmplDigest, 3); err == nil ||
					!strings.Contains(err.Error(), "canary slot 2, want 3") {
					t.Fatalf("slot-swapped canary accepted: %v", err)
				}
				// Cross-host tool identity: another host's identity invalidates it.
				if err := ValidateWorkspaceCanary(&c, series, skillDigest, "tool-identity:codex", tmplDigest, slot); err == nil ||
					!strings.Contains(err.Error(), "canary tool identity drift") {
					t.Fatalf("cross-host canary accepted: %v", err)
				}
				// Template re-freeze after the canary ran invalidates the map.
				refrozenTmpl := CodexTemplate(cfg.CodexProvider, cfg.CodexModel+"-next")
				refrozen, _ := refrozenTmpl.Digest()
				if refrozen == tmplDigest {
					t.Fatal("re-frozen template must produce a different digest")
				}
				if err := ValidateWorkspaceCanary(&c, series, skillDigest, toolIdentity, refrozen, slot); err == nil ||
					!strings.Contains(err.Error(), "canary execution template drift") {
					t.Fatalf("canary accepted after template re-freeze: %v", err)
				}
				// A staged canary file edited after the run is an observation mismatch.
				tampered := content + "tampered\n"
				if err := os.WriteFile(canaryFile, []byte(tampered), 0o600); err != nil {
					t.Fatal(err)
				}
				c.ObservedFileDigest = LFNormalizedSHA256Bytes([]byte(tampered))
				if err := ValidateWorkspaceCanary(&c, series, skillDigest, toolIdentity, tmplDigest, slot); err == nil ||
					!strings.Contains(err.Error(), "canary observation mismatch") {
					t.Fatalf("canary accepted over an edited staged file: %v", err)
				}
			}
		}
	}
	if digests[HostClaude] == digests[HostCodex] {
		t.Error("canary template digests must distinguish hosts")
	}
}
