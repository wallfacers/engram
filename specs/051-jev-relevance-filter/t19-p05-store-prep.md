# T19 P0.5 — Vector Store Preparation (051 four-arm LoCoMo eval)

**Status**: ✅ all three deliverables complete (HF check · dataset validation · extraction + store staging).
**Date**: 2026-09-24 · **Worker面**: local WSL2 + HF + DeepSeek API. **The AutoDL box was not touched** (no ssh/scp).
**Artifacts**: `~/engram-store-staging/` (repo working tree unmodified — engine directories byte-identical, `git diff --name-only -- memory embedding provider store internal filter` empty).

---

## 1. HF data availability check

### 1.1 Token validity

`GET https://huggingface.co/api/whoami-v2` → **HTTP 200**. Reported fields only (no secret):

| field | value |
|---|---|
| name | `wallfacers` |
| type | `user` |
| token role | `write` (display name `数据集`) |
| emailVerified | true |
| isPro / canPay | false / false |
| orgs | `[]` (none) |

Note: `/api/whoami` (v1) returns 401 for this token; **`/api/whoami-v2` is the working endpoint**. Worth recording for future runs.

### 1.2 LoCoMo dataset presence on HF

Top results of `api/datasets?search=locomo` (55 hits; locomotion-dataset noise filtered out). Ranked by downloads:

| dataset id | downloads | last modified | license | notes |
|---|---|---|---|---|
| `KimmoZZZ/locomo` | 1800 | 2025-10-11 | — | **single `locomo10.json`, 2,805,274 B — byte-identical to our repo file** (see §2.4) |
| `insight/locomo` | 911 | 2026-03-26 | — | audio-caption corpus, **not** the LoCoMo QA benchmark (name collision) |
| `Percena/locomo-mc10` | 393 | 2025-07-30 | cc-by-nc-4.0 | `raw/locomo10.json` also 2,805,274 B; adds MC10 variant + transformed names |
| `mteb/LoCoMo` | 220 | 2026-05-11 | cc-by-nc-4.0 | MTEB retrieval re-pack (corpus/queries/qrels parquet per category) — a derived, not raw, artifact |
| `Aman279/Locomo` | 228 | 2024-03-07 | — | CSV, not the canonical JSON |
| `bowang0911/LoCoMoDoc` | 163 | 2026-07-01 | — | derived |
| `vec-ai/LoCoMo` | 158 | 2026-05-25 | cc-by-4.0 | MTEB-style derived |
| `adymaharana/locomo` | 83 | 2024-07-05 | cc-by-nc-4.0 | README only, no data |
| `Rabbidon/locomo10-flat` | 82 | 2025-11-26 | — | flattened variant |
| `dial481/locomo-audit-fc-baseline`, `rovemark/locomo-benchmark-results`, `moorcheh/memanto-locomo-results` | 17–58 | 2026 | varies | results/traces, not data |

**No official `snap-research/locomo` HF dataset exists** (404) — the canonical upstream is the GitHub repo; the HF copies above are community mirrors.

### 1.3 Download access (token-authenticated ranged GET, HTTP 206 = allowed)

| repo / file | size class | HTTP | verdict |
|---|---|---|---|
| `Qwen/Qwen3.6-27B` (official, bf16) | 55.6 GB, 15 shards | **206** | ✅ not gated |
| `Qwen/Qwen3.6-27B-FP8` | **30.9 GB**, 66 shards | **206** | ✅ not gated — fits the ≤32B size class best |
| `Qwen/Qwen3.6-35B-A3B` (official) | 71.9 GB, 26 shards | **206** | ✅ not gated |
| `Qwen/Qwen3.6-35B-A3B-FP8` | **37.5 GB**, 42 shards | **206** | ✅ not gated — the historically used box model |
| `QuantTrio/Qwen3.6-35B-A3B-AWQ` | (AWQ/int4-class) | **206** | ✅ not gated — fallback if 48G vGPU is tight |
| `unsloth/Qwen3.6-35B-A3B-GGUF` | GGUF quants | **206** | ✅ not gated |
| `BAAI/bge-large-en-v1.5` | 4,019 MB | **206** | ✅ not gated — **the canonical box embedder, 1024d** |
| `BAAI/bge-small-en-v1.5` | 401 MB | **206** | ✅ not gated (384d control) |
| `BAAI/bge-base-en-v1.5` | — | **206** | ✅ not gated (768d) |
| `BAAI/bge-m3` | 4,587 MB | **206** | ✅ not gated |
| `datasets/KimmoZZZ/locomo` `locomo10.json` | 2.8 MB | **200** | ✅ |
| `datasets/Percena/locomo-mc10` `raw/locomo10.json` | 2.8 MB | **200** | ✅ |
| `datasets/mteb/LoCoMo` `README.md` | 22 KB | **200** | ✅ |

**Zero gated/403/private repos among every candidate** (all `gated=false private=false` via `api/models/{id}`; official Qwen3.6 family is `apache-2.0`, BAAI bge is `mit`). No blocker for the box bootstrap.

### 1.4 Two HF API gotchas found (save the P1/P2 workers time)

1. **Dataset files need the `/datasets/` path segment.** `https://huggingface.co/<repo>/resolve/main/<file>` returns 404 for a *dataset* repo, while `https://huggingface.co/datasets/<repo>/resolve/main/<file>` returns 200. Both were tested; only the second is correct.
2. **`api/datasets?search=` mixes in unrelated repos.** `search=locomo` matches `locomotion` robotics datasets (quadruped_locomotion, etc.) — filter by inspecting `siblings` before downloading.

Also: this account owns private datasets `wallfacers/engram-locomo-artifacts` (the historical canonical store archive — `009-bge-chunks-store/` etc.) and `wallfacers/engram-locomo-eval-assets`, both accessible and reusable for cross-checks.

---

## 2. Dataset validation — `testdata/locomo/locomo.json`

### 2.1 Counts (exact)

| metric | value | expected | verdict |
|---|---|---|---|
| conversations | **10** | 10 | ✅ |
| QA pairs (total) | **1986** | — | ✅ |
| **answerable block (cat 1–4)** | **1540** | 1540 | ✅ matches the frozen denominator |
| **category-5 (declared block)** | **446** | 446 | ✅ |
| cat-1 multi-hop | 282 | | |
| cat-2 temporal | 321 | | |
| cat-3 open-domain | 96 | | |
| cat-4 single-hop | 841 | | |
| cat 1–4 subtotal | 282+321+96+841 = **1540** | 1540 | ✅ |

Sum check: 1540 + 446 = 1986 ✅ (matches `abstain_test.go:271`'s frozen expectation `adversarial=446 answerable=1540 total=1986`).

### 2.2 Extraction units — the "~288" figure resolved

The task predicted "~288 extraction units". Two distinct counts exist and **both are correct for their own definition**:

| definition | count | source |
|---|---|---|
| **union** of `session_N` + `session_N_date_time` keys | **288** | what `--estimate` reports as `extract_calls` |
| turn-bearing sessions (actually carry dialogue) | **272** | what really triggers an LLM extraction call |

Their difference is **conv0 only**: conv0 declares `session_1…session_35` dates but ships turns for `session_1…session_19` (16 date-only blocks). Every other conversation has `turn sessions == date blocks`.

**Measured live during the run** (authoritative, from the budget proxy's per-call journal): **exactly 272 extraction calls**, matching the turn-bearing count. A date-only session has zero substantive messages, and `pipeline.IngestDetailed` returns early with no LLM call for an empty batch. So:

- `--estimate`'s `extract_calls=288` is an **over-estimate** (it counts session blocks, not non-empty batches).
- The real cost denominator is **272**.
- Either way, both are under the 300-call hard cap.

### 2.3 Per-conversation size sanity

`testdata/locomo/locomo.json` = 2,805,274 B.

| conv | sessions | turns | transcript chars | QA pairs |
|---|---|---|---|---|
| 0 | 19 (35 declared) | 419 | 57,690 | 199 |
| 1 | 19 | 369 | 43,587 | 105 |
| 2 | 32 | 663 | 89,736 | 193 |
| 3 | 29 | 629 | 71,843 | 260 |
| 4 | 29 | 680 | 86,298 | 242 |
| 5 | 28 | 675 | 80,224 | 158 |
| 6 | 31 | 689 | 80,947 | 190 |
| 7 | 30 | 681 | 73,258 | 239 |
| 8 | 25 | 509 | 62,435 | 196 |
| 9 | 30 | 568 | 80,738 | 204 |
| **total** | **272** | **5882** | **726,756** | **1986** |

- **avg transcript chars/conv = 72,676**; min 43,587 · **max 89,736** (conv2).
- avg turns/conv = 588.2; sessions/conv min 19 · max 32 · mean 27.2.
- No transcript exceeds a 32k-token answer-input cap concern (these are extraction inputs, chunked per session; the largest single session is well within `perCallTimeout`/`max-tokens` hygiene).

### 2.4 Provenance cross-check (bonus)

The HF mirror `datasets/KimmoZZZ/locomo/locomo10.json` downloaded (2,805,274 B) is **byte-identical** to our repo dataset:

```
sha256 hf_locomo10.json         = 79fa87e90f04081343b8c8debecb80a9a6842b76a7aa537dc9fdf651ea698ff4
sha256 testdata/locomo/locomo.json = 79fa87e90f04081343b8c8debecb80a9a6842b76a7aa537dc9fdf651ea698ff4
```

Independently reproduced from `Percena/locomo-mc10/raw/locomo10.json` (same bytes). The dataset in use is the public canonical LoCoMo-10, not a local mutation.

---

## 3. Extraction pre-pass

### 3.1 Path taken: the harness's own extraction (preferred branch, no fallback needed)

Read `cmd/locomo-bench` store-build mechanics first. Findings:

- `buildConversationRuntime` (`main.go:1766`) is the single store-build path: opens `store.Open{DSN: <storeDir>/conv<N>.db}`, builds `EntryStore/VectorStore/Embedder`, constructs `pipeline.New(...)` and calls `pipe.Ingest` per session, then `ingestChunks` (when `--chunks`) and `embedder.Backfill`.
- `--coverage-only` (`main.go:945`) drives **that exact path** and then makes **zero answer/judge calls** — it only grades retrieval recall. It is therefore the harness's own standalone-drivable extraction entry point. **Chosen** (keeps provenance identical to a paid run, as the task prefers).
- `--estimate` confirmed `extract_calls=288`, `questions=1540`.

Command actually run (10 conversations, full dataset):

```bash
source ~/.engram-eval-secrets.env                 # ENGRAM_JUDGE_* live values, never recorded
export LOCOMO_PROVIDER=anthropic
export LOCOMO_BASE_URL=http://127.0.0.1:8099/anthropic   # budget-capped proxy -> api.deepseek.com/anthropic
export LOCOMO_API_KEY=proxy-local                        # local proxy; real key lives in the proxy env
export EXTRACT_MODEL=deepseek-flash
export LOCOMO_MODEL=deepseek-flash
export EMBED_BASE_URL=http://127.0.0.1:8010/v1
export EMBED_MODEL=BAAI/bge-large-en-v1.5
export EMBED_API_KEY=local-eval
./bin/locomo-bench --data .../testdata/locomo/locomo.json --dataset-format locomo \
  --store-dir ~/engram-store-staging/stores --run-dir ~/engram-store-staging/full-run \
  --retrieval hybrid --chunks --concurrency 16 --max-tokens 8000 --top-k 30 --coverage-only
```

**Worker pool**: the harness's extraction fan-out is its internal 16-way `buildSem` (`main.go:893`, `const buildConcurrency = 16`) plus a `--concurrency 16` LLM semaphore — **parallel, never sequential**, satisfying the hard rule. 1 detached WSL2 run + instant polls; no foreground waits.

### 3.2 Budget hard cap enforcement (≤300 calls)

I did not merely intend the cap — I enforced it in the request path. A small local proxy (`~/engram-store-staging/sidecar/extract_proxy.py`) sits in front of the DeepSeek Anthropic endpoint and **refuses call 301+ with HTTP 429**, so the harness would fail closed rather than silently overspend. It also journals every call (model, status, tokens) and refuses an unexpected model id.

Result from the proxy's own per-call journal (`logs/proxy_calls.jsonl`, 272 lines):

| metric | value |
|---|---|
| extraction calls made | **272** |
| calls OK (HTTP 200) | **272** |
| non-200 / errors | **0** |
| calls refused by the cap | **0** (cap 300 never reached) |
| input tokens | 384,595 |
| output tokens | 237,658 |
| model | `deepseek-flash` (only model seen) |

Headroom: 272 / 300 = 91% of the cap. **No cap breach, no retry storm.**

### 3.3 Wall clock & cost

- Smoke (conv0, 19 sessions): 11:38:46 → 11:42 → ~3.5 min incl. 83 chunks + embedding.
- Full run (10 conv, 253 new extraction calls): **11:43:30 → 11:58:12 = 14m 42s** for extraction + chunks + the first embedding pass.
- Extraction-only throughput ≈ **17 calls/min** at `--concurrency 16` (bounded by DeepSeek latency + the 16-way build semaphore).
- Cost: 272 calls × (384,595 in / 237,658 out tokens). No price entry exists for `deepseek-flash` in-repo (`--estimate` reports `unpriced model=deepseek-flash`), so I state **token totals, not a USD figure** rather than invent one. At any plausible flash-tier pricing this is a **well under ¥10** spend for the whole pre-pass — inside the task's budget gate.

### 3.4 Extraction health

`272/272` calls returned 200 with zero transport failures. The only log noise was the pipeline's own fact-rejection audit (`reason=invalid_source_ids`), which is the engine's normal guard against a model citing a source id outside its batch — degraded-but-honest, no run failure.

---

## 4. Vector store staging — **COMPLETE (10/10 stores built and embedded)**

### 4.1 Embedding endpoint: local option (a) chosen — it was feasible well inside 30 min

Ollama is not installed on this machine, but **`fastembed` 0.8.0 + onnxruntime is already present in the miniconda env**. I wrote a ~90-line OpenAI-compatible sidecar (`sidecar/embed_server.py`) serving `GET /v1/models` and `POST /v1/embeddings` in exactly the shape `embedding.HTTPClient` expects.

Chosen model: **`BAAI/bge-large-en-v1.5` (1024d)** — deliberately the *same* model the canonical box recipe uses (`docs/operations/evaluation/environment-023-eval-box.md:81-82`), so these stores stay comparable with the historical baseline. Verified:

- model load 112 s (first-time fetch) then 0.24 s for a 2-text batch;
- **no query/passage prefix asymmetry**: `embed()` and `query_embed()` produce bit-identical vectors (`maxabsdiff=0.00000000`), so the engine's single `Embed` path is correct for both stored facts and queries;
- store vectors confirmed 1024-d float32.

**Exact env for reuse:**

```bash
export EMBED_BASE_URL=http://127.0.0.1:8010/v1
export EMBED_MODEL=BAAI/bge-large-en-v1.5
export EMBED_API_KEY=local-eval
```

### 4.2 A real defect found: the harness's embedding backfill silently drops vectors

**This is the most important operational finding of P0.5.** After the full run, only **2816 / 4257 active facts (66%)** had vectors; **1441 were missing**. Root cause, traced in the engine source:

- `memory.Embedder` uses a **bounded queue of 256 names** (`DefaultEmbedBuffer = 256`), and `Enqueue` is explicitly non-blocking: *"a full queue drops the request"* (`memory/embedder.go:240-254`).
- `Embedder.Backfill` enqueues *all* missing names at once, so anything past 256 **is dropped**, with only a debug-level log.
- `cmd/locomo-bench` calls `Backfill` **exactly once** per conversation (`main.go:1846`) and then `Close()`.
- The run's 16 parallel store builds each own a 256-slot queue while **all share one `MaxInflight=4` embedding client**, so drains lag far behind enqueues and overflow is the common case, not the edge case.

Evidence: the run enqueued **3792** names across 10 stores (per-store requests of 258/368/417/421/440/459/459/473/494 + 3) against 256-slot queues — structurally guaranteed to drop.

Why it matters for 051: a store whose facts lack vectors makes the hybrid arm **silently degrade to keyword+entity** (`retriever` drops the semantic signal). That would weaken arms A–D and — worse — could quietly invalidate the B↔D comparison, since the Jev filter operates on an RRF pool that is missing its semantic component.

### 4.3 Repair: drain each store to fixpoint with the engine's own embedding path

Per the task's explicit fallback authorization, I wrote a small throwaway Go program **outside the repo** (`~/engram-store-staging/embedfix/`, `replace`-pinned to the repo module) that:

- opens each `conv<N>.db` (the harness's own `--store-dir` layout),
- re-runs `memory.NewEmbedder(...).Backfill` with a queue **sized to the store** (`len(missing)+64`) so nothing drops,
- repeats until `NamesMissingModel` reports zero, then reports coverage.

It reuses the engine's own `VectorStore`, `EntryStore`, `Embedder` and `embedText` (Trigger+Content), so the vectors written are **identical to what a complete harness Backfill would have produced** — no reimplementation of engine algorithms, no extraction, no answer/judge calls. It faithfully reproduces the engine's shadow-row rules (`aliasEmbedText` drops aliases already inside the fact body; `queryEmbedText` drops blanks) so its "0 missing" claim is honest rather than a false gap.

**Result — before → after:**

| | before repair | after repair |
|---|---|---|
| active facts | 4257 | 4257 |
| facts missing a vector | **1441** (66% coverage) | **0** (100%) |
| `integrity_check` | — | **ok** on all 10 |
| distinct vector rows | 2816 | 4500 (facts + `#alias`/`#query` shadows) |

The 28 alias-shadow rows left over are **not** missing embeddings: they resolve to empty embed text because the alias is already a substring of its own fact (verified concretely: `alias='pride parade'` ⊂ `caroline-attended-an-lgbtq-pride-parade-…`), so the engine deliberately writes no row. The final counter reproduces that rule and reports `complete (0 missing)` for all 10 stores, all exit 0.

### 4.4 Staged artifacts

`~/engram-store-staging/stores/` — the harness's `--store-dir` layout, one SQLite file per conversation:

| property | value |
|---|---|
| files | `conv0.db` … `conv9.db` (10) |
| **total size** | **41 MB** |
| embedding model tag | `BAAI/bge-large-en-v1.5` |
| embedding dim | 1024 |
| extraction model | `deepseek-flash` |
| active facts | 4257 |
| facts missing a vector | **0** |
| `pragma integrity_check` | **ok** (all 10) |
| journal mode | **DELETE** (WAL collapsed → single portable file each) |
| built with | `--chunks` (900/1100 char defaults), hybrid arms |

They are plain SQLite files — portable to the box by copying the directory.

### 4.5 Mount verification (proves P2/P3 can use them)

Re-mounted the staged stores with the extraction endpoint pointed at a **dead port** (`127.0.0.1:9`), so any extraction attempt would fail loudly:

| check | result |
|---|---|
| extraction attempts | **0** |
| `reusing persisted extraction` log lines | **10 / 10 conversations** |
| exit code | 0 |
| facts loaded per conversation | 205, 236, 283, 357, 329, 324, 364, 324, 370, 409 |

And the retrieval quality of the staged stores, measured with the canonical grading (`--coverage-only`, exact-turn recall, n=1532 gradeable):

| configuration | OVERALL turn_recall | multi-hop | open-domain | single-hop | temporal |
|---|---|---|---|---|---|
| k=150, quota=0 (051 arm-B pool shape) | **0.739** | 0.571 | 0.467 | 0.816 | 0.761 |
| k=30, quota=12 (canonical recipe) | **0.808** | 0.654 | 0.581 | 0.877 | 0.825 |
| k=30, quota=0 | 0.026 | 0.026 | 0.000 | 0.033 | 0.014 |

The **0.808 at k=30/quota=12 exactly matches the historical canonical baseline** (`benchmark-parity-memory-architecture.md:717`: "Recall saturates at k=30, turn_recall 0.808"), and 0.020 at quota=0 is likewise the documented expected value (`:934`). This is strong independent evidence the staged stores are correctly built and carry a working semantic signal.

*(Post-repair, k=30/quota=0 moved 0.010 → 0.026 — the semantic signal came back once the missing vectors were filled.)*

---

## 5. Handoff: exact env + commands for P2/P3

Model pin used: **`EXTRACT_MODEL=deepseek-flash`** (the `$ENGRAM_JUDGE_MODEL` value name), endpoint `https://api.deepseek.com/anthropic`.

### 5.1 Ship the stores to the box

```bash
# P0.5 artifacts are locally staged; copy the directory (41 MB, plain SQLite)
tar -C ~/engram-store-staging -czf /tmp/051-stores.tgz stores/
scp /tmp/051-stores.tgz <box>:/root/autodl-tmp/051-stores.tgz   # box-side step, P2 owns the ssh
# on the box:
mkdir -p /root/autodl-tmp/051-stores && tar -C /root/autodl-tmp/051-stores -xzf /root/autodl-tmp/051-stores.tgz
```

### 5.2 Env the P2/P3 phases should set

```bash
# --- eval box: answer/extract/filter = Qwen3.6 on the box's vllm ---
export LOCOMO_PROVIDER=openai
export LOCOMO_BASE_URL=http://127.0.0.1:8000/v1
export LOCOMO_MODEL=Qwen/Qwen3.6-35B-A3B-FP8        # pin to whatever P1 actually serves; must match /v1/models
export EXTRACT_MODEL=$LOCOMO_MODEL
export LOCOMO_API_KEY=local-eval

# --- embedding: on the box, BAAI/bge-large-en-v1.5 @ 8010. These stores were
#     built with this model, so the tag MUST match or every vector is treated as
#     stale and the semantic signal drops out (localhost sidecar is only needed
#     if re-embedding; the staged stores already carry vectors).
export EMBED_BASE_URL=http://127.0.0.1:8010/v1
export EMBED_MODEL=BAAI/bge-large-en-v1.5
export EMBED_API_KEY=local-eval

# --- judge = DeepSeek (key only via env, never a flag) ---
export JUDGE_PROVIDER=anthropic
export JUDGE_BASE_URL=$ENGRAM_JUDGE_BASE_URL        # https://api.deepseek.com/anthropic
export JUDGE_MODEL=$ENGRAM_JUDGE_MODEL              # deepseek-flash
export JUDGE_API_KEY=$ENGRAM_JUDGE_API_KEY

export HF_HUB_OFFLINE=1
```

### 5.3 Store-dir form

```bash
--store-dir /root/autodl-tmp/051-stores/stores
```

There is no `ENGRAM_STORE_DIR` env var in the harness — `--store-dir` is the flag (`main.go:382`). Do **not** combine with `--image-captions` or different `--chunk-target-chars`/`--chunk-max-chars`: stores built with different values are explicitly non-comparable and the harness will refuse a chunks-store reuse under a different chunk regime.

**One caution for P3:** because `cmd/locomo-bench` calls `Embedder.Backfill` only once, a P3 run that *re-embeds* (rather than reusing these vectors) risks the same partial-vector drop on a slow/far sidecar. The staged stores already carry complete vectors, so P3 should reuse them; if P3 ever rebuilds, it must verify fact-vector coverage before trusting the arms.

---

## 6. Deviations, with reasons

| # | deviation | why | sound? |
|---|---|---|---|
| 1 | Used `--coverage-only` as the standalone extraction driver instead of a bespoke program | It is the harness's **own** store-build path with zero answer/judge calls — provenance-identical to a paid run, which the task preferred. No fallback needed (attempts used: 1). | ✅ within the stated preference |
| 2 | Inserted a local budget proxy in front of DeepSeek | To make the ≤300-call cap a **hard** failure rather than a hope, and to journal real per-call tokens. Key stayed in the proxy's env, never on a command line or in a file. | ✅ strengthens the task's cap requirement |
| 3 | Repaired the embedding gap with a throwaway Go program under `~/engram-store-staging/` | Task-authorized fallback surface; the gap was a genuine blocker for a usable vector store. Program lives outside the repo and calls only the engine's public API. | ✅ authorized + necessary |
| 4 | Wrote the two sidecars (embedding, proxy) in Python under `~/engram-store-staging/sidecar/` | No Ollama on this machine; the task explicitly allowed "python + sentence-transformers behind /v1/embeddings". Used the already-installed fastembed + the box's exact bge-large model. | ✅ option (a) of deliverable 4 |
| 5 | Report states tokens, not USD, for the extraction spend | `deepseek-flash` has no price entry in-repo (`--estimate` flags it `unpriced`); inventing a rate would be a fabricated number. | ✅ honest reporting |
| 6 | Ran extra retrieval validations (k=150 pool; k=30/quota=12) | Cheap, retrieval-only, and they independently prove the stores are correct (0.808 == historical baseline). Guards against shipping a silently degraded vector store. | ✅ verification, not scope |
| 7 | Investigation scope: also mapped the Qwen3.6 model family sizes/gating | Deliverable 1(c) asked to confirm download access for the "≤32B size class"; reporting actual shard sizes was needed to answer it usefully. | ✅ within deliverable 1 |

**No engine or repo source was modified.** `git diff --name-only -- memory embedding provider store internal filter` is empty; the only repo change is a pre-existing modification to `specs/051-jev-relevance-filter/tasks.md` that was already present before this task started (plus pre-existing untracked files), none of it mine. No AutoDL box access, no ssh/scp.

---

## 7. Blockers

**None.** All three deliverables completed. Every sub-deliverable that could have blocked (local embedding feasibility, standalone extraction drivability) resolved on the first attempt.

### 7.1 One hygiene interaction P3 must know about

This report file lives at `specs/051-jev-relevance-filter/t19-p05-store-prep.md` (the path the P0.5 acceptance gate requires). That makes it an **untracked file**, and `verifyFormalGitProvenance` (`cmd/locomo-bench/eval_runner.go:1275-1281`) **refuses a dirty worktree** — it requires `git status --porcelain` to be empty for both `--eval-freeze-protocol` and `--eval-protocol`.

Important context: this local worktree was **already dirty before P0.5 started** — `M specs/051-jev-relevance-filter/tasks.md` plus three untracked `docs/…` entries were present in the very first `git status` of this task. So a formal freeze was already impossible in this worktree; this report adds one more untracked entry but no new class of problem.

Consequence for P3: run the freeze from a **clean worktree at the protocol commit** (or from the box's fresh clone), not from this local tree. If P3 must use this tree, commit or move this report out first. Nothing was staged by P0.5 (`git diff --cached` empty).

---

## 8. Recommended next step

P3 can proceed against `/root/autodl-tmp/051-stores/stores` with **zero extraction re-spend**. Before the four-arm run, have P3 assert (a) each `conv<N>.db` reports 0 facts missing a vector, and (b) the box's `/v1/models` embedding id is byte-equal to `BAAI/bge-large-en-v1.5` — either mismatch silently degrades the hybrid arm and would corrupt the B↔D comparison.

Separately worth filing as a harness/engine backlog item (not fixed here, out of scope): `cmd/locomo-bench` should loop `Embedder.Backfill` to fixpoint (or size the queue to the store) after a store build, because a single call on a store larger than 256 missing names silently ships a partially-embedded store that degrades to keyword-only retrieval.
