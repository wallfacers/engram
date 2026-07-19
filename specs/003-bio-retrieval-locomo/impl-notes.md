# Feature 003 Implementation Notes

## Batch 3.5 Known Limitations

- When `EMBED_RERANK_MODEL` is enabled, the cross-encoder rerank can override
  temporal multipliers, and entity-neighbor expansion can reintroduce entries
  removed by `TemporalHardFilter`. The current Strike 2 evaluation environment
  does not enable reranking; this interaction is intentionally deferred.
- `--abstain-prompt` remains a no-op in the answer path from the pre-US5
  implementation. US5/Strike 3 owns that behavior; this batch does not change
  it.
