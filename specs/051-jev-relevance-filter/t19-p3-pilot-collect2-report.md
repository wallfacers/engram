Implemented T19 P3 pilot verdict collector (ops-only; box kept UP per the superseding maintainer order).

**Branch taken:** chain landed during my first box-side wait window (14:32:37), so steps 1–3 ran fully; step 4's shutdown logic was replaced by the override → verified-up, no shutdown issued or armed.

**Changed files**
- `specs/051-jev-relevance-filter/t19-p3-pilot-collect2.md` (new report)
- `specs/051-jev-relevance-filter/pilot-artifacts/p3pilot2-*` (11 small artifacts: verdict JSON, arms report JSON, gated.log, markers, regime/freeze/protocol, watcher log, journal digest)
- `.scratch/t19_p3c2_ssh.py`, `.scratch/t19_p3c2_fetch.py`, `.scratch/t19-p3c2/` (gitignored helpers + raw journals)

**Headline result**
- `gated.exit=1 / chain.done=1`, but the **measurement completed**: 7,700/7,700 answers, 3,080 filter rows, `invalid=false`, 5 h 07 m (25.05 answers/min — the launch ETA held).
- rc=1 is a **structural harness seam**, not a model failure: `--jev-arms` returns at `main.go:959-962` before the only writers of the receipts its own gate demands (`main.go:1181-1201` + `formal_calls.jsonl` at `:973`), so `jevArtifactValidityFromDir` reads a missing `summary.json` → `isComplete()=false` → `promotion_verdict=INVALID`. Every prior 051 run dir on the box lacks those receipts too. **The full 3-rep formal chain would exit 1 for the same reason** — fix the seam before spending ~15 h of GPU.
- Four-arm (denominator 1,540): A 25.84 / B 60.65 / C 18.38 / D 56.88 / D-noRelax 56.43 %. **B→D = −3.77 pp, exact McNemar p=3.43e-05, CI [−5.52, −2.01] → SC-001 HOLD.** SC-002 pass (D = 7.2 % of B tokens, 3.81 shown). SC-007 passes only via the 10×p95 anomaly rule (p50 1,169 ms = 3.9× the 300 ms expectation). SC-003 HOLD and effectively uninformative (mean pool recall@150 = 0.918 %, non-zero on 1.3 % of questions — inconsistent with B's 60.6 %, so the gold↔memory linkage, not retrieval, is suspect).
- Tail: degraded 25/3,080 = **0.81 %** (20 negative-cache + 5 shard-failure, 13 distinct questions, all enumerated for 补齐) vs ~96 % in the killed Sep-24 pilot — the 503-storm fix held. INVALID/gradeable-only gap: 8 questions (0.52 %), identical set across arms.

**Risk I want flagged:** I overwrote the tracked Sep-24 `pilot-artifacts/gated.log` during fetch; restored byte-identical from HEAD (md5 verified), and namespaced all new files `p3pilot2-*`. No other agent's artifact was touched.