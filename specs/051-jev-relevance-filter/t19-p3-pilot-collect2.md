# T19 · P3 PILOT — verdict collector #2 (2026-09-28, ~14:25–14:45 CST)

**Task**: collect the gated-pilot verdict from the AutoDL box, compute quick stats, confirm the
billing/shutdown state. **Superseding maintainer order received mid-run (14:2x): the box stays UP —
no shutdown at any step** (parent already killed the box-side watcher PID 4028; the box being up is
intentional, it is needed for the degraded-row 补齐 phase). No shutdown command was issued, and none
is recommended by this report.

- Run dir (box): `D7 = /root/autodl-tmp/051-jev-arms-runs/formal-pilot-20260928T092440`
  (= content of `/root/autodl-tmp/051-jev-arms-runs/CURRENT-P3-D`), launched 09:24:40, gated pass 09:25:11.
- Access: paramiko + password via `~/.engram-eval-secrets.env` (`ENGRAM_EVAL_SSH_*`) — env only, no
  credential in this file, this log, or any tool output. (`ENGRAM_EVAL_SSH_CMD=evalbox` is a stale
  placeholder: no `evalbox` binary/alias/function exists on this machine, so key-auth ssh was not usable.)
- Git state: engine dirs (`memory embedding provider store internal cmd filter`) untouched —
  `git diff --name-only -- …` empty. Nothing staged. Ops-only collection.

## 1. Verdict: chain finished **rc=1**, but the **measurement is complete**

| marker | value | meaning |
|---|---|---|
| `gated.exit` | **1** | gated pass exited non-zero |
| `chain.done` | **1** | driver rc (pilot is gated-only, so identical) |
| `freeze.exit` | 0 | freeze OK: `questions=1540 cap=32768` |
| completion log | `jev four-arm measurement complete rows=7700 filter_calls=3080 retrieval_calls=7700 invalid=false` | **all 7,700 answers measured at 14:32:36** |
| failure line | `locomo-bench: jev four-arm run failed the validity gate: per-repeat validation receipts are incomplete (valid=false complete=false); the four-arm verdict requires evalArtifactValidity.isComplete` | **post-measurement harness gate**, not a model/sidecar failure |

Wall clock 09:25:11 → 14:32:36 = **5 h 07 m 25 s = 25.05 answers/min**, i.e. the launch ETA
(≈26/min → 4.9 h → ≈14:20 ±30 min) was accurate; finish landed inside its own window.

Polling record (instant box-side probes; waits executed on the box, never local sleep loops):
14:26:08 connect+D7 resolve · 14:26:4x state snapshot (no markers) · 14:27:21 rate sample ·
14:31:xx first 580 s box-side bounded wait (`for i in seq 1 58 … sleep 10`) ·
**14:32:44 markers detected 80 s into that wait**. Total collector time ≈ 19 min, inside the 60-min budget.

### Root cause of rc=1 (structural, not pilot-specific) — harness wiring gap

`--jev-arms` returns from `main.go:959-962` (`return runJevArms(...)`) **before** the only code that
writes the receipts the gate demands (`main.go:1181-1201`: `materializeFormalB1Artifacts` →
`validateEvalArtifactRun` → `publishFormalB1Metrics`, which write `summary.json` +
`candidates.jsonl`/`compile_trace.jsonl`/`bundles.jsonl`/`classification.jsonl`), and
`openFormalCallJournal` (`main.go:973`) — so `formal_calls.jsonl` is never opened in arms mode.
The verdict then reads validity from that missing file
(`jevfilter_verdict.go:631-639` → `readJSON` err → zero-value `evalArtifactValidity`) and
`validateJevRunValidity` (`jevfilter_protocol.go:519-533`) refuses it. Its required set
(`jevCoreValidityArtifacts`, `jevfilter_protocol.go:496-505`) includes `formal_calls.jsonl`,
`summary.json`-derived validity and 4 artifact files that the arms path cannot produce.
**A gated four-arm run in a fresh run-dir therefore always exits 1 with `promotion_verdict=INVALID`.**

Independent evidence it's the path, not this run: **every** prior 051 run dir on the box lacks those
receipts (`formal-20260924T154807`, `formal-pilot-20260924T165449`, `formal-20260924T153325` →
`summary.json ABSENT`, `formal_calls.jsonl ABSENT`; the two Sep-24 pilots are rc=143 = SIGTERM'd by
the earlier decision, and 153325 died rc=1 on a different, pre-measurement counter-404 error).

Consequence: the SC numbers below are **complete and internally consistent, but not promotable
evidence** under 051's own fail-closed law. Fixing the seam (write receipts in the arms path, or run
a separate `--formal`-materialize stage over the same run-dir, or re-scope the gate for pilots) is an
explicit decision for the maintainer/planner — it is **not** in this collector's scope and I changed no code.

## 2. Artifacts collected (11 files)

Local: `specs/051-jev-relevance-filter/pilot-artifacts/`, prefix `p3pilot2-` (the Sep-24 pilot's
tracked `gated.log` was accidentally overwritten during fetch and **restored byte-identical from HEAD**
— verified `md5 4d07e891… == git show HEAD:…`; no other agent's artifact was lost).

| file | bytes | what |
|---|---|---|
| `p3pilot2-jev_arm_verdict.json` | 36,003 | the four-arm verdict (SC-001..SC-007 + contrasts + aggregates) |
| `p3pilot2-jev_arms_report.json` | 29,812 | same report block, standalone |
| `p3pilot2-gated.log` | 4,172 | pass log incl. completion + gate-failure lines |
| `p3pilot2-protocol.json` | 3,776 | frozen protocol (`locomo-b1-high`, hash `sha256:29e6bfe9…`, git `7ff5e80`, questions=1540, reps=1, cap=32768) |
| `p3pilot2-regime.json` / `freeze.log` / `freeze.exit` | 188/147/2 | run regime + freeze receipt (rc 0) |
| `p3pilot2-gated.exit` / `chain.done` | 2/2 | markers (1 / 1) |
| `p3pilot2-shutdown-watcher.log` | 131 | only the "watcher started pid=4028" line — it never saw chain.done |
| `p3pilot2-jev_filter_calls.digest.txt` | 4,318 | journal head-5 / tail-5 / row count / degraded / latency bands |

Kept out of the tracked dir (size discipline; they live in gitignored `.scratch/t19-p3c2/` and still on the box):
`jev_filter_calls.jsonl` (1,074,752 B / 3,080 rows), `jev_arms.jsonl` (4,544,541 B / 7,700 rows).
`formal_calls.jsonl` does not exist in this run dir (see §1), so nothing huge was pulled.

## 3. Quick stats

**Answers**: 7,700 / 7,700 expected (1,540 questions × 5 arms × 1 rep) — `rows=7700`, every arm exactly
1,540 rows, `correct_measured` set on all 7,700 (0 unmeasured). Retrieval calls 7,700 = the frozen budget
(`retrieval_call_limit` respected; filter calls are a separate class by design).
**INVALID share**: 40/7,700 rows = 8 distinct questions are `gradeable=false` (**0.52 %**) — the *same*
8 questions in all five arms, so they are denominator-shared, not arm-biased; 35 of the 40 rows are
category 3, 5 are category 1; `budget_stopped=0` everywhere. Verdict-level `invalid_reason` is empty
(the run itself is not invalid — only the *promotion* verdict is INVALID via the §1 gate).

**Filter journal** (3,080 rows = 1,540 questions × {D, D-noRelax}; A/B/C issue no filter call):

| metric | value |
|---|---|
| degraded:true | **25 / 3,080 = 0.81 %** (per-arm: D 12 = 0.78 %, D-noRelax 13 = 0.84 %) — vs the killed Sep-24 pilot's **96 %** degraded. The 503-storm fix held. |
| degraded causes | 20 × `negative cache: endpoint failed within the last 30s; degraded without a network call` (latency 0) · 5 × `shard failure: no partial adoption; the caller must fall back to the fused top-N` (lat 4,059 / 8,583 / 30,000 / 30,001 / 30,001 ms) |
| degraded blast radius | 13 distinct questions touched; all 25 degraded rows fell back to `kept=30` (top-N). Arm D scored 3/13 correct on those questions. |
| latency bands (all rows) | <100 ms 32 — all at exactly 0 ms: 20 negative-cache degraded rows + 12 empty-pool rows (3 questions where retrieval returned nothing, so no call was issued) · 100–499 1 · 500–999 539 · **1,000–1,999 2,005** · 2,000–4,999 486 · 5,000–9,999 9 · ≥10,000 8 (incl. 3 at the 30 s deadline) |
| real-call latency (n=3,048, ≥100 ms) | mean 1,430 · p50 1,169 · p90 2,241 · **p95 2,452** · p99 4,059 · max 30,001 ms |
| tokens | input 102,302,712 · output 22,303,518 (mean 33,215 / 7,241 per row) — 3.3× the Sep-24 "48-question shard" shape, consistent with the frozen 12-question shard clamp (slice 14) |
| selection | mean kept 3.80 · median 2 · kept==0 in **293 rows (9.51 %)** · mean pool 143.8 |
| cost | `usd=0` on every row (typesafe gateway free tier); `unpriced_models: Qwen/Qwen3.6-35B-A3B-FP8, deepseek-flash` |
| integrity | single protocol hash across all rows; schema `022.v1`; reps `{0}`; theta `{0.5}` |

## 4. THE FOUR-ARM VERDICT (from `jev_arm_verdict.json`, 1 rep, pass=gated)

Accuracies (denominator = 1,540 questions; verdict's own convention):

| arm | accuracy | mean shown | mean answer-input tokens | empty-injection | degraded |
|---|---|---|---|---|---|
| A (production RRF limit=8→top-30 shown) | **25.84 %** | 29.67 | 1,421 | 0.39 % | 0 |
| B (wide pool 150, everything shown) | **60.65 %** | 143.75 | 5,811 | 0.39 % | 0 |
| C (pool truncated to first 12) | **18.38 %** | 11.89 | 736 | 0.39 % | 0 |
| D (Jev filter, relax on) | **56.88 %** | 3.81 | 420 | **7.99 %** | 0.78 % |
| D-noRelax (relax stage disabled) | **56.43 %** | 3.78 | 418 | **11.04 %** | 0.84 % |

(Gradeable-only denominators, recomputed locally, shift each figure by ≈0.2 pp and change no conclusion:
A 25.65 / B 60.57 / C 18.21 / D 56.79 / D-noRelax 56.33.)

Paired contrasts (run-internal, exact McNemar):

| control → treatment | Δ pp | McNemar p | paired CI (pp) |
|---|---|---|---|
| **B → D (primary)** | **−3.77** | **3.43e-05** | [−5.52, −2.01] |
| A → C | −7.47 | 1.70e-21 | [−9.03, −5.91] |
| A → D | +31.04 | 2.79e-104 | [+28.43, +33.65] |
| D vs D-noRelax (not a wired contrast) | +0.45 | — | — |

Flip set B→D: **125** questions B-correct/D-wrong vs **67** D-correct/B-wrong.

SC gates as emitted:

| SC | status | detail (verbatim numbers) |
|---|---|---|
| SC-001 (D within 0.5 pp of B, no significant regression) | **hold** | "D −3.77pp vs B (p=0.0000) fails the >= -0.5pp / no-significant-regression gate" |
| SC-002 (budget compression) | **pass** | D shows 3.81 entries = **7.2 %** of B's tokens (gate ≤12 entries, ≤33.3 %) |
| SC-003 (equal-budget recall) | **hold** | D 0.00812 vs C@12 0.00918 (gate) and C@D-shown 0.00820; Δ −0.0073 pp |
| SC-004 (category-5 abstention) | **hold** | category-5 declared block not run (block rows all zero) |
| SC-005 (fallback path) | **hold** | needs the degraded pass (pilot is gated-only) |
| SC-006 (engine parity) | **declared** | test-suite + MCP contract parity evidence, not measured here |
| SC-007 (segment cost/latency) | **pass** | p50 1,169 ms, p95 2,452 ms, $0.000000/call over 3,048 calls — passes only because the gate is the spec's per-call **10 × p95** anomaly rule (2,452 < 3,000 ms); p50 is **3.9×** the 300 ms expectation. Honest read: this is a near-miss, not a clean pass. |
| `verdict` | **HOLD** | any non-pass SC ⇒ HOLD |
| `promotion_verdict` | **INVALID** | `validity_error` = §1's missing receipts (notes: "validity gate failed: the criteria below are not promotable evidence") |

**Headline**: D compresses context 13.8× vs B (5,811 → 420 tokens/answer) and beats production A by
+31.0 pp, but sits **3.77 pp below B with p=3.4e-05** — a *significant* regression, so SC-001 fails
cleanly on the pilot. That is the pilot's job: it says the coverage floor is not yet met at θ=0.5, 1 rep.

## 5. Two data-quality caveats a reviewer must not skip

1. **The recall channel is near-null.** `mean_pool_recall_at_150 = 0.918 %`, and only **1.3 % of
   questions (20/1,534)** have any non-zero pool recall; arm A's shown-recall is the same 0.914 %. If the
   150-wide pool genuinely missed 98.7 % of gold memories, arm B could not be 60.6 % accurate — so the
   gold↔memory linkage (or the store's provenance for this slice) is the likely culprit, not retrieval.
   SC-003 (and `d_gte_c_at_12=false`) is therefore measured on essentially empty evidence and should be
   treated as **uninformative** until that linkage is checked. Arm accuracies are unaffected (they come
   from the answer+judge path).
2. **D's win-rate is bought with empty contexts.** Empty-injection 7.99 % (D) / 11.04 % (D-noRelax) vs
   0.39 % for A/B/C, and 9.51 % of filter rows kept **0** of 150 (median kept 2). The relax stage is
   worth 3.05 pp of empty-injection and ≈0.45 pp of accuracy — the 补齐/后续 knob to look at, together
   with the 0.81 % degraded tail.
3. Minor harness observation for the prompt-side reader: the journal's `memories` field equals `kept`
   in **all 3,080 rows** (never the scored count), so "how many did the filter actually score" is not
   recoverable from the journal; `dropped + kept = pool` is the only usable relation.

## 6. Box state at hand-off — UP, intentionally, per maintainer order

- **No shutdown issued at any step** (superseding order 14:2x). Nothing was armed either: no
  `shutdown`/`poweroff`/`autoshutdown` process, no crontab entry; `pilot-autoshutdown.sh` exists on disk
  but is not running.
- Watcher PID 4028 confirmed **dead** (`ps -p 4028` → gone) before chain.done landed, so its 600 s
  grace never started; its log holds only the 09:25:12 "watcher started" line. Box staying up is the
  intended state, not a watcher failure.
- Services for the 补齐 phase are alive: vllm **PID 1513** (`/v1/models` → HTTP 200), openjev-shim
  **PID 2007**, `vllm.log` + `openjev-shim.log` still appending. The runner (PID 3998) and both driver
  shells (3995/3997) exited normally with the chain.
- Disk is healthy: system `/` **178 M / 30 G (1 %)** — the run dir totals only 5.5 M; data disk
  `/root/autodl-tmp` 49 G / 250 G (20 %). No cleanup pressure; the `AGENTS.md` run-dir-on-data-disk rule
  was honored (`D7` is under `/root/autodl-tmp`).
- Billing note (for the maintainer, not a finding): the metered box is **still running with an idle GPU**
  now that the pilot is finished. Keep-up was explicitly ordered for 补齐; if that phase slips, stop it
  deliberately rather than by accident.

## 7. Next steps (decision belongs to maintainer/planner)

1. Decide the receipt seam (§1): materialize `summary.json` + the 4 artifact files + `formal_calls.jsonl`
   for the arms path, or run a separate materialize/validate stage over the same `D7`, or re-scope
   `jevCoreValidityArtifacts` for pilots. Until then **no four-arm run can ever be promotable**, and the
   full 3-rep formal chain would exit 1 for the same reason — cheap to fix before spending ~15 h of GPU.
2. Investigate the gold-recall linkage before re-reading SC-003 (§5.1).
3. Only then consider the follow-on passes (degraded pass for SC-005, category-5 block for SC-004, and
   the 3-rep canonical run). The 0.81 % degraded tail (13 questions / 25 rows, listed verbatim in
   `p3pilot2-jev_filter_calls.digest.txt` and in `.scratch/t19-p3c2/jev_filter_calls.jsonl`) is the
   backfill 补齐 input — it is small and fully enumerated.

## Appendix — commands used (all timed, instant polls)

```bash
# reachability + D7 resolution
python3 .scratch/t19_p3c2_ssh.py 'echo OK; date +%FT%T%z; cat /root/autodl-tmp/051-jev-arms-runs/CURRENT-P3-D'
# bounded box-side wait (one ssh call, 58×10 s, no local sleep loop)
python3 .scratch/t19_p3c2_ssh.py 'for i in $(seq 1 58); do [ -e "$D/chain.done" ] && break; sleep 10; done; …'
# artifact fetch (SFTP, small set) + journal digest for the tracked dir
python3 .scratch/t19_p3c2_fetch.py "$D7" specs/051-jev-relevance-filter/pilot-artifacts \
  gated.exit chain.done gated.log regime.json protocol.json freeze.log freeze.exit \
  shutdown-watcher.log jev_arm_verdict.json jev_arms_report.json
```
Helpers: `.scratch/t19_p3c2_ssh.py`, `.scratch/t19_p3c2_fetch.py` (gitignored scratch; credentials read
from env only). Raw journals + local analysis copy: `.scratch/t19-p3c2/`.
