# T001 — Setup Scope Record (048)

**Recorded**: 2026-09-01 (implement start) · **HEAD at record**: `d76e835` (master)

This receipt is controller bookkeeping, not a formal score artifact. It records
the execution-scope decisions that later formal receipts must match or supersede
explicitly.

## 1. Active feature / worktree evidence

| Item | Value at record time |
|---|---|
| Active feature | `specs/048-implicit-memory-flywheel` (`.specify/feature.json`) |
| Primary checkout | `/home/wushengzhou/workspace/github/engram` @ `d76e835 [master]` |
| Sibling worktree | `.claude/worktrees/042-counterfactual-evidence-utility` @ `afe9647 [worktree-042-counterfactual-evidence-utility]` |
| 042 scope | `cmd/locomo-bench` (eval-utility path). **048 touches no file under `cmd/locomo-bench/`.** Re-check before any edit that would approach that path. |
| Engine-zero baseline | `git diff --name-only -- memory embedding provider store internal` = empty at record time; must remain empty at delivery (T067/T068). |

## 2. Operator-provided protected-execution mechanism (planned)

The formal `official-dual` series requires an operator-provided isolation
boundary (separate user / container / mount namespace / ACL or equivalent).
**Planned default: per-worker container slots** via the host container runtime —
one independent container per `host × worker_slot`, each with its own
fresh HOME/XDG/cache/session root. The concrete `--protected-exec`
/ `--core-exec` / `--stage-exec` boundary configs are supplied live at
`series prepare` / `holdout generate` time and are never committed; only their
safe digests enter receipts. If the operator substitutes a different mechanism
(users or ACLs), the substitution is recorded in the corresponding
`ProtectedExecutionReceipt` boundary-kind field — this note is not binding for it.

## 3. Planned per-worker evaluated-child identities / capacity

- Worker slots are indexed `1..concurrency` per host. `--concurrency 3` is the
  working default (3 hosts run as independent processes; each host's batch uses
  its own 3 slots).
- Capacity invariant: `isolated_worker_capacity ≥ required_concurrency`, proven
  by the `ProtectedExecutionReceipt` probe matrix before an official-dual seal.
- Child identity is captured per attempt as `child_identity_digest`; slots never
  share identities across concurrent workers.

## 4. Disposable per-case state allocators + controller-only retirement

- Core and holdout use **disjoint allocator sets**: `core-alloc-<series>` and
  `holdout-alloc-<series>`; holdout roots are created only when the holdout leg
  starts, never during core.
- Each case gets a fresh never-reused HOME/XDG/cache/session/container root.
- After child teardown, the controller quarantines the state root/workspace
  under a controller-only retirement boundary long enough for the next child to
  prove (existence-proven) denial, then final-deletes; the last case records
  controller-verified deletion.

## 5. Disjoint author/review vs formal state roots

Author/review ephemeral state roots (recorded in
`author_review_state_roots_digest`) are allocated from a separate namespace than
formal HOME/XDG roots (`formal_state_roots_digest`); the
`ProtectedExecutionReceipt` fails closed if the two sets intersect.

## 6. Protected access-probe matrix / controller-target-proof contract

Per host × worker slot, the exact evaluated child must record:

- denied: `protected-root-traverse | protected-root-list | protected-root-read`,
  `author-review-audit-read`, `author-review-state-read`;
- allowed: `own-workspace-read` (readable);
- when `concurrency > 1`: denied read of every simultaneously active sibling
  workspace;
- before every primary case: denied `prior-case-state-read` and
  `retired-case-workspace-read` against controller-confirmed targets.

Every denied probe carries `controller_target_proof_digest` (target exists,
nonsecret nonce/content digest, inherits real parent policy) recorded
immediately before launch; a `not-found` outcome without that proof is a
failure. The author/review stage uses the same proof pattern per attempt
(`AuthorReviewIsolationReceipt`).

## 7. Engine / LoCoMo zero-scope baseline

- No edits under `memory/ embedding/ provider/ store/ internal/` are in scope
  (FR-016). Delivery check: empty engine diff + green parity/namespace tests.
- `cmd/locomo-bench` and LongMemEval paths are out of scope (FR-017); the LoCoMo
  regression gate therefore does not trigger, and this reasoning is restated in
  the delivery report (T068/T070).
