# 016 判决台账

## G-尺子门（T021）：**未达标 0.901 < 0.95 —— 按规格停在此处**

**日期**: 2026-07-26 · **状态**: 已停止，等待维护者裁定是否重新登记判据后继续

### 实测

smoke 集：30 题 / 6 题型全覆盖 / 24 有证据 + 6 无证据 / 712 条消息
（T019 覆盖抽取，两次运行 sha256 相同）。

配方：`--chunks --chunk-quota 12 --top-k 30 --coverage-only --retrieval hybrid`，
embedding = 本地 bge-large-en-v1.5（1024d），抽取 = 隧道内 vllm。
**答题模型调用 0、判分模型调用 0**（`--coverage-only` 结构性保证）。

| 题型 | 精确轮次覆盖 | n |
|---|---:|---:|
| knowledge-update | **1.000** | 4 |
| single-session-assistant | **1.000** | 4 |
| single-session-user | **1.000** | 4 |
| single-session-preference | 0.917 | 4 |
| temporal-reasoning | 0.750 | 4 |
| multi-session | 0.742 | 4 |
| **OVERALL** | **0.901** | **24** |

6 道无证据题按 FR-007 判为 `gradeable=false`，正确排除在 n 之外（G4 通路已验证）。
`single-session-preference` 出现即证明 G1 通路已验证。

### 门失败的原因**不是**门所声称的原因

规格给本门的理由是「覆盖率不达标 ⇒ 读取或计量有误（oracle 按构造零干扰项，
覆盖率理应接近满分）」。四项独立诊断否定了这个解释：

| 诊断 | 结果 | 排除了什么 |
|---|---|---|
| 建库侧天花板：每条黄金轮次是否落在某个 chunk 的血缘里 | **1.000，6 型全满，0 题低于 1.0** | 排除 loader / DiaID 合成 / chunk 血缘出错 |
| 全量读取时 `evidenceUnmatched`（oracle + s_cleaned 各 500 题） | **0** | 排除黄金证据指向不存在的 turn |
| 同一 store、去掉 chunk 配额（`--chunk-quota 0 --top-k 300`） | **OVERALL 1.000** | 排除检索取不到黄金轮次 |
| 向量完整性（反连接，非计数比较） | `missing=0`（另有 298 个孤儿向量） | 排除 Backfill 有界队列静默丢弃 |

**结论：尺子本身正确。0.901 是 canonical 配方在 12-chunk 预算下的真实覆盖上限。**

### 附带发现：`--chunk-quota` 在 coverage 模式下被静默钳到 12

`coverage.go:262` 以 `traceSelection=true` 调用 `selectorForArm`，因此
`pcic.go:51` 的短路条件不成立、**总是**返回选择器；无 `--pcic`/`--oracle` 时落到
`default: pcicSelect(...)`，而 `pcic.go:252-253` 将 budget 硬钳为 12。

实证：`--chunk-quota 12 --top-k 30` 与 `--chunk-quota 40 --top-k 100` 给出
**逐位相同**的 0.901；`--chunk-quota 0`（绕开配额路径）才给出 1.000。

即：在 coverage-only 下，任何大于 12 的 `--chunk-quota` 都无效果且无提示。
这不是 016 引入的，是既有行为，记录在此供后续裁定。

### 为什么 LongMemEval 比 LoCoMo 更吃预算

缺口全部集中在 `multi-session` 与 `temporal-reasoning` —— 证据跨会话最多、
黄金轮次最分散的两型。LongMemEval-oracle 单题 3 个会话、每会话 12–25 条消息，
黄金轮次可散落在多个 chunk 中；12 个 chunk 的预算装不下。LoCoMo 的会话更短，
同样预算下不吃紧。这是**benchmark 结构差异**，不是实现缺陷。

### 待裁定

按 tasks.md T021 与 quickstart 的字面规定，门未过 ⇒ 停止本特性。已停止，未进入 P2。

**不在此处自行调整判据** —— 判据是测量前登记的，看过数字之后由执行者放宽阈值，
正是本特性要防的那类操作。是否以「无预算约束下覆盖上限 = 1.000」为修正后的尺子门、
重新登记并继续，属于维护者决定。

## 最终判决（T036）

**未执行** —— US2/US3 未启动。
