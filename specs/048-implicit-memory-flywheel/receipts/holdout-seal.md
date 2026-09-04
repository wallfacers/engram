# holdout96 Dataset Seal Receipt (T036)

**Date**: 2026-09-03
**Protected root**: `~/.engram-eval/holdout96-v2` (operator-provided, mode-guarded)
**Contract**: dataset-protocol.md v4.4 · **Dataset**: `agent-memory-trigger` / `holdout-96-v1`
**Split**: `holdout` (generalization score; disjoint from core172, never combined)
**Plaintext policy**: this receipt carries digests and counts only — zero case content.

## Sealed artifacts

| Item | Value |
|---|---|
| Cases | 96 (case ids sealed in manifest `case_ids`, 96) |
| Payload digest | `000efc1653f901c1192775ebd6b639965ca71789adb47c89b8bd73febbc67d6f` |
| Manifest file | `<root>/sealed/manifest.json` (frozen, never overwritten) |
| Anchor | `immutable-object` : `sha256-000efc1653f901c1` (content-address of the payload digest) |
| Anchor content digest | `8b0f384bead5ec5f23b5bd8b2e01fa84aa778588310f7647dada3d0e67cb38fc` |
| Membership | split=`holdout`, score_membership=`holdout96` |
| Matrix | module 20/20/20/20 + TRP 8 + TWN 4 + TRN 4 · zh/en 48/48 · authors 32×3 · 8 scenario buckets ×12 — all PASS |
| Validation | `validate --split holdout`: 89 PASS / 0 FAIL / exit 0 |

## Frozen provenance digests (for later disjointness checks)

| Digest | Value |
|---|---|
| `author_review_isolation_digest` | `67125b0f2cc9d89cec335b1cb09d18655c13439db47128634a3b48644c414d03` |
| `author_review_state_roots_digest` | `2e4958a42b9c144e54ebe51d89faa19319bd49d856523c9a845a078180d03d73` |
| `author_review_attempt_event_chain_digest` | `c6c51f17c1212124faf024ec30fa21c8919422cdcc94292fe9b6952713a543f7` |
| `admission_chain_digest` | `09391285618d30d1aa8c0d79e95dc3cc62051c2b1d0e3c3dd36ad9fe49e340c8` |
| `accepted_family_state_digest` | `789f8a6dfdfd9ffebf28c6ba45f8740229d39f3529408c7f64a4b492729a4b00` |
| `case_ids_digest`, payload files digests | sealed in manifest |

## Generation provenance

- Author prompt: `holdout-authoring` v1, sha256 `f3f3670dc996b755e7ef38bf5c4506cb98264d6343452799cd372c5a15547ae4` (one digest per batch, gate-enforced)
- Review prompt: `holdout-review` v2, sha256 `8c189cecb633b08cbb0284595d4e47887d33e91c660082ffd6ce2ddcfbcffb46` (v2: fixed four-step decision order; v4.2/v4.3 gate semantics)
- Lanes: claude / codex / opencode2, all resolved to the maintainer's authorized Bailian `qwen3.8-flash` (contract v4.4: billing honestly declared, no free-model assumption)
- Ledger: 2017 started / 2473 terminal events (terminal > started because author attempts carry a second append-only final terminal: `admitted` | `rejected`)
- Admissions: 365 receipts, 100 committed (96 sealed cases + 4 superseded duplicates of repeated slot tuples; their families remain in the accepted-state novelty baseline by design)
- Isolation: every launched attempt carries the complete 8-probe receipt set; aggregate gate passed at every save and at seal
- Concurrency: 10 (WSL2 detached `setsid` pattern); 7 generation rounds across 2026-09-02/03

## Known residual (recorded, not hidden)

- 1 reviewer violated the closed `reason` code with a free-text rationale (recorded verbatim in the ledger reason counts); the verdict itself was well-formed.
- 4 accepted families correspond to first incarnations of repeated slot tuples whose cases were overwritten in the Filled map before the instance-index fix; they stay in the novelty baseline, are NOT in the sealed payload.
- Authoring-bias limits on negative modules (contract v4.3): the negative behavioral contract is author-owned; reviewer unanimity + novelty are the anti-collusion core. Declared for T037's dataset card.
