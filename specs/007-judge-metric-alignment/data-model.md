# Data Model: LoCoMo Judge 口径对齐

无持久化数据 / 无 schema。以下是本特性的内存/文件级实体。

## judge 口径模式(judgeAlignmentMode)

| 值 | 触发 | judgeSystemPrompt 规则 | fingerprint 标签 |
|---|---|---|---|
| `strict`(默认) | flag off | 现有严格规则(逐字不变) | (无) |
| `mem0-aligned` | `--judge-mem0-aligned` | Mem0 7 条宽松规则 + 收口 | `;judge=mem0-aligned` |

- 承载:一个从 options 传入 judge 调用点的模式值(bool 或枚举)。
- 不变量:`strict` 模式下,对同一 (question, gold, predicted),判定结果与改动前逐字一致(SC-005)。

## golden 夹具项(GoldenCase)

`cmd/locomo-bench/testdata/judge_golden.jsonl`,每行一条 JSON:

| 字段 | 类型 | 含义 |
|---|---|---|
| `question` | string | 问题 |
| `gold` | string | gold answer |
| `predicted` | string | 待判的预测答案 |
| `expected_correct` | bool | 期望判定(mem0-aligned 模式下) |
| `rule` | string | 所测规则,如 `partial-credit` / `date-tolerance` / `emotion-valence` / `anti-lure-wrong-name` / `anti-lure-contradiction` / `boundary-14d` |
| `note` | string | 人读说明 |

- 覆盖矩阵(必备):
  - `expected_correct=true`:partial-credit(列表 1/2)、date-tolerance(≤14d)、duration-50%、emotion-valence、paraphrase、extra-detail、same-referent。
  - `expected_correct=false`(anti-放水陷阱):wrong-name、contradiction、wrong-number、zero-overlap、idk("我不知道")、date->14d。
  - 边界:boundary-14d(恰好 14 天,true)、list-zero-hit(零命中,false)、emotion-opposite(反价,false)。
- 不变量:每条陷阱项(expected_correct=false)在 golden 层真跑中 MUST 判 WRONG(SC-001);每条宽松项 MUST 判 CORRECT(SC-002)。

## run fingerprint(既有,扩展)

- 现状(`main.go:1018` 附近):`force_answer=%t;abstain_prompt=%t;no_idk_retry=%t`(+ 可选 `;temporal_answer_prompt=true`)。
- 扩展:mem0-aligned 时追加 `;judge=mem0-aligned`。
- 作用:同 fingerprint 才允许配对/缓存复用;口径不同 → fingerprint 不同 → 隔离(SC-006)。
