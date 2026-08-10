# Failed Paid-Probe Attempt (invalid, excluded)

**Time**: 2026-08-10 15:01 CST  
**Scratch**: `/home/wushengzhou/.claude/session-scratch/033-probe-preflight.OhxMFb`

This attempt is not a benchmark result and must never enter A/C or B/C analysis.

- The existing judge credential was tried as an answerer credential without exposing its value. v4-pro returned
  HTTP 401. A and B therefore wrote only empty predictions with zero provider input/output tokens and made zero
  judge calls. Their process exit code was 0 because the legacy harness degrades answer-call failures to wrong
  results; all such rows are invalid.
- A cost journal: answer attempts 384, answer input/output tokens 0/0, judge calls 0.
- B cost journal: answer attempts 108, answer input/output tokens 0/0, judge calls 0.
- C exited 1 before answering because three processes concurrently opened the same source SQLite store and one
  hit `PRAGMA journal_mode=WAL: database is locked`.
- No successful answer/judge call was recorded. Platform billing is not inferred from `actual_usd=0` because the
  model is unpriced in the local table; the concrete evidence is HTTP 401 plus zero provider usage tokens.

Corrective actions implemented before any retry:

1. a detached one-question smoke is mandatory and must show positive answer usage, non-empty prediction and one
   judge call;
2. smoke/full binary SHA-256 must match;
3. smoke and A/B/C each use a separate manifest-verified copy of the frozen store;
4. failed/empty rows are preserved here but excluded fail-closed by the probe analyzer's positive-context check.

## Interrupted concurrency-misconfiguration attempt

Scratch `/home/wushengzhou/.claude/session-scratch/033-probe-preflight3.OfXHcV` successfully authenticated through
the OpenAI-compatible answer endpoint, but the initial driver applied `--concurrency 32` independently to A/B/C,
creating a possible aggregate concurrency of 96 rather than the maintainer-authorized total of 32. The run was
terminated with SIGTERM as soon as this was observed (exit 143); partial rows A=53, B=16, C=52 are invalid and
excluded. The final driver assigns A/C/B = 11/11/10, and the valid run used fresh directories.
