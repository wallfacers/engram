# Phase 0 Research: Multi-hop Chunk-First Contract Repair

## R1 — 根因边界

**Decision**: 把缺口限定为 multi-hop entity ordering 没有执行 030 的全局 chunk-first 优先级。
当前旧排序与旧 renderer 对固定输入保持相同 evidence 行序；不得声称旧版已发生 units/prompt 漂移。

**Rationale**: `orderHitsForAssembly` 的 multi-hop 分支绕过通用 `kindRank`，`groupHitsByEntity` 组内
只按 score 排序，高分 fact 因而能位于低分 chunk 前。只改单侧 sorter 后，旧 renderer 会重新按实体
合并，才会产生 units/prompt 漂移，所以两处必须成对设计。

**Alternatives considered**:

- 把问题归因于检索：拒绝；64/64 control/treatment 输入候选集合可保持一致。
- 声称旧 renderer 已重排 units：拒绝；旧 flat group order 经 first-encounter partition 可重建原序。

## R2 — 唯一顺序真相

**Decision**: 采用 canonical flat sequence + streaming renderer。排序器一次生成完整 evidence 顺序，
renderer 不再 partition/sort，只在连续段边界写实体 header。

**Rationale**: 这是同时满足 chunk-first、实体组织、prefix cap 与 units/prompt 一致的最小结构。截断只需
删除 canonical 尾部，下一轮 renderer 与 relation block 自然消费同一 prefix。

**Alternatives considered**:

- 显式 layout IR（kind layer→entity group→member）：契约强，但会扩散 assembly/relation/token 接口，
  对 top-30 harness 过度设计。
- chunk/fact 各调用一次旧分组 renderer：renderer 仍拥有排序权，并把 coverage 语义改成分层 coverage。
- 直接按 `(kind, entity, score)`：丢失旧版 coverage-first 实体优先语义。

## R3 — 确定性顺序

**Decision**: 全局 kind 顺序为 chunk→fact；每层实体组按全候选 coverage desc、entity asc；组内
score desc、SourceID asc、原始 ordinal 兜底；每层 ungrouped 最后。

**Rationale**: 保留旧版实体 coverage 语义，同时使同分结果不依赖输入排列或 map 迭代。SourceID 是
既有稳定标识，ordinal 只在异常重复 ID 下保证稳定。

**Alternatives considered**:

- 只用 stable score sort：依赖输入顺序，不能满足重复运行/输入置换合同。
- 按 kind 后全局 score：gold-rank 离线信号更强，但完全移除 entity grouping，混入第二个机制变化。

## R4 — 实验 control

**Decision**: 提供默认关闭的 benchmark-only legacy entity order。64 题跑三臂：无 assembly baseline、
legacy assembly、repaired assembly；全量通过主门后只跑 baseline/repaired。

**Rationale**: `assembly=false` 能回答完整 repaired assembly 是否有望突破 90，但不能隔离这次合同修复；
legacy arm 才能在同 binary、同时间窗口下做单变量归因。全量省去 legacy 避免无必要付费。

**Alternatives considered**:

- 两个 commit/二进制顺序跑：可行但更易受模型时间漂移影响，resume receipt 也更弱。
- 不保留 legacy control：实现最简单，但无法满足 spec 的独立归因要求。

## R5 — cap 与候选闭包

**Decision**: “候选不变”限定为 assembler 输入闭包；cap 后 treatment 输出是新 canonical sequence 的
最长可预算 prefix，不要求与 legacy 保留相同条数或 ID。

**Rationale**: 顺序变化会合法改变尾部截断结果。强求 cap 后集合一致会否定 chunk-first 的主要作用，
也与 030 prefix-truncation 合同冲突。

**Alternatives considered**:

- 先选与 legacy 完全相同的 admitted set 再重排：人为保留旧排序造成的淘汰，削弱合同修复。

## R6 — 确定性候选答案选择

**Decision**: 不把纯文本候选 selector 纳入本 feature。

**Rationale**: 旧三跑离线留一验证中，最好严格 gold-blind 规则仅达到 1380/1540（89.61%，净 +9）；
唯一超过 90 的规则依赖全数据 correctness 选型，是评测泄漏。它不能提供所需净 +16。

**Alternatives considered**:

- 文本众数、medoid、最长答案、问句类型规则和 nested ranker：均未在 conversation-held-out 条件下过 90。
- LLM 候选裁决：属于独立 benchmark-only feature，需单独 SDD 与 label-free packet，不用于挽救本实验。
