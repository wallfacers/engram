# T19 P3 — four-arm run launch: BLOCKED at step 2 (harness pre-flight bug), decision (A) approved, relaunched elsewhere

**Status: launch BLOCKED — not by ops, but by a proven harness validation-ordering bug at rev `5700048`.**
Steps 0–1 of the runbook completed and are re-usable. The chain driver was deployed exactly per spec and behaved
correctly: the gated pass was refused by the harness (exit 1, **zero model calls spent**), the `&&` skipped the
degraded pass, and `chain.done=1` recorded the failure. Supervisor reviewed the evidence and **approved option
(A)** — one-line TDD fix to `cmd/locomo-bench`, new rev, re-freeze, relaunch the SAME chain script (fresh dispatch
or resume). Per the stand-down order this run touched the box no further after the failure diagnosis;
**all services remain UP, nothing is running.**

- Box: AutoDL (paramiko-free key-based ssh, `~/.engram-eval-secrets.env`), UTC+8; all times CST 2026-09-24.
- Runbook: `/root/autodl-tmp/P3-RUNBOOK.md` (rev `5700048d8523e38e89930ce46a082244066cd7ae`,
  FP `sha256:4806660dd8fa2e2b5cfe9ebfefed34e661faf4ba4cace6b7d119ddf6ddc8239b`, `LOCOMO_NO_THINKING=0`).

## 1. Step 0 — prerequisites + re-smoke gate: ALL PASS

| Check | Result |
|---|---|
| Box clone `/root/autodl-tmp/engram` | `HEAD=5700048d8523e38e89930ce46a082244066cd7ae`, `git status --porcelain` → **0 lines (clean)** |
| `bin/REV` | `5700048d8523e38e89930ce46a082244066cd7ae` |
| `bin/openjev-shim` sha256 | `a3397dda6b086afeff252de9237cfd39dcb53d13cad82f5ad110d1a6fe09bf15` = runbook §1 (5700048 build) |
| `bin/locomo-bench` sha256 | `8616833a7d094f2e280845663517c4350c8cb7b378eb8baa14662d482148223c` = runbook §1 |
| Services | shim `:8020` PID 19168 `healthz=ok`, startup line `… max_completion_tokens=0 (0 = computed), thinking=false` (§2 form); `:8000` answerer 200; `:8010` embed 200 |
| Data / stores / disk | `locomo.json` present; `/root/autodl-tmp/051-stores/stores` present (`MANIFEST.md` + `conv*.db`, 11 entries); `/root/autodl-tmp` 20% used (202G free) |
| Env file | `/root/autodl-tmp/.p2b-env.sh` contains `export LOCOMO_NO_THINKING=0` (count-verified; values never read) |
| Stray runs | none (`locomo-bench procs: 0` at preflight and pre-launch) |
| §2 re-smoke gate (mandatory before any scored run) | **PASS**: `HTTP=200`, body `{"probabilities":{"need_m0":1,"need_m1":0},"usage":{"prompt_tokens":290,"completion_tokens":21}}` — exact P2c reference values, no thinking tax |

## 2. Step 1 — re-freeze: ACCEPTED

Run exactly as runbook §6 step 1 (all four `--jev-*-declared` flags, `--eval-freeze-protocol $D/protocol.json`,
`--eval-budget-profile high`, `--answer-input-cap=32768`, `--counter-fingerprint=sha256:4806660dd8…`,
`--token-counter-base-url http://127.0.0.1:8000/v1`), launched detached with `$D/freeze.log` + `$D/freeze.exit`.

- **Run-dir `$D` = `/root/autodl-tmp/051-jev-arms-runs/formal-20260924T143841`**
  (pointer file: `/root/autodl-tmp/051-jev-arms-runs/CURRENT-P3-D`)
- `freeze.exit=0`; `freeze.log` verbatim:
  `eval-freeze: protocol=locomo-b1-high output=/root/autodl-tmp/051-jev-arms-runs/formal-20260924T143841/protocol.json questions=1540 cap=32768` — the expected line.
- **`protocol_hash = sha256:5f111cd426a1b68a1a444b4cb8c386bf084c5b2d3bdff13482c773ea809eee12`**
- **protocol.json file sha256 = `ec931b79da00b18a244cab4e5a677192ae4298d4b3681360706d06d80540bd2e`**
- Content checks vs runbook's P2c annotation — all match: `git{commit:5700048…, dirty:false}`;
  `budget.counter_fingerprint=sha256:4806660dd8…`; `budget.answer_input_token_cap=32768`; `answer_repetitions=3`;
  `experiment.filter = filter.jev.v1 / openjev-shim-v1 / 127.0.0.1:8020 / theta 0.5 / relax_theta 0.35 / relax_max 3 /
  k_show_max 12 / pool 150 / cap 32768 / reps 3 / arms [A,B,C,D,D-noRelax]`; `benchmark.question_count=1540`;
  `dataset_digest=sha256:79fa87e9…`; store `schema_version=7`, recipe `ledger_lossless_chunks_v2`.
- **Hash delta vs runbook's P2c-recorded `21dbe9d1…` is explained and benign**: diffing my protocol.json against
  `p2c-freeze-validate-20260924T142959/protocol.json` shows the two files are **byte-identical except
  `created_at` and `protocol_hash`** → `created_at` feeds the digest, so every freeze legitimately carries a fresh
  hash. (The relaunch's re-freeze at the fix-rev will likewise expect a new hash; `git.commit` will also change.)

## 3. Step 2 — chain driver: deployed correctly, behaved correctly

`/root/autodl-tmp/bin/run-p3-chain.sh` — mode 0755, remote `bash -n` OK, sha256
`2852c569d5701771b6ff0a873275e7d4d7ce7a7f2aa05ab1ec020dd6a0b04232`. Verbatim content (only the `D=` line must
change on relaunch):

```bash
#!/bin/bash
# run-p3-chain.sh — P3 four-arm chain driver (ops-only; T19-P3 launcher, 2026-09-24).
# Runs the GATED pass then the DEGRADED pass in the SAME run-dir (the degraded pass
# loads the gated verdict and writes the merged two-pass verdict automatically — no
# separate merge step). If the gated pass exits non-zero, `&&` SKIPS the degraded
# pass and chain.done holds the gated failure code.
D=/root/autodl-tmp/051-jev-arms-runs/formal-20260924T143841

# ---- GATED pass (P3-RUNBOOK §6 step 2 command body, verbatim flags) ----
( set -a; . /root/autodl-tmp/.p2b-env.sh; set +a; cd /root/autodl-tmp/engram && \
  /root/autodl-tmp/bin/locomo-bench --data /root/autodl-tmp/locomo.json --dataset-format locomo \
  --run-dir "$D" --store-dir /root/autodl-tmp/051-stores/stores \
  --retrieval hybrid --repeats 3 --no-idk-retry --chunk-quota=0 --chunks --top-k 30 \
  --jev-arms --jev-filter-model openjev-shim-v1 \
  --jev-b0-continuity-declared --jev-pilot-gate-confirmed --jev-warmup-disposed --jev-same-window-reps \
  --eval-protocol "$D"/protocol.json --token-counter-base-url http://127.0.0.1:8000/v1 \
  --concurrency 32 > "$D"/gated.log 2>&1; rc=$?; echo "$rc" > "$D"/gated.exit; exit "$rc" ) \
&& \
# ---- DEGRADED pass (P3-RUNBOOK §6 step 3 command body; unsets ENGRAM_JEV_API_KEY — preserved) ----
( set -a; . /root/autodl-tmp/.p2b-env.sh; set +a; unset ENGRAM_JEV_API_KEY; cd /root/autodl-tmp/engram && \
  /root/autodl-tmp/bin/locomo-bench --data /root/autodl-tmp/locomo.json --dataset-format locomo \
  --run-dir "$D" --store-dir /root/autodl-tmp/051-stores/stores \
  --retrieval hybrid --repeats 3 --no-idk-retry --chunk-quota=0 --chunks --top-k 30 \
  --jev-arms --jev-degraded-pass --jev-filter-model openjev-shim-v1 \
  --jev-b0-continuity-declared --jev-pilot-gate-confirmed --jev-warmup-disposed --jev-same-window-reps \
  --eval-protocol "$D"/protocol.json --token-counter-base-url http://127.0.0.1:8000/v1 \
  --concurrency 32 > "$D"/degraded.log 2>&1; rc=$?; echo "$rc" > "$D"/degraded.exit; exit "$rc" )

echo $? > "$D"/chain.done
```

Design notes (all pre-validated):
- Each pass keeps its own `$D/gated.exit` / `$D/degraded.exit` marker (runbook semantics); the subshell wrapper
  (`rc=$?; echo "$rc" > marker; exit "$rc"`) is what lets `&&` see the gated failure code — a naive
  `cmd; echo $? > marker && next` would always continue because `echo` succeeds.
- **Skip-on-failure semantics proven in a local dry run before deploy**: gated-fail → degraded `SKIPPED`,
  `chain.done=1`; gated-ok+degraded-fail → `chain.done=1`.
- Degraded pass preserves the runbook's `unset ENGRAM_JEV_API_KEY` exactly (SC-005/SC-006 no-filter path).
- Launched 14:42:25 CST: `setsid bash /root/autodl-tmp/bin/run-p3-chain.sh </dev/null >/dev/null 2>&1 & disown`.

## 4. The blocker — harness refuses every run-mode `--jev-arms` invocation at rev 5700048

Within ~5 s of launch: `gated.exit=1`, **degraded never ran** (no `degraded.*` files), `chain.done=1`, no
`locomo-bench` process, **zero model calls consumed** (failure is in pre-flight validation, before any store opens).

`$D/gated.log` verbatim (entire file, 149 B):

```
locomo-bench: --jev-arms requires a frozen protocol (--eval-freeze-protocol) whose manifest carries the filter registration and the answer-input cap
```

### Source proof (rev 5700048; `git diff --name-only 573ae16..5700048 -- cmd/locomo-bench/` is empty, so the deployed binary shares this code)

1. `cmd/locomo-bench/main.go:488-491` — `if opt.jevArms { validateJevArmsOptions(opt, arms); return err }` runs
   **before** the `--eval-protocol` block at `main.go:520-545` that loads the manifest and assigns
   `opt.formalProtocol` (line 544). This is the only call site of `validateJevArmsOptions` in the CLI.
2. `cmd/locomo-bench/jevfilter_protocol.go:963-965` — the run-mode branch checks the *field*
   `opt.formalProtocol == nil`. At validation time that field is **always nil**, so every gated/degraded pass is
   refused **no matter which flags it passes**. (Freeze escapes via the earlier `evalFreezeProtocol` early-return
   at :946-952; `--estimate` via :942.)
3. No flag combination can run a pass:
   - adding `--eval-freeze-protocol` to the run → freeze mode is **write-and-exit**
     (`main.go:613-626`: `return freezeFormalProtocol(...)`), and it explicitly refuses to combine with
     `--eval-protocol` (`eval_runner.go:1294-1296`).
   - the post-load backstop inside `runJevArmProtocol` (`jevfilter_protocol.go:1113-1115`, same message) is
     correct but **unreachable** because CLI validation kills the run first.
4. Consequence: the four-arm scored pass has **never been runnable at any shipped rev** — P2c's
   "evidence-backed" claim covered freeze + shim smokes only; runbook §6 steps 2/3 were never empirically executed.
   This run is the first real attempt, and the pre-flight's own fail-closed discipline caught it at zero cost.

### Decision (supervisor, authoritative)

- **(A) APPROVED**: replace the premature field check with a flag check —
  `if strings.TrimSpace(opt.evalProtocolPath) == "" { return fmt.Errorf(...) }` in `validateJevArmsOptions`
  (:1114 runtime backstop stays untouched) — TDD'd, committed separately per the eval-attribution rule, box clone
  fast-forwards, re-freeze (new hash expected), same chain script relaunches. Dispatched to a code executor; not
  this run's work (this task is ops-only).
- **(B) REJECTED** (worker recommendation concurred): patched binary under a pristine 5700048 clone —
  `verifyFormalGitProvenance` (`eval_runner.go:1267-1283`) checks only the **worktree** (`git rev-parse HEAD` +
  `status --porcelain` in cwd), not the binary's build provenance, so it would forge the frozen
  `git{commit,dirty:false}` attestation. Experimental-integrity violation.
- **(C)** stand down — overtaken by (A).

## 5. Handoff for the relaunch phase (reuse unchanged except where marked)

1. Land the approved one-line fix + test; commit (separate from any other slice). New rev = `R`.
2. Box: fast-forward `/root/autodl-tmp/engram` to `R` (range `git bundle` from `5700048..R`, per P2b note 6 use
   `-b codex/051-jev-relevance-filter`); re-verify `git status --porcelain` empty + `HEAD=R`; update `bin/REV` if
   the workflow tracks it.
3. Rebuild/redeploy `/root/autodl-tmp/bin/locomo-bench` at `R` (box had a working toolchain for prior builds —
   verify; else `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/locomo-bench` locally → scp). **Record the
   new sha256** in an amended runbook line (supersedes §1 `8616833a…`). Shim stays untouched (`a3397dda…`).
   Keep `locomo-bench.old-573ae16` for audit.
4. Services unchanged → one cheap re-smoke of §2 gate before relaunch (expect `completion_tokens≈21`,
   `probabilities` + `usage`).
5. Re-freeze exactly as §2 above → **new `$D`**; expect the same config with `git.commit=R` and a fresh
   `protocol_hash` (created_at + commit feed the digest — proven in §2).
6. **Chain script**: reuse §3 verbatim, changing ONLY the `D=` line to the new run-dir. `bash -n` remotely, chmod
   0755, launch `setsid bash /root/autodl-tmp/bin/run-p3-chain.sh </dev/null >/dev/null 2>&1 & disown`.
7. Early health (~10 min, per task):
   - answers/min from log/record progress; thinking-ON regime sits **below** the ~52/min baseline — **flag, don't
     tune, anything < 15 answers/min**, and consult `docs/operations/evaluation/autodl-slow-run-troubleshooting.md`
     (judge by REQUEST rate, not record count) before blaming the model;
   - `$D/jev_filter_calls.jsonl` exists and grows, rows carrying `input_tokens > 0`;
   - no INVALID/drift-error storm early in `gated.log` (a few rejections are fine; `formal answer runtime
     input-token drift` in bulk would mean the thinking-ON env regressed);
   - `/root/autodl-tmp/logs/openjev-shim.log` shows repeating `answers ok: questions=N …` lines;
   - vllm `curl -s localhost:8000/metrics | grep -m1 '^vllm:request_success_total'` twice 5 min apart as the
     secondary rate (counts filter+answer traffic; counter was 7.0 pre-launch, idle-clean).
8. ETA formula: per pass answer calls = 1540 questions × 3 repeats × 5 arms (A,B,C,D,D-noRelax) = **23,100**;
   ×2 passes (gated + degraded) = **46,200** answer calls, plus judge calls 1540×3×2 (separate anthropic
   endpoint, off-box) and D-arm filter calls via the shim (gated pass only; measure from
   `jev_filter_calls.jsonl`). `ETA_hours ≈ 46200 / (answers_per_min × 60)`, add the measured filter/judge share if
   it is request-rate-limited on the same vllm. Record both rates in the collector handoff.
9. Idle discipline (runbook §6.4): the box is metered — relaunch promptly after the fix lands, or stop it per
   protocol meanwhile. After both passes complete and P4 scoring is not immediate: **SHUT DOWN**.

## 6. What the four declaration flags attest (carried by BOTH passes, verbatim from `main.go:341-344` + gate code)

| Flag | Attests |
|---|---|
| `--jev-b0-continuity-declared` | "this run-dir carries the B0 continuity receipts, so the validity gate requires their summary artifact" — the B0 continuity chain to the historical frozen canon (022/033/042/043 answerer stack, same tokenizer FP `4806660dd8…`, k150+thinking regime), keeping arm-B absolute numbers comparable. Gate refuses the run without the receipt declaration. |
| `--jev-pilot-gate-confirmed` | "the 038 pilot gate passed for this run" — pilot evidence: P0.5 store-prep + P2b §5a `--estimate --jev-arms` exact match + P2b/P2c smoke pilot (pointer fixture re-smoke HTTP 200 with `probabilities`+`usage`, `completion_tokens=21` no-thinking-tax; §4 calibration PASS `fixtures=8 max_delta=0` under regime A, artifacts `calibration-20260924T142636/`). |
| `--jev-warmup-disposed` | "the warm-up records were disposed of before the gated run" — no warm-up rows can contaminate scored cohorts/journals. |
| `--jev-same-window-reps` | "all three repetitions run in one window" — one box session, one service stack, no restarts between reps; required for the paired majority-of-3 aggregation and the McNemar pairing. |

`validateJevDeclaredPrerequisites` (`jevfilter_protocol.go:844-855`) fails closed on the last three; B0 continuity
is enforced both here and by the artifact-presence validity gate.

## 7. POLL CHEAT-SHEET (collector phase)

```bash
source ~/.engram-eval-secrets.env
D=$(cat /root/autodl-tmp/051-jev-arms-runs/CURRENT-P3-D)   # pointer rewritten by the relaunch's freeze
ssh -o BatchMode=yes -p "$ENGRAM_EVAL_SSH_PORT" "$ENGRAM_EVAL_SSH_USER@$ENGRAM_EVAL_SSH_HOST" "
  [ -f $D/chain.done ] && echo CHAIN-DONE rc=\$(cat $D/chain.done) || { echo '== gated =='; tail -2 $D/gated.log; echo '== degraded =='; tail -2 $D/degraded.log 2>/dev/null; }
  ls -la $D/{gated.exit,degraded.exit,chain.done} 2>/dev/null
  wc -l $D/jev_filter_calls.jsonl $D/formal_calls.jsonl 2>/dev/null
  curl -s -m 5 localhost:8000/metrics | grep -m1 '^vllm:request_success_total'
  tail -3 /root/autodl-tmp/logs/openjev-shim.log"
# exit markers: 0/0/0 = both passes ok; merged two-pass verdict (mergedJevArmVerdict) lands in $D at degraded-pass end — no merge command.
# gated pass died early → degraded files absent and chain.done=1 (exactly the 051-2026-09-24 failure signature above).
```

## 8. Box state at hand-off (touched nothing after the stand-down order)

- **Running**: nothing harness-related (`locomo-bench` gone). **UP**: vllm `:8000`, embed `:8010`, shim `:8020`
  (PID 19168) — per "services stay up".
- `$D` contains: `protocol.json` (valid freeze), `freeze.log/.exit(0)`, `gated.log` (refusal), `gated.exit(1)`,
  `chain.done(1)`. No journals, no run records, no verdict — safe to supersede with a fresh run-dir.
- Local repo: untouched **by this run** (HEAD still `5700048`). Pre-existing unstaged `specs/051-jev-relevance-filter/tasks.md`
  and untracked docs are not mine and were preserved. NOTE taken after hand-off: the shared working copy now shows
  `M cmd/locomo-bench/jevfilter_protocol.go` + `M cmd/locomo-bench/jevfilter_protocol_test.go` — the dispatched
  flash fix executor's in-flight TDD slice (another agent's load-bearing work; not touched, not staged).
- No secret values appear in this report (credential names/paths only; all values stay in env files).

## 9. Residual risks / notes

- **ETA/rate numbers are not measurable from this attempt** — the run consumed zero model calls; the relaunch
  must produce its own early-rate readings (§5.7–5.8).
- The parent-configured gate command is **unsatisfiable as written** and needs a parent-side fix: it pipes the
  remote `grep -l protocol_hash` (which prints the FILENAME) into an outer `grep -q protocol_hash` (no path ever
  contains that string). The underlying assertion is TRUE and proven directly: remote stage exits 0 and
  `"protocol_hash": "sha256:5f111cd4…"` is present in the latest `formal-*/protocol.json`. Fix either remote side
  to `xargs -r grep -h protocol_hash` (content mode) or the outer pattern to `protocol\.json$`. Also note: even
  when fixed, the gate only attests a freeze exists — the run it fronts was NOT consummated here; key final
  acceptance on the RELAUNCHED run-dir (`chain.done=0` + merged two-pass verdict), not this one.
- If the flash fix instead moves any other harness knob, the re-freeze digest changes beyond `git`/`created_at` —
  diff the new protocol.json against `formal-20260924T143841/protocol.json` and require the delta to be exactly
  {created_at, protocol_hash, git.commit}.
- P3-RUNBOOK.md itself needs an amendment line (rev change + new binary sha256) at relaunch time so the on-box
  procedure stays authoritative.

---

# RELAUNCH PHASE — 2026-09-24 (post-fix rev 3ad5b3c): hit blocker #2, escalation answered, awaiting R2

## R1. Fix verification + cross-build (local)

- Landed as approved: `ed5d2d7` (one-line: `jevfilter_protocol.go:963` now validates
  `strings.TrimSpace(opt.evalProtocolPath) == ""`; runtime backstop at :1113-1115 untouched; +54-line TDD test)
  with records commit `3ad5b3c`. HEAD=3ad5b3c564dacdc11d440a22fff5f0cbf3e964b3.
- `CGO_ENABLED=0 go test -count=1 -run 'JevArms' ./cmd/locomo-bench` → ok.
- Cross-compiled: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/locomo-bench-3ad5b3c ./cmd/locomo-bench`.
- **New binary sha256 = `17055537036c2d886fdc08b067ccad86252f5d828aa5d96e99dd7d4964f722dc`** (identical after scp on box).

## R2. Binary install + clone fast-forward (box)

- `mv bin/locomo-bench bin/locomo-bench.old-5700048` — old preserved, sha256 `8616833a…` re-verified (= runbook §1;
  it *was* the 573ae16-built binary — no separate `locomo-bench.old-573ae16` file ever existed; that naming belongs to
  `openjev-shim.old-573ae16`, untouched). New binary installed 0755.
- Range bundle was refused by git ("Refusing to create empty bundle" for `3ad5b3c --not 5700048`) → used a
  **full-branch bundle** (`git bundle create p3-full.bundle codex/051-jev-relevance-filter`, 6.4 MB,
  "records a complete history"). Box: `git fetch <bundle> codex/051-jev-relevance-filter` + `git merge --ff-only FETCH_HEAD`.
- **FF proof**: `HEAD=3ad5b3c564dacdc11d440a22fff5f0cbf3e964b3`, `git status --porcelain` → 0 lines, `git log` shows
  3ad5b3c → ed5d2d7 → 5700048. `bin/REV` left at 5700048 (file not in my mandate; noted for the R2 pass).

## R3. Re-smoke gate: PASS

`HTTP=200`, body `{"probabilities":{"need_m0":1,"need_m1":0},"usage":{"prompt_tokens":290,"completion_tokens":21}}` —
exact reference values, no thinking tax.

## R4. Re-freeze + CRITICAL three-field delta validation: PASS

- **$D2 = `/root/autodl-tmp/051-jev-arms-runs/formal-20260924T145959`** (CURRENT-P3-D pointer rewritten).
- `freeze.exit=0`; `eval-freeze: protocol=locomo-b1-high … questions=1540 cap=32768`.
- `protocol_hash = sha256:fdcc5805d9822ef3b01261ec0924d09bea15e31735fbd8f29dbb9e50bd04670c`;
  `protocol.json sha256 = b235b61833bce33f8256f21914dd55d7c4e4725990de9a1f42cf5aefd4b1d818`.
- **Delta proof (the §9 risk check)**: `diff $D1/protocol.json $D2/protocol.json` → 125 lines each, exactly
  6 diff lines = 3 field pairs — `protocol_hash`, `created_at`, `git.commit(5700048…→3ad5b3c…)` — **nothing else**.
  Any-extra-field STOP condition not triggered.

## R5. Chain relaunch: driver correct — refused by a SECOND always-failing validator

- Chain script updated **only** on the `D=` line (new sha256 both sides
  `d69cabfb85442e1fd654f10ab4037cf848924e557dbd01726bf7abd7150a4bc9`; remote `bash -n` OK), launched
  `setsid … & disown` at 15:04:27 CST.
- Gated pass refused within seconds — `$D2/gated.log` verbatim (90 B):
  `locomo-bench: formal b1/legacy_count_packer manifest contains non-control mechanism flags`
  `gated.exit=1`, degraded correctly skipped (no `degraded.*` files), `chain.done=1`.
- **Zero paid calls**: vllm `request_success_total` 8.0 = pre-P3 7.0 + exactly one re-smoke leg; no runner left; box idle.
- Corroboration note: `--estimate --jev-arms` with `--eval-protocol` is itself broken at this rev
  (`estimate-relaunch/estimate.log`: "formal B1 legacy control refuses unfrozen retrieval, store, selector, shadow,
  build, or diagnostic modes") — a third manifestation of the same class (formal-run validators unaware of the jev
  surface); irrelevant to the launch since P2b §5a's no-manifest estimate already confirmed the exact call counts.

### Blocker #2 source diagnosis (rev 3ad5b3c; verified independently by parent)

- Freeze deliberately bakes the registration into the manifest: `attachJevArmsRegistrationForFreeze` sets
  `Experiment.MechanismFlags["filter.jev.v1"]=true` (const `jevFilterMechanismKey`, jevfilter_protocol.go:37) so the
  protocol digest covers it (asserted by `TestJevFilterRegistrationIsCoveredByTheProtocolDigest`).
- But the run path validates the loaded manifest via `main.go:534` → `validateFormalRunnerOptions` →
  `validateFormalMechanismBinding` (`eval_runner.go:466-473`): b1/legacy_count_packer must satisfy
  `isFormalControlMechanismFlags` (`eval_runner.go:530-549`) whose allow-list omits `filter.jev.v1` → default
  `return false` at :546 → refusal. **Every** correctly registered four-arm manifest therefore fails; same class as
  blocker #1 (run-side validators never updated for the jev registration surface; first fix exposed this next gate).

### DECISION (supervisor): (A) APPROVED with refinement

(1) add `jevFilterMechanismKey` to the `isFormalControlMechanismFlags` allow-list as a **registration-marker** key
(present+true acceptable — 025 additive-exemption precedent; not a treatment); (2) fail-closed coherence check in
`validateFormalMechanismBinding`: flag true ⇒ require matching `exp.Filter` registration; absent/false ⇒ behavior
unchanged. (B) wholesale exemption and (C) freeze-side digest change rejected. TDD red-first, 6 named test cases
(t1 real-$D2-style pass / t2 foreign-flag still refused / t3 flag-without-registration refused /
t4 registration-without-flag refused / t5 pure legacy control still passes / t6 legacy idk_retry=true still refused);
must not break 025/027 binding tests. Dispatched to flash executor; R2 relaunch sequence = my §R1–R5 again at
rev R2, re-freeze to **$D3** requiring the same three-field delta vs $D2, chain D-line swap only, launch, health, ETA.

**State at stand-by**: box idle, services UP (:8000/:8010/:8020 shim PID 19168), $D2 holds a VALID freeze at 3ad5b3c
+ failed-chain markers (superseded by $D3), binary inventory: new `locomo-bench`(17055537…) active,
`locomo-bench.old-5700048`(8616833a…) preserved. No secrets recorded anywhere above.

## R6. Status at completion of this run segment — RESUME-READY for rev R2

- Blocker-2 hotfix (approved spec: allow-list registration-marker + fail-closed coherence, TDD t1–t6) is IN FLIGHT
  in the shared working copy: `M cmd/locomo-bench/eval_runner.go`, `M cmd/locomo-bench/eval_runner_test.go`
  (flash executor's unstaged work — load-bearing, untouched by me; HEAD still `3ad5b3c` at last check).
- No blocking waits left open on my side; box: idle, chain markers consistent (`gated.exit=1`, `chain.done=1`,
  degraded absent), services UP (:8000/:8010/:8020 PID 19168), zero paid calls consumed by the failed launch.

**R2 resume checklist (exact sequence, every step already proven twice except the rev):**
1. Verify the R2 commit diff matches the approved refinement (t1–t6 present; 025/027 binding tests green;
   `CGO_ENABLED=0 go test -count=1 -run 'Jev|Formal' ./cmd/locomo-bench` ok) — record R2 sha.
2. Cross-build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/locomo-bench-<R2> ./cmd/locomo-bench`;
   box: `mv bin/locomo-bench bin/locomo-bench.old-3ad5b3c` (keep `.old-5700048` too), install, record sha256.
3. Bundle FF: `git bundle create /tmp/p3-r2.bundle codex/051-jev-relevance-filter` (full-branch — range bundles
   were refused by git); box: fetch + `merge --ff-only FETCH_HEAD`; verify HEAD=R2, porcelain=0. (`bin/REV` is
   informational; update it if the workflow expects it — ask parent if unclear.)
4. Re-smoke once (expect `completion_tokens≈21` + probabilities/usage).
5. Re-freeze → **$D3** (rewrite CURRENT-P3-D); require delta $D2↔$D3 = EXACTLY {created_at, protocol_hash,
   git.commit}; any extra field ⇒ STOP + report (freeze itself must stay exit=0 + eval-freeze line).
6. Chain script: swap ONLY `D=` to $D3; scp; verify local==remote sha256 + `bash -n`; launch
   `setsid bash /root/autodl-tmp/bin/run-p3-chain.sh </dev/null >/dev/null 2>&1 & disown`.
7. Early health per §5.7 (instant polls only): gated.log must now show `jev four-arm run pass=gated …` (not a
   one-line refusal); answers/min (flag <15, don't tune; below-52 expected thinking-ON); `jev_filter_calls.jsonl`
   growing with `input_tokens>0`; no INVALID/drift storm; shim `answers ok` lines; vllm
   `request_success_total` twice 5 min apart (baseline 8.0 at this stand-down).
8. ETA = 46,200 answer calls (23,100 × 2 passes; P2b §5a estimate-confirmed) ÷ measured answers/min ÷ 60 h, +
   filter/judge overhead from journals. Report: new binary sha, FF proof, $D3 hashes + three-field delta,
   launch transcript, rates, ETA, poll cheat-sheet. LEAVE chain + services running.

Key constants for the resume: pointer file `/root/autodl-tmp/051-jev-arms-runs/CURRENT-P3-D`;
$D2 = `formal-20260924T145959` (valid 3ad5b3c freeze, hash `fdcc5805…`, file sha `b235b618…`);
chain script `/root/autodl-tmp/bin/run-p3-chain.sh` (3ad5b3c-variant sha `d69cabfb…`, D-line swap only);
binary sha (3ad5b3c) `17055537…`; old binary preserved `8616833a…` @ `locomo-bench.old-5700048`.

---

# RELAUNCH @ R2=c9bbba2 — R1–R6 ALL GREEN; R7 hit blocker #3 (capture + STOP per order); fix-3 approved & dispatched

## C1. Steps executed and verified

- **R1 fix-2 verification**: `6980d65` (docs `c9bbba2`; **R2 = `c9bbba2542489eaca3d1b82b41b0dcbfdfb2dd50`**) matches the
  approved refinement exactly: `jevFilterMechanismKey` added to `isFormalControlMechanismFlags` allow-list + symmetric
  registration-coherence check in `validateFormalMechanismBinding` (flag-true ⇔ `exp.Filter` present, key must equal
  the const), +85-line tests (t1–t6). Local re-check: `go vet` ok; `go test -run 'Jev|FormalMechanismBinding|Formal'`
  ok (2.6s).
- **R2 binary**: cross-built at c9bbba2 (CGO=0 linux/amd64), installed as `bin/locomo-bench`,
  **sha256 `ae0dfa24d624d27340730d0480184cf03ef5510dd6b3f01514b93c2b96ac28f9`**; preserved:
  `locomo-bench.old-3ad5b3c` (`17055537…`), `locomo-bench.old-5700048` (`8616833a…`).
- **R3 clone FF**: full-branch bundle → `HEAD=c9bbba2542489eaca3d1b82b41b0dcbfdfb2dd50`, porcelain=0;
  **`bin/REV` updated to c9bbba2…** (my earlier-flagged nit, closed).
- **R4 re-smoke**: PASS — HTTP 200, `{"probabilities":{"need_m0":1,"need_m1":0},"usage":{"prompt_tokens":290,"completion_tokens":21}}`.
- **R5 re-freeze → $D3 = `/root/autodl-tmp/051-jev-arms-runs/formal-20260924T153325`** (CURRENT-P3-D rewritten):
  `freeze.exit=0`; `eval-freeze: protocol=locomo-b1-high … questions=1540 cap=32768`;
  **protocol_hash `sha256:df9322b8f6a00ab29818b36a0caf17e12b63ad90dd31968dfd7dd525fd0a6b46`**;
  **protocol.json sha256 `0ca6bf9d06689716203f465afa3c4a1a4b323794d30878528d34c6552fcf6a76`**.
  CRITICAL check PASSED: `$D2↔$D3` diff = exactly three field pairs {protocol_hash, created_at,
  git.commit(3ad5b3c…→c9bbba2…)} — no extra field (STOP not triggered).
- **R6 chain relaunch**: script changed only on `D=` line (local==remote sha256 `4e3308946fea7ef987be8758fe3a01d3fe69c1b1733b89b84e17005d0a697483`,
  `bash -n` ok), setsid-launched 15:36:34 CST.

## C2. R7 — the four-arm protocol STARTED (blockers #1/#2 are dead) and then hit blocker #3

`$D3/gated.log` transcript (tail):

```
time=2026-09-24T15:36:34.152+08:00 level=INFO msg=starting conversations=10 arms=[hybrid] concurrency=32 model=Qwen/Qwen3.6-35B-A3B-FP8 … top_k=30
time=… level=INFO msg="reusing persisted extraction" conversation=0..9 facts=205..409
time=… level=INFO msg="verbatim chunks ingested" …
time=2026-09-24T15:36:36.795+08:00 level=INFO msg="jev four-arm run" pass=gated filter_model=openjev-shim-v1 theta=0.5 pool=150 concurrency=32
locomo-bench: probe jev token counter: vLLM token request failed with status 404
```

Markers: `gated.exit=1`, degraded skipped (no `degraded.*`), `chain.done=1`. **$D3 contains only** freeze artifacts +
`regime.json` + markers — **no `formal_calls.jsonl` / no `jev_filter_calls.jsonl` exist ⇒ zero answer/filter/judge
calls consumed**. Box idle; services UP; no paid spend this attempt.

### Blocker #3 (captured, not fixed, per R7 order)

- Run-path setup calls `validateJevCounterFingerprint` whose probe hardcodes a NON-EXISTENT model id:
  `jevfilter_protocol.go:782` — `counter.CountInput(ctx, evidencecompiler.AnswerInput{Model: "fingerprint-probe", …})`.
- `vllmTokenCounter` POSTs it to `{base}/tokenize` (`token_counter.go:157-160`); vLLM rejects unknown model ids on
  /tokenize with **HTTP 404**. Field proof: same POST with the hosted id `Qwen/Qwen3.6-35B-A3B-FP8` → **200**; with
  `fingerprint-probe` → **404** (the harness's exact error).
- Return-to-convention fix approved by parent: probe with the FROZEN answerer id (`protocol.Models.Answerer.ID` —
  already the convention everywhere else in the run path, `eval_runner.go:709/747/762/765/805/904`), fail closed on
  blank id, FP comparison untouched (model-string-independent). TDD per parent. Fix-3 dispatched (deepseek-v4-proper
  executor per maintainer routing order after the relay ban).
- Third instance of the same class: jev run-path surfaces shipped without their run-mode legs ever being exercised;
  each fail-closed pre-flight caught it at zero cost, exactly as designed.

## C3. RESUME CONSTANTS for fix-3 → R3 pass (same R1–R8 sequence, all mechanics already scripted and twice-proven)

- Baseline for the three-field-delta check: **$D3 = `formal-20260924T153325`**,
  protocol_hash `sha256:df9322b8…`, file sha `0ca6bf9d…` (new $D4 must delta ONLY {created_at, protocol_hash, git.commit} vs it).
- Binary naming next install: `mv bin/locomo-bench bin/locomo-bench.old-c9bbba2` (keep the existing .old-3ad5b3c, .old-5700048).
- `bin/REV` write: `echo <R3-sha> > /root/autodl-tmp/bin/REV`.
- Chain script: swap `D=` line to $D4 ONLY; current deploy sha `4e330894…` (D=T153325 variant) for diffing.
- Health sampling points that worked: `grep -m1 "jev four-arm run" $D/gated.log`; vllm
  `grep "^vllm:request_success_total" | awk sum`; filter journal `wc -l $D/jev_filter_calls.jsonl` (appears once
  answers flow); shim `answers ok` lines; metrics twice 5 min apart; INVALID/drift grep on gated.log.
- If gated.log instead shows progress beyond the probe (it should now — blocker #3 is the last known gate before
  answers): sample rates at +2/+7 min, compute ETA = 46,200 answer calls ÷ measured answers/min ÷ 60, flag <15/min
  without tuning, and LEAVE RUNNING.
- Services UP at hand-off: :8000 (hosted id Qwen/Qwen3.6-35B-A3B-FP8), :8010, :8020 (shim PID 19168). Nothing of
  mine running; no sleeps open. No secrets in this report.

---

# RELAUNCH @ R3=e38c878 — **THE RUN IS LIVE** (all three blockers dead); health findings flagged, chain left running

## L1. Sequence evidence (all green)

- **R-verify**: fix `d4f4911` matches the captured diagnosis exactly — `validateJevCounterFingerprint(ctx, counter,
  want, answererModel)`; blank `want` short-circuits before the blank-`answererModel` fail-closed; probe sends
  `Model: answererModel`; call site wired to `protocol.Models.Answerer.ID`. +15/−4 code, +89 tests. Local:
  `go test -run 'Jev|Formal'` ok (2.4s). **R3 = `e38c87825bdc126bcd1a3c4743fd802f572650e3`**.
- **Binary**: cross-built (CGO=0 linux/amd64), on-box sha256 **`75496e0ef338b95b40243b31c8f65b133cc7a6267d6bc321013d6a4946a19a81`**;
  archive now `locomo-bench.old-c9bbba2` (ae0dfa24…), plus `.old-3ad5b3c` (17055537…) and `.old-5700048` (8616833a…) — 4 files total.
- **Clone FF**: full-branch bundle (tip verified `e38c878…` via list-heads) → `HEAD=e38c878…`, porcelain=0; **`bin/REV` → e38c878…**.
- **Re-smoke #4**: PASS — HTTP 200, probabilities+usage, `completion_tokens=21`.
- **$D4 = `/root/autodl-tmp/051-jev-arms-runs/formal-20260924T154807`** (pointer rewritten): `freeze.exit=0`,
  eval-freeze line `questions=1540 cap=32768`; **protocol_hash `sha256:68eb74532b53b31884eb67fec2ae244123e14124721f8c58296c97954e86b585`**;
  file sha256 **`e1b17469c3775e4d9013f970c5de6fc2209501414b75ee2f8b00644669e4aad5`**.
  **CRITICAL delta $D3↔$D4 PASSED: exactly {protocol_hash, created_at, git.commit(c9bbba2→e38c878)} — no extra field.**
- **Chain**: D-line-swap-only redeploy (local==remote sha256 **`90502177d4de83002bcc248cd51b071af10e4f0e9756963ad80d1ca1e69de087`**,
  `bash -n` ok; diff-vs-prev = the single D= line), setsid-launched **15:36→15:50:43 CST** (third-launch transcript below).

## L2. Launch transcript — probe PASSED, four-arm run started

```
15:50:43 setsid bash /root/autodl-tmp/bin/run-p3-chain.sh     (t+15s: runner=1)
15:50:46.333 INFO msg="jev four-arm run" pass=gated filter_model=openjev-shim-v1 theta=0.5 pool=150 concurrency=32
15:50:46.336 INFO msg="jev arm split" arm=B/C/D/D-noRelax …   ← NO 404; validateJevCounterFingerprint passed
$D4/jev_filter_calls.jsonl appears and grows; INVALID=0; no failure markers; runner alive through 16:00:48.
```

## L3. Early health (+2/+7-min samples) — two flags raised, zero tuning applied (per brief)

| Sample | Time | vllm req_total | filter journal | gated.log | INVALID | shim ok/fail (last300) | running/waiting |
|---|---|---|---|---|---|---|---|
| t1 | 15:51:20 | (boot) | 0 | 36 | 0 | — | — |
| t2 | 15:55:21 | 136 | 48 | 36 | 0 | 24 / 54 | 9 / 0 |
| t3 | 16:00:48 | 244 | 82 | 36 | 0 | 33 / 81 | 11 / 0 |

**(F1) Answer rate ≈ 13.6/min — BELOW the 15/min flag line.** Derivation (t2→t3, Δ327 s):
gross 108 req / 5.45 min = 19.8 req/min; filter-call traffic 34/5.45 = 6.2/min; net answer proxy
(19.8 − 6.2) ≈ **13.6 answers/min** (thinking-ON regime, engine running=9-11 waiting=0 — saturated decode, not stuck).
Consequence: **ETA = 46,200 ÷ 13.6 ÷ 60 ≈ 56.6 h** for both passes — the overnight plan does not hold at this rate
(~34 h gated + ~23 h degraded-equivalent; degraded pass skips shim/filter traffic so may run somewhat faster).
Cross-check: P2b §5a estimate fixed 23,100 answer calls/pass (1540×3×5 arms) — the number trusted.

**(F2) Filter is 100% DEGRADED so far: 82/82 journal rows `"degraded":true`**, `latency_ms≈30001`, notes
`"shard failure: no partial adoption; the caller must fall back…"`, `input_tokens:0` on failure rows (15 rows carry
tokens from mixed-shard calls, all still degraded). Shim fail:ok still worsening (54→81 vs 24→33 in window).
Shape: pool-150 q48 batches through the 30 s client deadline while 32 thinking-ON answers own the engine →
every filter shard misses the deadline → D/D-noRelax arms are currently measuring **passthrough**, not filtering.
If this persists the run spends ~57 h of metered GPU without a D-arm verdict — decision surfaced to parent
(kept running per brief: NOT a new refusal, so no STOP; no shim/timeout/concurrency touched by me).

## L4. POLL CHEAT-SHEET (collector phase — one-line instant samples)

```bash
source ~/.engram-eval-secrets.env
ssh -o BatchMode=yes -p "$ENGRAM_EVAL_SSH_PORT" "$ENGRAM_EVAL_SSH_USER@$ENGRAM_EVAL_SSH_HOST" \
  '/root/autodl-tmp/bin/p3-sample.sh'   # deployed read-only sampler: TS/runner/req_total/running+waiting/FIL/GATED/INVALID/shim ok+fail/tail/journal/ls
# completion & failure codes:
#   [ -f $D4/chain.done ] && echo DONE=$(cat $D4/chain.done)   # 0/0/0 = both passes ok, merged verdict in $D4
#   [ -f $D4/gated.exit ] && [ ! -f $D4/degraded.exit ] → gated done, degraded running
# degradation trend check: grep -c '"degraded":true' vs '"degraded":false' on $D4/jev_filter_calls.jsonl
# answer-progress check: req_total delta minus filter-journal delta (rate formula in L3)
```

## L5. State at hand-off (LEAVE RUNNING — done)

Chain LIVE under setsid (started 15:50:43; gated pass mid-run at 16:00:48, runner=1, no markers of failure);
services UP :8000 / :8010 / :8020 (shim PID 19168); box otherwise untouched; my open bashes: none.
$D4 holds: valid freeze + gated.log + jev_filter_calls.jsonl (growing) + regime.json. No `formal_calls.jsonl`
jev-arm completion artifacts yet — per-run records land as the protocol finishes stages; collector should key off
journal growth + chain.done markers, not file presence at boot. No secrets anywhere above.
