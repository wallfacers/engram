# 034 Label-Blind Build Receipt

**Date**: 2026-08-10
**Status**: PASS; offline only, provider calls = 0.

## Why 033 stopped

The rejected 033 chunk-first probe remains recorded in its own isolated worktree as NO-GO. Its target cohort moved
only +1/32 against a preregistered +8 gate. More importantly, every one of the 57 chunk-gold repetitions admitted all
30 candidates and the gold chunk; cap truncation and gold exclusion were both 0/57. The treatment therefore only
reordered evidence already visible to a strong answerer. It did not repair a recall or truncation failure, and the
tail-rank subgroup produced zero flips. A full retrieval/assembly run was correctly stopped.

034 tests the remaining bounded hypothesis: the three frozen answer runs already contain different candidate answers,
so an evidence-grounded selector may choose among existing answers without changing retrieval or generating a fourth
answer. This is an answer-side diagnostic, not a paid reranker/recall dependency and not an engine feature.

## Frozen public build

| Receipt | Value |
|---|---:|
| Protocol hash | `sha256:9b840473b0c1fef8c5c0f97a55c5cde6fb7fa771efb8103ff74a526aa99efb19` |
| Packet-set digest | `sha256:70d63daf01bf07e3fc2de3535d940f12c3c2f198f854b89b7b1221bc687e0a4a` |
| Questions | 1540 |
| Triggered | 771 |
| Context parity | 1532 |
| Triggered context parity | 766 |
| Resolved evidence items | 46,200 |
| Provider calls | 0 |
| Forbidden execution fields | 0 |

Sanitized candidate-source digests, sorted canonically:

- `sha256:1d19f85f147f1df754402a6be5d9d9f4ce86d2c947a189ad3d844357ae2ea262`
- `sha256:5f6a6550cc10e5b870e3b4c4b138174f884325a45bdb7dc9000b22f1f65a76f0`
- `sha256:dab5da3f3c77ad076131ff280dcab986e224773797259104a4eaa4c3912f4fdf`

Additional label-free receipts:

- sanitized trace: `sha256:6fd07de5df398ed443eca00b880c40783e2fe6ae7434d285aa1ba99b701e2875`
- store semantic snapshot: `sha256:84e854f28a6d96a5c79929dab6ff333a7ec7b674028fc79194080851c4029f20`
- question IDs: `sha256:fa8e64417a3fd0c18322cf5d074808771b28e8a728294bfab6fec92be8837032`
- verifier prompt: `sha256:a92fed147d2cf4a5deec0469c2fbfda36d28f8de06ed34a2a411682bc185f36e`
- store inventory: `sha256:347b28bb1680b0efde4ca3abfb9297b96de15a18e5c955e220fcb7c290b3ad76`
- score-only slot map: `sha256:7211708d291aae65f094874838122db99d516cf96f41829fc46372ee2b952911`

The historical candidate-model identity is deliberately marked `legacy_operator_claim`, not verified provenance.
No source path or secret is present in tracked receipts.

## Reproducibility and mutation checks

- Rebuilding with candidate CLI arguments in another order produced byte-identical `manifest.json` and
  `packets.jsonl`.
- The public validator recomputed all frozen counts and accepted the build.
- Candidate and trace parsers materialized only allowlisted fields. Hidden-label mutation changed raw custody digests
  while leaving sanitized records and execution digests unchanged in tests.
- All ten SQLite stores were opened with `mode=ro&immutable=1`; pre/post file digests matched and no WAL/SHM sidecars
  existed or were created.
- Public run/validate inputs do not contain `gold`, `correct`, `verdict`, source-run identity, or raw memory entry names.

The historical 13/5 judge-instability counts intentionally remain unjoined here. They are hidden-label facts and may
only be recomputed by the score phase after a valid decision seal exists.
