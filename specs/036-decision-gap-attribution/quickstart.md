# Quickstart: Decision-Gap Attribution

This guide proves the offline contracts before any real 034/035 data. It contains no credential.

## 1. Build and test offline

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench
CGO_ENABLED=0 go build ./...
```

Expected: both exit 0 without network; ordinary CLI and frozen 034/035 modes retain parity; `git diff --name-only --
memory embedding provider store internal` is empty.

## 2. Run attribution on the real 034 data (maintainer's other machine)

The 034 stage dir must contain the validated public artifacts plus the custody-validated hidden slot map, and the three
candidate results files must be supplied exactly once each.

```bash
go run ./cmd/locomo-bench \
  --adjudication-attribution /path/to/034-stage0 \
  --adjudication-candidate /path/to/r1/results-hybrid.jsonl \
  --adjudication-candidate /path/to/r2/results-hybrid.jsonl \
  --adjudication-candidate /path/to/r3/results-hybrid.jsonl \
  [--adjudication-audit-source /path/to/035-stage0]
```

Expected (real frozen run): report written to `/path/to/034-stage0/decision-gap-attribution.json`; summary prints
`gaps=33 control_only_loss=13 both_wrong=9 categories=4 audit=035-audit dominant=...`. The four category rows sum to
33; `gap_count == oracle_correct - selected_correct` (1411 − 1378). `fallback_gaps` is 11 (6 not-triggered + 5
triggered); fallback gaps are counted separately from the 13/9 accepted-override split (never mixed).

Verified on the real 034/035 artifacts on 2026-08-11 (session-scratch, see tasks.md §Real-Data Verification and
`docs/evaluation/reports/036-decision-gap-attribution-verdict.md`).

The `--adjudication-audit-source` flag is optional. When omitted or when the 035 seal is missing/invalid, every gap
row is marked `audit_unavailable` and the report still succeeds.

## 3. Run attribution on the offline fixture (no real data needed)

The Go test suite exercises `buildAttributionRows` directly with `frozenAdjudicationScoringFixture` (1540 packets,
oracle 1411, control 1368) plus small hand-built fixtures for gap classification, both-wrong, semantic equivalence,
035 cross-audit, and overwrite refusal. No provider, no network, no real 034/035 artifacts.

## 4. Reading the report

- `rows.rows[]` — all 1540 questions with majority/oracle/control/selected state.
- `rows.gaps[]` — the 33 (real) or 43 (fixture) gap questions with per-gap failure-mode evidence.
- `categories[]` — category × failure-mode table (four categories sum to gap count).
- `summary` — frozen denominators, gap split (control-only loss vs both-wrong), dominant failure mode.

## 5. What this report is NOT

`decision_gap_attribution` is a diagnostic artifact. It does not change any decision, does not modify any 034/035
artifact, does not create a formal LoCoMo score, and does not authorize a rejudge. It only informs a future mechanism
spec (e.g. feeding lineage original wording, or improving answer generation).
