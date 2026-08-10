# 033 Paid Probe Preflight Receipt

**Frozen**: 2026-08-10; superseded after a failed environment attempt and re-frozen below. This receipt contains
no credential value.

## Binary and execution

- Source HEAD: `03281ceaed6025c97fc1679fd1952f39ea9e7066` in the isolated 033 worktree.
- Frozen scratch binary:
  `/home/wushengzhou/.claude/session-scratch/033-probe-run.ew8rCD/locomo-bench-033`.
- Binary SHA-256: `299a98837cc3c18f52d30aef4a71b26dd86a4b7ea5db3cc7ab071402f6795998`.
- Driver: `run033.sh`; `bash -n` passed. `--estimate` built the binary once; `--start` is required to reuse
  that exact binary and fresh `arm-a`, `arm-b`, `arm-c` directories.
- WSL launch: one detached auth smoke must pass first. The paid probe then uses three independent `setsid -f`
  processes, launched A/C/B in one time window. Each writes a log and terminal exit file under the same session
  scratch directory; status polling never sleeps in foreground.
- Each paid arm receives its own `cp -a` snapshot of the source store. The driver compares relative per-DB SHA-256
  manifests before launch, avoiding concurrent SQLite WAL initialization on the same files.
- `RERANK_MODEL` is cleared in every child. No reranker flag is passed. Paid cloud rerank/recall is impossible in
  this driver.

## Frozen protocol

- Answerer: OpenAI-compatible official DeepSeek endpoint, model `deepseek-v4-pro`; key was injected by no-echo
  stdin into a temporary shell and was never serialized.
- Judge: `anthropic`, `deepseek-v4-flash`, mem0-aligned; judge endpoint/key are inherited, never serialized.
- Embedding: local `http://127.0.0.1:8010/v1`, `BAAI/bge-large-en-v1.5`; `/models` returned HTTP 200.
- Retrieval: hybrid, top-k 30, chunk quota 12, persisted store, chunks enabled.
- Aggregate LLM-call concurrency: 32, explicitly authorized by the maintainer for this endpoint; simultaneous
  A/C/B processes are capped at 11/11/10 respectively (not 32 each).
- Answer regime: force-answer, `LOCOMO_NO_THINKING=0`, legacy IDK retry enabled, repeats 3,
  `trace_mediation=false`.
- A: assembly off, 64 questions. B: legacy grouped assembly + runtime audit, 18 multi-hop questions.
  C: kind-layered assembly + runtime audit, 64 questions.
- Assembly cap is the harness constant 3600. No `--token-counter-base-url` is supplied because the hosted
  v4-pro answer endpoint is not treated as a local vLLM `/tokenize` sidecar. Therefore assembly audit is expected
  to be `tokens_estimated=true`; the analyzer separately requires the provider-reported
  `answer_context_tokens > 0` for every actual answer. The verdict must not claim token-exact assembly.

`--estimate` reported 64/18/64 questions over 3 repeats. This freezes **438 scored repetitions**, consisting of
438 primary answer calls plus 438 primary judge calls. Adaptive legacy IDK answer/rewrite provider calls are
additional and must be taken from runtime cost artifacts. The generic estimator printed 288 extraction calls per
arm because it does not model persisted-store reuse; that is not a planned paid count. Runtime must show zero
extraction calls or the probe is invalid.

## Frozen data/store

- Dataset SHA-256: `79fa87e90f04081343b8c8debecb80a9a6842b76a7aa537dc9fdf651ea698ff4`
  (`2,805,274` bytes).
- Store: 10 SQLite files, `71,536,640` bytes. The original pre-change path-bearing manifest was
  `d3b8bd4ebc18090f112a78b85d141fe511fb05aef109e3b37386dee20879d772`. The failed concurrent attempt opened
  the source databases and changed SQLite journaling bytes, so its new relative-name manifest is
  `06d2c484582cca071c7edf21dc33edf943b721bbce7fd351d917af183dfc0089`; this is the snapshot now frozen for
  all arms. A fresh retrieval-only treatment run on a copy produced 64/64 records that were byte-equal to the
  pre-attempt `treatment4` records after sorting, with input closures equal 64/64; `PRAGMA integrity_check` was
  `ok`. Thus retrieval-visible logical content/order did not drift, while the byte-manifest change is disclosed.

## Frozen execution and analysis cohorts

| Artifact | Count | SHA-256 | Runtime input? |
|---|---:|---|---|
| `target-32.txt` | 32 | `2f0ed8586c8648b1fcfecc95db512fdfcd0e1e77813bc2d83ed599ace7531f4b` | via probe union |
| `guard-32.txt` | 32 | `864bdff5115c0bd93a135cf8ae0d8e7490ac42776abf3e8ba500d0197119b581` | via probe union |
| `probe-64.txt` | 64 | `3ac0efc5ccbaa2e677eee3b97c1f0cc5bb11f59f8af30d82c297c0fc36237eba` | A/C only |
| `multi-hop-18.txt` | 18 | `48e8298a75178b17e2da4e48d663bc6d6e8a5a08db15703584b7324a00f05c32` | B only |
| `chunk-gold-19.txt` | 19 | `39be45c3a4411a294e92240123697db3c16646788419b574ebe898ac08eb11ea` | no; post-result only |
| `chunk-gold-rank19.txt` | 16 | `543c9cda0fa5053d715ef813d65007d79af82609ef6720b9f88a1c7711fe544a` | no; post-result only |
| `chunk-gold-map.json` | 19 | `da200d354c296e3241dbfed405db91ebb31d89aeb0244d130770c0c6f32b62ff` | no; post-result only |

The source attribution trace SHA-256 is
`fa65bdd327ba513f960370b7f8ece0339f536eb86898e3fd9babc87d8e4bf9b6`. The diagnostic prose says 14
questions at rank ≥19, but its explicit rank list and trace produce 16. The analyzer derives the literal predicate
from `chunk-gold-map.json` and rejects any mismatch with the 16-ID file. Gold-derived files are not referenced by
the benchmark execution arguments.

## Tooling gates

- Runtime audit tests passed:
  `TestAssemblyAuditValidationAndFingerprintNeutrality` and `TestAnswerPathWritesRuntimeAssemblyAudit`.
- Probe analyzer fixtures: 5/5 passed, including missing/misaligned audit rejection, literal high-rank cohort
  validation, exact McNemar and Holm correction.
- Invalid auth and aggregate-96-concurrency attempts are documented in `failed-probe-attempt.md`. The driver now requires
  `smoke-1.txt` (SHA-256 `b79a9e430d3467e8ef45de4e74f2cfdb13d65c89b448aa00c7be4436f60da67b`) to pass before full launch. The smoke
  requires the OpenAI-compatible provider and at least 1000 reported input tokens, preventing cache-miss-only
  Anthropic usage from masquerading as complete context accounting.
- The final OpenAI-compatible smoke recorded 3230 provider input tokens, a non-empty prediction and one judge call;
  the valid A/B/C run then completed with exit 0 and exact 192/54/192 coverage.
