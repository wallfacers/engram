# Contract: 强化时序推理契约文本 + 不变量

冻结本 feature 的外向契约:契约文本(答题系统提示)与三处不变量。实现前冻结,单测据此断言。

## C1: 强化 `forceTemporalAnswerPrompt` 文本

替换 `cmd/locomo-bench/runner.go` 现有 `forceTemporalAnswerPrompt` 常量为(措辞可微调,但四锚 + 终局约束语义 MUST 在位):

```text
You answer a temporal question about a long conversation using ONLY the retrieved
memories provided. TEMPORAL REASONING PLAN (reason silently, then output only the
final short answer):
1. ENUMERATE: list every candidate memory's [event: YYYY-MM-DD] date before deciding.
2. RELATIVE→ABSOLUTE: if a memory phrases time relatively ("next month", "last week",
   "two days ago"), resolve it to an absolute date using that memory's own [event:]
   date as the anchor, then answer with the absolute date. Never echo the relative phrase.
3. EXACT MATCH: lock onto the question's temporal constraint exactly. If the question
   asks about an event "in May", only a memory dated [event: YYYY-05-*] qualifies —
   a memory dated April or June is a DIFFERENT event, never "close enough".
4. DURATION ARITHMETIC: for "how long / how many days/months/years" questions, identify
   the START and the END memory by their [event:] dates and compute the difference;
   answer with the duration (e.g. "about 3 months"), not a date.
5. Output the absolute date in natural language (e.g. "21 July 2023"), never ISO format.
   Keep the answer to the shortest phrase that fully answers the question; do not restate it.
6. This is an answerable evaluation: always give your best supported answer; never decline.
```

### 单测可断言的稳定 token(措辞微调时须保留)

- 锚1 枚举:含 `ENUMERATE` 且提及 `[event:` 
- 锚2 相对→绝对:含 `RELATIVE` 或 `relative`,且含 `anchor`,且含 `Never echo`(或 `never echo`)
- 锚3 精确匹配:含 `EXACT MATCH`,且含 `DIFFERENT event`(反 ±1 的反例句)
- 锚4 时长:含 `DURATION` 且含 `START and the END`
- 终局:含 `never ISO` 语义(`never ISO format`)+ `never decline`

> 单测断言这些**语义 token**而非整段逐字,给措辞留微调空间但锁住四模式覆盖。

## C2: 不变量(FR-007)

`answerPromptForRegime` 在下列输入下返回值 MUST 与本 feature 前**完全一致**:

| 输入 | 期望返回 |
|---|---|
| `(1, force, temporalAnswer=任意, abstain=false)` | `forceMultiHopAnswerPrompt`(force)/`multiHopAnswerPrompt` |
| `(3, force, *, false)` | `forceOpenDomainAnswerPrompt` / `openDomainAnswerPrompt` |
| `(2, force, temporalAnswer=false, false)` | `forceAnswerSystemPrompt`(开关关,不走契约) |
| `(2, *, *, abstain=true)` | `abstainAnswerPrompt`(abstain 优先) |

仅 `(2, forceAnswer=true, temporalAnswer=true, abstain=false)` 的返回从旧契约变为新强化契约。

## C3: 引擎零改(FR-008)

`git diff --name-only -- memory embedding provider store internal` MUST 为空。契约改动 100% 落 `cmd/locomo-bench`。

## C4: e2e 门契约(US2)

- 命令形态:canonical recipe(`--top-k 30`,**无 cat-top-k**)+ `--repeats 3`,三臂 base/old-tplan/new-tplan,box 全本地栈,固化 store。基线是干净 top-k 30 bge-large(维护者规范:默认 30,cat-top-k 这类"大力出奇迹"只作后续无奈之举,不进默认门)。
- 通过条件(SC-001):`new-tplan` vs `base` category-2 配对 McNemar 显著抬 **且** overall 不回退。
- 归因(SC-004):报告 `new-tplan` vs `old-tplan` 差分。
- 纪律(SC-005):干净复跑基线(冷启动首臂丢弃);跑完核 `regime.json` 四要素。
