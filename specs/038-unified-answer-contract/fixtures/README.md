# Behavior fixtures

`behavior-cases.json` contains 17 development smoke cases. They were written
alongside the unified answer contract to exercise obvious boundaries and the
probe/reporting path. They are not an independently authored held-out set, a
representative sample of production traffic, or evidence that the contract
generalizes.

Passing all 17 cases cannot establish a false-abstention rate of 2% or less.
Promotion requires a separately authored and pre-registered held-out cohort,
with expected/prohibited behavior frozen before model calls and both arms
reviewed by humans who are blinded to arm. The directly-supported slice must be
large enough for its one-sided exact 95% upper confidence bound to be at most
2%; even with zero false abstentions that requires at least 149 independent
directly-supported cases.

Do not copy these case texts into the system prompt or use failures from this
file to tune a prompt and then report the same file as validation.
