# T19 P3 PILOT — slices 12+13 deployed & proven; pilot LAUNCHED, then **STOPPED per step-5 gate (decision A)** — box auto-shutdown armed

**Bottom line:** everything the pilot required was deployed and validated at HEAD `882c75f` — s12 shim **separation smoke PERFECT**, s13 `--jev-arms-reps 1` freeze passed with an **exactly-legitimate $D4↔$D6 protocol delta**, gated-only chain launched clean. The pilot then **failed the step-5 acceptance gate for two independent reasons** (gateway 503 storm ⇒ ~96 % degraded filter rows; vllm answer completions **frozen ≥ 9 min**, ≈0–1.2 answers/min vs the 15/min flag line). Supervisor decision **A**: collect evidence → kill by PID → auto-shutdown watcher fires (billing stops ~17:26; data persists). **No box-side debugging, no tuning.** No secrets anywhere (key referenced by `$AI_GATEWAY_API_KEY` / `.jev-env.sh` only; value never printed — log scans verified 0 occurrences).

Box CST 2026-09-24, sprint window 16:47–17:17.

## 1. Deploy @ 882c75f — DONE (both binaries + clone)

| item | value |
|---|---|
| local HEAD | `882c75f1bf5a864de184ae0cc7f0c8f60ac0b552` (4ef97d7 fix + 882c75f docs; e22ee82 s12 inside); `git status --porcelain -- cmd/` empty; engine untouched (`git status --porcelain -- cmd/ memory/ embedding/ provider/ store/ internal/ filter/` = 0 lines at end) |
| shim (s12 self-contained questions) | build sha256 `91d02cc9f8611783210774f79b36c03446c7e72a494a469012fe1bb9d517c816` → box `bin/openjev-shim`; previous pilot shim preserved as `openjev-shim.old-239f573` (`9a147d12…`); kill-by-PID 24614 → relaunch `set -a; . /root/autodl-tmp/.jev-env.sh; set +a; OPENJEV_UPSTREAM=typesafe OPENJEV_MAX_QUESTIONS=256 setsid bin/openjev-shim --port 8020 >>logs/openjev-shim.log` → **PID 25741**, healthz `ok` |
| startup line (verbatim, key-free; pre-kill log scan `key-occurrences=0`) | `2026-09-24 16:53:59 listening on http://127.0.0.1:8020 (upstream=typesafe, endpoint https://ai-gateway.vercel.sh/v4/ai/evaluation-model, model typesafe-ai/jev, max_questions=256)` |
| bench (s13 reps gate) | build sha256 `f9324ce4ec2b7a4147aa3c64b871455b31f15977116683b0f3066c86108d91c6` → box `bin/locomo-bench`; e38c878 build preserved as `locomo-bench.old-e38c878` (`75496e0e…`). **This deploy makes the earlier "runner unchanged" moot — `cmd/locomo-bench` DID change (4ef97d7), so the binary was genuinely rebuilt + redeployed** |
| clone ff | bundle `--branches --tags` @ 882c75f, sha256 `41659d1f7796fc5845b923839479bfa439074baa4015e6c98a1b945eec9e7040` (local == remote); `merge --ff-only` → **HEAD `882c75f1…`, porcelain = 0 lines, `bin/REV` = full sha** |

## 2. CALIBRATION SEPARATION SMOKE (s12 decisive) — **PERFECT PASS**

Fixture `/root/autodl-tmp/scratch/p3-pilot-calib-sep4.json` (sha256 `cc530c384727544f1e4f27424717e8b1cd31d49e328a3986b860e8b25dc02f95`): query *"What is Ana's favorite food?"*, m0=Ana/pizza (relevant), m1=Bob/chess, m2=Ana/dog Rex, m3=France/Paris (irrelevant).

HTTP **200**, latency **5.43 s**, full body verbatim:

```json
{"probabilities":{"need_m0":1,"need_m1":0,"need_m2":0,"need_m3":0},"usage":{"prompt_tokens":879,"completion_tokens":123}}
```

**Binary separation exactly as designed** (m0=1.0 high; m1/m2/m3=0.0 low) — the slice-12 self-contained question binding fixed the zero-separation finding from the previous report (old shim: 0.97/0.98 on the analogous case). Gate to proceed: OPEN.

## 3. Pilot freeze $D6 — PASSED; protocol delta EXACTLY legitimate

$D6 = `/root/autodl-tmp/051-jev-arms-runs/formal-pilot-20260924T165449` (CURRENT-P3-D rewritten to it — pointer now = $D6). Command = runbook §6 step-1 with the three authorized substitutions: `--repeats 1` + `--jev-arms-reps 1` + `--jev-filter-model typesafe-ai/jev`; everything else identical (four declarations, `--eval-budget-profile high`, `--answer-input-cap=32768`, FP `sha256:4806660dd8…`, token-counter :8000/v1).

- `freeze.exit=0`; freeze.log verbatim: `eval-freeze: protocol=locomo-b1-high output=$D6/protocol.json questions=1540 cap=32768`
- **protocol_hash `sha256:a602653ccdf254b60958feb96e304e504865a71e348441280b6751c6f9269c86`**; protocol.json file sha256 `49c62454dac9a982bf484b55c5659088e12a8022c0a9ba9f1fddb8e4badbafa0`
- **$D4↔$D6 flattened-key diff = 6 fields, all legitimate:** `created_at`; `protocol_hash`; `git.commit` (e38c878→882c75f); `aggregation.answer_repetitions` 3→1; `experiment.filter.answer_repetitions` 3→1 (the filter-registration field encoding the declared reps — allowed by the delta set); `experiment.filter.filter_model` openjev-shim-v1→typesafe-ai/jev. **Nothing else.** No STOP.

## 4. Pilot chain (gated ONLY) — launched, then STOPPED per gate

`/root/autodl-tmp/bin/run-p3-pilot.sh` — sha256 `e465e8d3d9113ddccb868de1660cac5c21816efec3169aa8af78f8e5ba498c4e` **local == remote**, `bash -n` OK both sides, flags per contract (`--jev-arms --jev-arms-reps 1 --jev-filter-model typesafe-ai/jev --repeats 1` + four declarations + `--eval-protocol $D6/protocol.json` + store-dir/chunks/no-idk-retry/chunk-quota=0/top-k 30/token-counter/concurrency 32), markers `$D6/gated.exit` + `$D6/chain.done`. **Launched setsid 16:55:31** (no stray runs pre-launch; runner PID 25975, drivers 25973/25974).

Launch transcript (gated.log): `starting conversations=10 arms=[hybrid] concurrency=32 model=Qwen/Qwen3.6-35B-A3B-FP8 … judge_model=deepseek-flash top_k=30` → `reusing persisted extraction` ×10 (zero extraction spend) → `jev arm split` ×5 → **`jev four-arm run pass=gated filter_model=typesafe-ai/jev theta=0.5 pool=150 concurrency=32`** (the required pass line — my earlier grep missed it only due to the `msg="…"` quoting).

## 5. Early health — **BOTH STOP conditions TRIGGERED** (capture, no debug)

**(H1) Filter rows still overwhelmingly degraded:true.** Final journal: **214 rows = 205 `degraded:true` / 9 `degraded:false` (≈4 % healthy)**. Degraded reasons: `shard failure: no partial adoption…` (~30, latency 4–12 s) + `negative cache: endpoint failed within the last 30s; degraded without a network call` (latency_ms=0) — one 503 shard poisons the whole 150-pool call AND the 30 s negative cache degrades neighbors **without even trying** (note: `memories` field is post-shard; pool 150 → ~4 shard calls of ≤48 each, so one failing shard ⇒ whole-row degrade). Shim counters for the session: `attempts=1` ok ×66, `attempts=2` ok ×18, **`attempts=3` FAILED ×94+** (`failed_total=84+` lines, all `status 503 Service temporarily unavailable`) — the gateway sheds the ~32 k-token 48-question shard calls at concurrency 32 (small 6-question calls succeed in 0.8 s; separation smoke at 879 prompt tokens succeeded attempts=1).

The 9 healthy rows ARE correctly shaped (input_tokens>0, NOT timeout-shaped) — 3 samples verbatim (hist 0–1 s ×2, 2–3 s ×1, 3–4 s ×2, 5–6 s ×3, 6–7 s ×1):

```
{"…","conv":8,"q":1,"arm":"D","pool":150,"memories":6,"kept":6,"dropped":144,"latency_ms":5098,"input_tokens":32072,"output_tokens":7570,"degraded":false}
{"…","conv":8,"q":1,"arm":"D-noRelax","pool":150,"memories":6,"kept":6,"dropped":144,"latency_ms":2488,"input_tokens":32072,"output_tokens":7570,"degraded":false}
{"…","conv":4,"q":4,"arm":"D","pool":150,"memories":8,"kept":8,"dropped":142,"latency_ms":3178,"input_tokens":32069,"output_tokens":7605,"degraded":false}
```

**(H2) Answer rate collapsed.** `vllm:request_success_total{finished_reason="length"}` = **21 at 17:04:58, STILL 21 at 17:13:48 — zero completions for ≥ 9 min** with `num_requests_running≈8–10, waiting=0` (pipeline wedged, not queued). Cumulative ≈21 answers in 18.5 min ≈ **1.2–2.3 answers/min ≪ 15/min flag line** (flagged, NOT tuned, per instruction). **ETA: N/A — pilot was invalid** (D-arm ≈ fused top-N fallback 96 % of the time ⇒ cannot measure the D ≥ B − 0.5pp gate). No INVALID/drift-storm in gated.log (0 error lines) — the run was still "validly" progressing, which is exactly why the ops gate, not the harness, had to stop it.

## 6. Disposition (supervisor DECISION A, executed in order)

1. **Evidence collected to LOCAL first** (box up): `specs/051-jev-relevance-filter/pilot-artifacts/` — `gated.log` (full, 36 L), `jev_filter_calls.digest.txt` (wc + head-20 + tail-20 + degraded counts + latency histograms both classes), `shim-log.tail200.txt` (tail-200 + attempts/status counters), `prekill-snapshot.txt` (vllm metrics + pgrep -a + markers, timestamped 17:1x). Freeze-window **17:04:58 → 17:13:48** annotated above. (Untracked, unstaged — `git diff --cached` empty.)
2. **Kill by PID**: `kill 25975` → markers landed `gated.exit=143`, `chain.done=143`; drivers exited (independent verify: no `run-p3-pilot` processes remain — the lone "2" was my own ssh pattern self-match); `locomo-bench=0`. No `pkill -f`.
3. **Watcher LEFT ARMED (not killed)**: PID 26350 logged `17:16:24 chain.done detected rc=143; grace 600s for artifact collection` → box powers off **≈ 17:26 CST**; `/root/autodl-tmp` persists. **The collector at 01:05 will find the box down — that is by design.** Shim 25741 + services simply die with the instance.
4. Not waited on: the shutdown itself. Do NOT ssh expecting the box up after ~17:26.

## 7. Poll cheat-sheet (post-mortem / reuse)

```bash
D=$(cat /root/autodl-tmp/051-jev-arms-runs/CURRENT-P3-D)   # now $D6 (pilot, rc=143 stopped)
[ -f $D/chain.done ] && echo DONE rc=$(cat $D/chain.done)  # 143 = SIGTERM'd by decision A; no verdict files expected (killed mid-run)
# gated.done semantics (1-rep): gated.exit=0 + single gated verdict JSON; degraded pass absent in pilot.
grep -c '"degraded":false' $D/jev_filter_calls.jsonl       # ratio is the F2-style health signal
curl -s -m5 http://127.0.0.1:8000/metrics | grep 'request_success_total.*length'   # the rate truth (static counter = wedge)
grep -o 'attempts=[0-9]*' /root/autodl-tmp/logs/openjev-shim.log | sort | uniq -c  # gateway 503 pressure
```

## 8. What this pilot PROVED (carries forward) vs. what it KILLED

- **Proven:** s12 separation (binary, perfect); s13 reps-gate freeze + protocol semantics (exact 6-field delta); full ops pipeline (deploy→freeze→launch→measure→stop) clean, zero extraction spend, ~550 gateway calls max before stop.
- **Killed (the real finding):** the **typesafe gateway at ~32-way × 48-question shard load 503-storms** (~50 % of calls fail after retries) — a **capacity/load-profile problem, not a wiring problem**. Any P4 retry needs the parent to decide among: lower `--concurrency`, larger `OPENJEV`-side negative-cache/retry tuning (harness), gateway quota/plan, or shard-size policy in `filter/jev` (default 48, clamp [32,64]) — all out of scope here by decision A.
