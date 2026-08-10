# 034 Paid Stage-0 Run Receipt

**Date**: 2026-08-10
**Status**: **COMPLETE — valid seal, 771/771 one-attempt calls, concurrency 32.**

The public artifacts were revalidated immediately before execution:

```text
protocol=sha256:9b840473b0c1fef8c5c0f97a55c5cde6fb7fa771efb8103ff74a526aa99efb19
questions=1540
triggered/planned calls=771
context parity / triggered context parity=1532 / 766
```

The operator environment supplied a credential different from the credential previously exposed in chat. The run used
the dedicated adjudicator environment, explicit `--adjudication-allow-paid`, `--concurrency 32`, and
`--adjudication-max-tokens 512`. No credential, raw endpoint, raw provider error, or provider response was persisted or
printed by the benchmark.

| Receipt | Result |
|---|---:|
| Provider API shape | `openai` |
| Model / revision receipt | `deepseek-v4-pro` / `unversioned-live-2026-08-10` |
| Endpoint fingerprint | `sha256:12b8deaccc34b32757dbb1497e029da0c2e7b26ffa86b9c926c08cb4692f4508` |
| Planned / started / attempts | 771 / 771 / 771 |
| Completed selections / terminal fallbacks | 718 / 53 |
| Retries | 0 |
| Non-trigger zero-call decisions | 769 |
| Final decisions | 1540 |
| Input / output tokens | 4,310,100 / 26,590 |
| Pricing status | `unpriced` |
| Seal validity | `true` |

Fallbacks were closed and deterministic: 49 `low_confidence`, 4 `invalid_response`, and 769 `not_triggered`. There
were no provider-transport failures and no orphan `STARTED` records. `calls.jsonl` contains exactly 1542 state records,
one STARTED and one terminal record for every triggered packet.

Artifact receipts:

```text
packet_set_digest=sha256:70d63daf01bf07e3fc2de3535d940f12c3c2f198f854b89b7b1221bc687e0a4a
decision_set_digest=sha256:7f38f710bb9e7b42446f9c32ed94f0ee893b3a39b070df19d3e9e4481c3f3694
prompt_digest=sha256:a92fed147d2cf4a5deec0469c2fbfda36d28f8de06ed34a2a411682bc185f36e
seal_file_sha256=58a43d0950cd06631aca4dedde00031655b913dd964fc1da6cd50bb5fb542c90
```

The seal is a complete execution receipt only. Its selection quality is assessed separately in `verdict.md`.
