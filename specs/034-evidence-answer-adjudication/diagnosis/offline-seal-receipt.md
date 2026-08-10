# 034 Offline Seal Receipt

**Date**: 2026-08-10
**Status**: PASS; injected deterministic stub, network calls = 0, concurrency = 32.

The frozen 1540 public packets were executed twice in session scratch with an injected provider-independent stub. The
stub returned `C1` with citation `E01` and fixed usage for every triggered packet. It exists only to exercise execution,
journaling, fallback, concurrency, accounting, sorting, and sealing; it makes no accuracy claim.

| Receipt | Value |
|---|---:|
| Questions / decisions | 1540 / 1540 |
| Planned / started / provider attempts | 771 / 771 / 771 |
| Completed / failed | 771 / 0 |
| Non-trigger fallback | 769 |
| Retries | 0 |
| Output cap | 64 synthetic tokens |
| Input / output tokens (synthetic) | 13,107 / 2,313 |
| Pricing | `declared_zero` (stub) |
| Decision-set digest | `sha256:a741cd07ca4ab4aca2a618ec99cd9b796a9254bf7ff337c6f80f492a9ede6c13` |

Both concurrent executions produced byte-identical `sealed-decisions.jsonl` and `seal.json`. Their file SHA-256 values
were respectively `a741cd07ca4ab4aca2a618ec99cd9b796a9254bf7ff337c6f80f492a9ede6c13` and
`4f7bcb70c69cc1a0382e4cacf6652bb2a6d8b3f01ebbfb3fb3a5a7c69fef9d75`. Each journal contained exactly 1542 records:
one fsynced STARTED and one terminal record for each of 771 triggered packets.

Focused command result:

```text
=== RUN   TestExecuteAdjudicationFrozenOfflineStub
--- PASS: TestExecuteAdjudicationFrozenOfflineStub (21.80s)
PASS
ok   github.com/wallfacers/engram/cmd/locomo-bench 21.807s
```

The output directory remains session scratch only. No key, endpoint, raw provider response, raw provider error, or
score-only slot map is present in public run artifacts.
