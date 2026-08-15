# Data Model: confidence-gated gap-guided deepening (043)

**Date**: 2026-08-15 | **Source**: spec.md Key Entities + plan.md

所有实体只存在于 cmd/locomo-bench harness 运行时与 run-dir 工件,**不落引擎 schema**(引擎零改动)。

## HesitationSignal(犹豫信号)

answerer 在证据不足时的可观测表现。pilot 阶段两种形态并列计算,机制阶段只消费选定形态。

| 字段 | 类型 | 说明 |
|---|---|---|
| kind | enum: `logprob` \| `textual` | 信号形态(pilot 按 AUC 择一后定稿) |
| available | bool | 是否成功解析(false ⇒ 按自信处理,不加深) |
| value | float64 | kind=logprob:三冻结特征之一(final_mean_logprob / final_p10_logprob / final_mean_top1_top2_margin,复用 042 `utilityMapFinalSignal`);kind=textual:犹豫 lexicon 命中强度(0/1 或命中数) |
| featureName | string | 定稿特征名(pilot seal 后冻结,机制臂读取) |
| closedReason | string | available=false 的原因(计入解析覆盖率统计) |

**校验**:value 数值有限;logprob 形态要求 final-span 严格后缀映射成功(042 同款 fail-closed);定稿后 featureName/阈值只读。

## GapItem(结构化缺口)

| 字段 | 类型 | 校验 |
|---|---|---|
| category | enum: `bridge_entity` \| `attribute` \| `relation` \| `evidence_span` \| `other` | 必填,枚举外拒绝 |
| target | string | 可空;≤120 chars |
| slot | string | 可空;≤80 chars |
| description | string | 可空兜底;≤240 chars |

- 领域无关通用槽位,无任何数据集/类别语义(FR-005)。
- 每题缺口块最多 3 条(S2G-RAG 同款上限);0 条合法(等价于不构造补检查询,直接不加深)。

**状态转移**:answerer 原始输出 → 解析(失败=丢弃,信号按自信)→ schema 校验 → 冻结进 DeepenDecision(不再修改)。

## DeepenDecision(每题加深审计记录)

写入 `<run-dir>/public/deepen-decisions.jsonl`(append-only),manifest 收计量。

| 字段 | 类型 | 说明 |
|---|---|---|
| decision_id | string | 稳定 id(conv, question, arm, repeat 派生) |
| triggered | bool | 是否触发加深 |
| signal | HesitationSignal 快照 | 触发依据(含 value 与阈值) |
| threshold | float64 | 本次 run 使用的定稿阈值(只读自 pilot seal) |
| gap_items | []GapItem | 解析存活的缺口(触发时非空) |
| gap_query | string | 确定性拼接出的补检查询(未触发为空串) |
| added_count | int | 去重后实际追加的条数 |
| round0_answer_digest | string | round-0 最终答案 digest |
| deepened_answer_digest | string | 重答最终答案 digest(未触发为空) |
| final_from_deepen | bool | 最终答案是否取自重答 |
| round0_context_digest | string | round-0 上下文 digest(补检前后必须一致,FR-007 校验项) |
| outcome_kind | enum: `none` \| `signal_unavailable` \| `gap_parse_failed` \| `query_empty_fallback_question` \| `search_error` \| `search_empty` | 每题终态分类(含正常回退路径;每题恰好一个终态;analyze F4:命名从 failure_kind 改为 outcome_kind,消除"非失败入失败枚举"歧义) |

## 工件文件与 seal

照抄 042 布局(`counterfactual_utility_artifact.go` 模式):

| 文件 | 内容 | 可见性 |
|---|---|---|
| `manifest.json` | run 计量(含 QuestionCount、arm、阈值、featureName、契约 digest)**——全部字段填满后才算 digest**(冻结前算 digest 的硬规则) | public |
| `seal.json` | manifest digest 签封;下游 stage 强制校验 | public |
| `public/deepen-decisions.jsonl` | DeepenDecision 逐题记录 | public |
| `public/answer-attempts.jsonl` | 两轮答案 + usage + 信号 | public |
| `pilot-report.json` | 双信号 AUC + 通道一致性对照 + 择优结论 + 阈值 | public |
| `hidden/`(如需) | 信号原始 logprob 明细 | hidden,public loader 拒读 |

## 实体关系

```
pilot-report.json (seal) ──定稿 featureName/阈值──▶ 机制 run manifest(只读引用)
DeepenDecision 1──1 question×arm×repeat
DeepenDecision 1──0..3 GapItem
DeepenDecision ──引用──▶ HesitationSignal 快照
GapItem ──确定性映射──▶ gap_query(纯字符串拼接,可复算)
```
