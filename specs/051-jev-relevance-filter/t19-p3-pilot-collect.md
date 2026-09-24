Implemented T19 pilot collector execution and report.

**Branch taken: task item (5) — box unreachable from the start (already shut down).**

Changed files:
- `specs/051-jev-relevance-filter/t19-p3-pilot-collect.md` (new report; nothing else touched, nothing staged)

Validation / evidence:
- 4 connection attempts over ~6 min (01:05→01:11 CST): SSH reaches the AutoDL proxy (TCP port open) but the server closes **before the banner** (`kex_exchange_identification: Connection closed by remote host`) — verified with both candidate keys, in BatchMode. This is the definitive "instance powered off" signature, not auth failure (`Permission denied` would appear post-banner) and not a network blip (identical failure across all attempts).
- Consequence: chain.done polling, artifact scp, quick stats (answers vs 7700 / degraded share / INVALID / four-arm A-B-C-D verdicts) are all **impossible remotely** — data persists on `/root/autodl-tmp` (data disk survives shutdown). Shutdown/billing-stop is satisfied a-contrario (instance is down).
- Report includes a ready next-boot collection checklist + stats contract, and an honesty ledger noting that **down ≠ completed-clean** (rc unknown; watcher fires on failed chains too; a stale rotated endpoint after a restart would show the same signature).
- Secret hygiene: report scanned — 0 occurrences of the port, endpoint FQDN, or any IP; credentials were handled via env only.

Open risks/questions:
- Whether the pilot **completed** (chain.done rc=0 + verdicts) or died early cannot be determined until the box is up again; if it completed, the artifacts have been sitting uncollected since shutdown.
- If the instance was restarted, `~/.engram-eval-secrets.env` `ENGRAM_EVAL_SSH_*` is stale — maintainer must supply the rotated endpoint.

Recommended next step: on maintainer's next boot + refreshed credentials, run the §4 checklist in the report to scp the small artifacts and compute the step-3 stats (answers vs 7700, degraded share, INVALID, D vs B vs C vs A).