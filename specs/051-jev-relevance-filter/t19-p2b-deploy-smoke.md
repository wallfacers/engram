# T19 P2b — deploy + smoke (box: AutoDL, answerer Qwen3.6-35B-A3B-FP8 @:8000, embed bge-large-en-v1.5 @:8010)

**Status**: deploy ✅ · shim launched ✅ · pointer smoke **HOLDED** (root cause found, code fix P1.5c in flight) · 5a estimate ✅ (exact match) · 5b freeze ✅ (after recorded refusals) · 5b model legs **HOLDED by parent** (throwaway run must wait for the P1.5c binary).
**Date**: 2026-09-24 · **Repo rev**: `573ae167c8441c51a1104f96cfaac8e329a90484` (branch `codex/051-jev-relevance-filter`) — verified at every step below.
**Services left UP** (P3 imminent): `:8000` 200, `:8010` 200, shim PID **14982** healthz `ok`.

---

## 1. Binaries (step 1) — DONE

Cross-compiled locally from the clean tree at 573ae16 (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`):

| file | size | sha256 |
|---|---|---|
| `/root/autodl-tmp/bin/locomo-bench` | 18,966,460 | `8616833a7d094f2e280845663517c4350c8cb7b378eb8baa14662d482148223c` |
| `/root/autodl-tmp/bin/openjev-shim` | 9,289,072 | `6ef3940c9927455db1864b08f034c964977c0f20e1bcacc62c567e6ebaea094e` |
| `/root/autodl-tmp/bin/REV` | 41 B | `573ae167c8441c51a1104f96cfaac8e329a90484` |

## 2. Clean clone on the box (step 2) — DONE

`git bundle create … codex/051-jev-relevance-filter` (complete history, 6,368,773 B) → scp →
`git clone -b codex/051-jev-relevance-filter /root/autodl-tmp/engram.bundle /root/autodl-tmp/engram`.
(The plain `git clone <bundle>` fails "remote HEAD refers to nonexistent ref" — single-ref bundles carry no HEAD; the `-b` form is the fix, recorded for future workers.)

Proof at `/root/autodl-tmp/engram`: `git log --oneline -1` → `573ae16 docs(051): T19 ops records …`; `git status --porcelain` → **empty**; `git rev-parse HEAD` → `573ae16…0484`.

## 3. Shim launch (step 3) — DONE

Env `OPENJEV_UPSTREAM_BASE_URL=http://127.0.0.1:8000/v1` · `OPENJEV_UPSTREAM_MODEL=Qwen/Qwen3.6-35B-A3B-FP8` · `OPENJEV_MAX_QUESTIONS=256` · `--port 8020`, setsid-detached, log `/root/autodl-tmp/logs/openjev-shim.log`.

```
2026-09-24 13:41:56 listening on http://127.0.0.1:8020 (upstream http://127.0.0.1:8000/v1, model Qwen/Qwen3.6-35B-A3B-FP8, max_questions=256)
$ curl localhost:8020/healthz  →  ok
```

(Launch hygiene: one duplicate launch attempt exited cleanly with "bind: address already in use" — the log carries that line; PID 14982 is the single live holder. `.exit` marker convention kept for the next launch.)

## 4. Pointer smoke + reasoning edge (step 4) — 502 reproduced, root cause measured, HOLDED for P1.5c

Contract §2 request (`/root/autodl-tmp/scratch/p2b-pointer-smoke.json`: query "What food does Ana like?", memories m0 "Ana loves pizza…"/m1 "Bob plays chess…", noul questions `need_m0`/`need_m1`, model `openjev-smoke`):

```
$ curl -X POST localhost:8020/answers --data @p2b-pointer-smoke.json
{"error":{"message":"upstream reply contained no JSON object answering the questions (finish_reason=length)"}}
HTTP=502 TIME=1.97s
```

**Empirical upstream forensics** (direct `chat.completions` probes, no source reading):

| probe | max_tokens | kwargs | result |
|---|---|---|---|
| A (shim-shaped) | 72 | default | `finish_reason=length`; `message.content` = prose "Here's a thinking process:…" (304 chars in); **no `reasoning_content` key** — this vllm serves NO reasoning parser; Qwen3.6-FP8 free-formats thinking **inline in content** |
| A′ (same prompt) | 2048 | default | `finish_reason=stop`; content 2,497 chars, **ends** `…
\n\n{"need_m0": 1.0, "need_m1": 0.0}` — valid JSON exists but only after ~2.4k chars of thinking; the stray closing `……… {\"enable_thinking\": false}` → vllm **exits 2**: `error: unrecognized arguments` — the flag does not exist in this CLI (parent `--help`-confirmed)
- 13:52 worker base64-patched `serve-051.sh` with the correctly-quoted flag (backup `serve-051.sh.bak-p2b`) — moot: the flag itself is dead; **and killed old answerer PIDs 15277/15278** (pre-order).
- Parent **restart3 launched WITHOUT the flag → :8000 = 200** (Qwen served; embed :8010 never went down). Worker-observed: `8000=200`, `8010=200`, shim `ok`.
- Cleanup verified: `grep -c chat-template-kwargs serve-051.sh` = **0**, `bash -n` clean.

**Net**: pointer smoke CANNOT pass on the current 573ae16 shim binary (budget + inline-thinking), regardless of vllm config — accepted as a **code slice (P1.5c)**, not an ops condition. Shim + upstream left running for the re-smoke.

## 5a. `--estimate --jev-arms` (step 5a) — PASS, exact match

Run on the box from CWD `/root/autodl-tmp/engram`, boxed env §7:

```
estimate jev-arms: questions=1540 repetitions=3 arms=A,B,C,D,D-noRelax
estimate jev-arms: answer_calls=23100 judge_calls=23100 filter_calls=9240 filter_in_tokens=166320000
  per-arm: A/B/C/D/D-noRelax each answer=4620 judge=4620; filter=4620 only on D and D-noRelax
estimate jev-arms: frozen answer_input_cap=32768 repetitions=3 empty_injection_floor=0.50
estimate jev-arms: unpriced model=Qwen/Qwen3.6-35B-A3B-FP8 | deepseek-flash | openjev-shim-v1 → estimated_usd=0.000000
```

**23100 / 23100 / 9240 — matches the expected totals exactly.** Deviations to note: USD is honestly 0 (no price table entries for the three models — local eval models; spend = judge tokens only, unpriced here); `filter_in_tokens=166.32M` is the nominal 120 tok/candidate estimate, not measured; legacy line reports `extract_calls=288` (P0.5 measured reality is 272, and P3 reuses the stores → 0 extraction calls).

## 5b. Freeze (step 5b) — PASS after recorded refusals; model legs HELD

Exact refusals, in order (each verbatim, each fixed by the named prerequisite):

1. `the 038 pilot gate must be confirmed for a four-arm run (--jev-pilot-gate-confirmed)`
2. `--eval-budget-profile must be low or high`
3. `--answer-input-cap and --counter-fingerprint are required for formal protocol freeze` — **persists even with a 64-hex fingerprint**: `isDigest()` requires the literal **`sha256:` prefix**. The corrected form passed.

Success (zero model calls; worktree proven clean):

```
eval-freeze: protocol=locomo-b1-high output=/root/autodl-tmp/051-jev-arms-runs/smoke-20260924T140158/protocol.json questions=1540 cap=32768
```

Artifact: `protocol.json` 3,775 B · sha256 `b29975a74e75b562a85b0803f47c48d6afaa7dfeb454dd6e60ad7671e9b81cf9` · `protocol_hash` `sha256:25a460c471af3f61328b5c0a769204d8839b40dc3f4b50f64e42ad498671a5ef` · `git:{commit:573ae16…,dirty:false}` · filter registration `filter.jev.v1 / openjev-shim-v1 / 127.0.0.1:8020 / theta 0.5 / relax 0.35≤3 / k_show_max 12 / pool 150 / cap 32768 / reps 3 / arms A,B,C,D,D-noRelax / three declarations true`.

**Honest caveats (smoke-scoped)**: the smoke manifest's `--counter-fingerprint` is a **placeholder digest**, not a `--token-counter-calibrate` artifact; the three `--jev-*-declared` flags were carried mechanically for smoke. **P3 must re-freeze** with the calibrated fingerprint and the genuine operator receipts.

The 2-question gated run + degraded pass (filter journal with non-zero InputTokens, INVALID-partial verdict) is **HELD per parent order** — it would just re-probe the broken shim budget. No `smoke-<ts>/jev_arms.jsonl` / `jev_filter_calls.jsonl` exists yet by design. **answers/min remains unmeasured** (feed the slow-run manual baseline check at P3 start; expected order ~52 answers/min per `docs/operations/evaluation/autodl-slow-run-troubleshooting.md` at full concurrency).

## 7. Exact P3 command block (four-arm run, gated + degraded passes; merge is automatic)

Merge note: the second pass in the **same run-dir** loads the prior pass's verdict and writes the merged two-pass verdict (`mergedJevArmVerdict`) — there is no separate merge command.

Box env file (0600, out of repo; judge secrets live-valued at deploy, never committed): `/root/autodl-tmp/.p2b-env.sh` —
`LOCOMO_PROVIDER=openai LOCOMO_BASE_URL=http://127.0.0.1:8000/v1 LOCOMO_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 EXTRACT_MODEL=$LOCOMO_MODEL LOCOMO_API_KEY=local-eval EMBED_BASE_URL=http://127.0.0.1:8010/v1 EMBED_MODEL=BAAI/bge-large-en-v1.5 EMBED_API_KEY=local-eval JUDGE_PROVIDER=anthropic JUDGE_BASE_URL=… JUDGE_MODEL=… JUDGE_API_KEY=… ENGRAM_JEV_BASE_URL=http://127.0.0.1:8020 ENGRAM_JEV_MODEL=openjev-shim-v1 ENGRAM_JEV_API_KEY=openjev-local` (all exported).

```bash
# 0) deploy the P1.5c-fixed shim binary, relaunch shim (kill old PID first):
kill 14982; mv /root/autodl-tmp/bin/openjev-shim /root/autodl-tmp/bin/openjev-shim.old-573ae16   # scp new binary in place
export OPENJEV_UPSTREAM_BASE_URL=http://127.0.0.1:8000/v1 OPENJEV_UPSTREAM_MODEL=Qwen/Qwen3.6-35B-A3B-FP8 OPENJEV_MAX_QUESTIONS=256
setsid bash -c '/root/autodl-tmp/bin/openjev-shim --port 8020 >>/root/autodl-tmp/logs/openjev-shim.log 2>&1; echo $? > /root/autodl-tmp/logs/openjev-shim.exit' </dev/null >/dev/null 2>&1 & disown
curl -s localhost:8020/healthz    # then ONE pointer smoke POST (see §4); NOTE client 30s negative cache after any failed call

# 1) (re-)freeze from a clean clone of the PROTOCOL commit, with the CALIBRATED fingerprint:
cd /root/autodl-tmp/engram   # git status --porcelain MUST be empty; HEAD = frozen protocol commit
set -a; . /root/autodl-tmp/.p2b-env.sh; set +a
TS=$(date +%Y%m%dT%H%M%S); D=/root/autodl-tmp/051-jev-arms-runs/formal-$TS; mkdir -p $D
FP=sha256:<64-lowercase-hex>   # from --token-counter-calibrate output artifact (prefix is MANDATORY)
/root/autodl-tmp/bin/locomo-bench --data /root/autodl-tmp/locomo.json --dataset-format locomo \
  --run-dir $D --store-dir /root/autodl-tmp/051-stores/stores \
  --retrieval hybrid --repeats 3 --no-idk-retry --chunk-quota=0 --chunks --top-k 30 \
  --jev-arms --jev-filter-model openjev-shim-v1 \
  --jev-b0-continuity-declared --jev-pilot-gate-confirmed --jev-warmup-disposed --jev-same-window-reps \
  --eval-freeze-protocol $D/protocol.json --eval-budget-profile high \
  --answer-input-cap=32768 --counter-fingerprint=$FP \
  --token-counter-base-url http://127.0.0.1:8000/v1
# expect: eval-freeze: protocol=locomo-b1-high … questions=1540 cap=32768

# 2) GATED pass (detached; policy envs only if deviating from frozen 0.5/0.35/3/12):
setsid bash -c 'set -a; . /root/autodl-tmp/.p2b-env.sh; set +a; cd /root/autodl-tmp/engram && \
  /root/autodl-tmp/bin/locomo-bench --data /root/autodl-tmp/locomo.json --dataset-format locomo \
  --run-dir $D --store-dir /root/autodl-tmp/051-stores/stores \
  --retrieval hybrid --repeats 3 --no-idk-retry --chunk-quota=0 --chunks --top-k 30 \
  --jev-arms --jev-filter-model openjev-shim-v1 \
  --jev-b0-continuity-declared --jev-pilot-gate-confirmed --jev-warmup-disposed --jev-same-window-reps \
  --eval-protocol $D/protocol.json --token-counter-base-url http://127.0.0.1:8000/v1 \
  --concurrency 32 > $D/gated.log 2>&1; echo $? > $D/gated.exit' </dev/null >/dev/null 2>&1 & disown
# poll: [ -f $D/gated.exit ] && cat $D/gated.exit || tail -2 $D/gated.log   (instant polls only; rate baseline ~52 answers/min)

# 3) DEGRADED pass (same run-dir; no filter configured — SC-005/SC-006):
setsid bash -c 'set -a; . /root/autodl-tmp/.p2b-env.sh; set +a; unset ENGRAM_JEV_API_KEY; cd /root/autodl-tmp/engram && \
  /root/autodl-tmp/bin/locomo-bench --data /root/autodl-tmp/locomo.json --dataset-format locomo \
  --run-dir $D --store-dir /root/autodl-tmp/051-stores/stores \
  --retrieval hybrid --repeats 3 --no-idk-retry --chunk-quota=0 --chunks --top-k 30 \
  --jev-arms --jev-degraded-pass --jev-filter-model openjev-shim-v1 \
  --jev-b0-continuity-declared --jev-pilot-gate-confirmed --jev-warmup-disposed --jev-same-window-reps \
  --eval-protocol $D/protocol.json --token-counter-base-url http://127.0.0.1:8000/v1 \
  --concurrency 32 > $D/degraded.log 2>&1; echo $? > $D/degraded.exit' </dev/null >/dev/null 2>&1 & disown
# merged two-pass verdict lands in $D automatically at the end of the degraded pass
# (jev verdict artifact + INVALID/VALID per cohort-coverage rule).

# 4) idle-box discipline: after both passes, if P4 (scoring) is not immediate, SHUT DOWN the paid GPU box (Constitution: 空闲必停).
```

## 8. Deviations / notes

1. Pointer smoke and 5b model legs held per parent steering (P1.5c code slice pending) — the empirical case for that call is in §4.
2. My §4-era `serve-051.sh` flag patch (later proven a dead flag) was removed; script verified flag-free + `bash -n` clean; backup retained.
3. One stray old answerer kill (15277/15278) during the pre-order remedy window; parent restart3 restored service; embed untouched throughout.
4. Smoke manifest carries a placeholder `counter-fingerprint` + mechanical declarations — **not** valid for the scored P3; re-freeze per §7-1.
5. Disk check: `/root/autodl-tmp` 20% used at deploy; no cleanup needed.
6. Bundle clone needs `-b <branch>` (single-ref bundle has no HEAD) — recorded for future deploys.
