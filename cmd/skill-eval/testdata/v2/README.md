# cmd/skill-eval testdata/v2 — fictional fixtures only

**NO REAL HOLDOUT CONTENT MAY EVER APPEAR IN THIS DIRECTORY.** Every file here
is a fictional, hand-written fixture exercising the 048 v2 schema/receipt/seal
validation paths (see `specs/048-implicit-memory-flywheel/data-model.md`).
Real holdout cases, real prompts with user facts, and any plaintext-bearing
formal receipt live only in the operator-provided protected root outside this
repository. Digests in these fixtures are placeholder hex, not real package or
payload digests; tests that need a true digest compute it at runtime.

Layout (grown by the test tasks that consume it):

- `core-case-v2.json` — valid fictional v2 dev case (implicit module, family-mapped)
- `core-regression-v2.json` — legacy-regression module case with **no `lang`**
  (exercises the `regression_unclassified` language policy)
- `holdout-case-v2.json` — fictional holdout case with authoring + two reviews
- `manifest-core-v2.json` — fictional two-case core manifest (`payload_files`, no seal)
- `blind-candidate-v1.json` — valid recursively closed `BlindCandidateV1`
- `review-record-v1.json` — valid non-author `ReviewRecord`
- `green-test-receipt-v1.json` — passing `series-prepare` GreenTestReceipt
- `package-validation-receipt-v1.json` — passing SkillPackageValidationReceipt
- `invalid/` — inputs that must fail closed, one defect each
