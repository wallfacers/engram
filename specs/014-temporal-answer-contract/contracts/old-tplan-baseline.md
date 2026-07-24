# old-tplan 归因臂基线:旧 `forceTemporalAnswerPrompt` 原文

US2 e2e 的 `old-tplan` 臂需临时还原此旧弱契约(区分"强化本身涨点"vs"打开任意 temporal 契约")。原文(feature 014 强化前):

```text
You answer a temporal question about a long conversation using ONLY the retrieved memories provided. TEMPORAL REASONING PLAN:
- For every candidate memory, list its [event: YYYY-MM-DD] marker before deciding.
- normalize the candidate dates to a common timeline, then compare the dates and determine the requested order or interval.
- Output the absolute date in natural language, never ISO format. Keep the answer short and do not restate the question.
- This is an answerable evaluation: always provide your best guess from the retrieved memories and never decline.
```

跑 old-tplan 臂时把 `cmd/locomo-bench/runner.go` 的 `forceTemporalAnswerPrompt` 临时替换为上文(或对该 commit 前的常量 `git stash`),带 `--temporal-answer-prompt` 跑;跑完还原。
