---
title: 033 chunk-first 合同修复失败分析
summary: 记录 deepseek-v4-pro 32 并发探针的 NO-GO，纠正 gold-in-pool 归因，并给出不依赖云 reranker 的后续可解路径与门禁。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-10
canonical_for: [033-failure-analysis]
tags: [evaluation, locomo, negative-result, attribution, answer-adjudication]
---

# 033 chunk-first 合同修复失败分析

## 裁决

**033 的 chunk-first 排序方向在当前 v4-pro / top-30 / 约 3.3k context 口径下已证伪。**
实现确实把 multi-hop 的全部 chunk 移到 fact 之前，但 B legacy 与 C repaired 在 18 道 multi-hop
题上多数结果完全相同（9/18，零翻转）；64 题主探针 target 净增仅 `+1`，未达到预注册 `+8`。
全量未启动，不能声明超过 90%。

这不是“排序代码没生效”，而是排序改变没有触达真正瓶颈。当前结果同时揭示：先前
`gold_in_pool` 诊断把“至少一个 gold turn 被覆盖”误读成“回答所需的全部证据已经齐全”，并把
“gold DiaID 所在 chunk 出现”误读成“答案承载字段已经进入 prompt”。这两项归因都需要纠正。

## 实测事实

| 事实 | 结果 | 含义 |
|---|---:|---|
| target-32 C−A | `+1`（C-only 2，A-only 1） | 远低于 `+8` 晋级门 |
| guard-32 C−A | `0` | 没有可见反伤，但不足以晋级 |
| B legacy vs C repaired（multi-hop 18） | `9/18` vs `9/18`，零翻转 | 本次 chunk-first 合同修复没有独立答分贡献 |
| `chunk-gold && rank>=19` | 16 题中双方仅 1 题多数正确，零翻转 | 预期靶心没有响应 |
| chunk-gold runtime audit | 57/57 gold admitted，0/57 截断 | 不是 cap 截断救回，只是展示顺序变化 |
| target top-30 任意 gold-turn 覆盖 | 32/32 | 原 `gold_in_pool` 实际表达的弱条件 |
| target top-30 **全部** gold-turn 覆盖 | **26/32** | 6 题仍缺回答链；不能称“无召回缺口” |
| target multi-hop 全部 gold-turn 覆盖 | **4/9**，平均 turn recall 70.4% | 多跳的互补证据缺失仍是真问题 |
| rank>=19 分层全部 gold-turn 覆盖 | 14/16 | 即使多数题证据齐全，排序仍未转成答案 |

有效探针与独立复核见 feature 的 [verdict](../../../specs/033-chunk-first-contract-repair/diagnosis/verdict.md)
和 [review](../../../specs/033-chunk-first-contract-repair/diagnosis/review.md)。

## 为什么原假设失败

### 1. `gold_in_pool` 只证明“任意命中”，不证明回答链完整

`buildAttributionTrace` 将 `GoldInPool` 定义为 wide pool 中出现第一个覆盖任意 gold turn 的候选；
`GoldRankTopK` 同样取第一个 `CoversGold`。它不要求 `mapped_gold_turns` 的并集覆盖
`gold_turns` 全集。

按 top-30 重新计算 union coverage：

- target-32 为 32/32 至少命中一个 gold turn，但只有 26/32 覆盖全部 gold turns；
- 9 道 multi-hop 只有 4 道全覆盖；5 道各缺 1–2 个互补 turn；
- `conv-9-q-60` 需要 3 个 turn，top-30 只覆盖 1 个。它恰好是 A-only 题，C 失败不能归因于
  chunk-first 排序。

因此先前“32/32 gold 在池内，所以没有召回缺口”的结论不成立。后续归因必须同时报告
`any_gold_covered`、`all_gold_turns_covered`、`turn_recall`，不得再以一个布尔值代替完整性。

### 2. DiaID 命中不等于答案承载内容进入 prompt

chunk 的 attribution 只按 `chunkTurns` 中的 DiaID 精确重叠判定覆盖；它不检查 chunk 文本是否
实际包含答案。当前 canonical store 使用默认 `--image-captions=false`，verbatim chunk 不包含
turn 的 `blip_caption`。

人工复核 target 的 16 道 single-hop 残差，**10 道的关键细节只存在于 caption，或必须靠 caption
才能消歧**，包括警示牌文字、`Trans Lives Matter`、紫色头发、向日葵纹身、Nintendo 主机、
沙拉内容、海上落日、沙漠仙人掌画、皮划艇和篮球比赛。它们的 DiaID/chunk 可以被标成
`covers_gold=true`，但答案文本并未进入 answer prompt。移动这些空载 chunk 不可能救回答案。

广泛开启 `--image-captions` 不是现成答案：历史 018 全量配对已测得整体 `−0.71pp`，原因是把
caption 折进所有 turn 后污染检索，尽管 caption 靶心子集净救回 2 题。因此若重试，只能做
**查询期、命中 turn 后的 caption late-binding**，不能重复全店 caption ingestion。

### 3. 证据齐全的 multi-hop 题需要集合/事件运算，而非位置调整

target 中 4 道 multi-hop 已覆盖全部 gold turns，C 仍为 0/4。典型计数题中，chunk-first 已把
全部 gold chunks 提到前 10 位：

| 问题 | gold chunks 的 canonical 位置 | gold | C 三次预测 |
|---|---|---:|---|
| `conv-2-q-62` 收养了几只狗 | 3、5 | 2 | 0、0、0 |
| `conv-4-q-49` 脚踝受伤几次 | 5、7 | 2 | 1、1、1 |
| `conv-5-q-17` 计划徒步几次 | 1、4、9 | 3 | 4、4、5 |

这些错误来自事件同一性、重复提及、跨 session 集合聚合和 benchmark 计数语义。全局
chunk-before-fact 不会告诉 answerer 哪两段是同一事件、哪些是新事件，也不会把多个候选编译成
可计数 ledger。

### 4. 没有发生截断，强 answerer 对 3.3k 上下文的位置变化不敏感

19 道 chunk-gold × 3 repeats 全部保留 30/30 候选。rank>=19 的 gold chunk 在 C 中已移动到
canonical 位置 1–10，但 16 题只有 1 题多数正确。对 v4-pro 而言，这个上下文长度仍可扫描；
排序主要改变注意力先后，没有增加信息，也没有解除 cap。预期中的 lost-in-the-middle 增益因而
缩到采样噪声内。

### 5. 观察到的 `+1` 不是本次 multi-hop 修复贡献

C-only 两题分别属于 single-hop 和 temporal；A-only 一题属于 multi-hop。真正隔离旧/新
multi-hop 顺序的 B/C 是零翻转。因此 `+1` 只能视为完整 assembly 与采样共同产生的描述性波动，
不能归因于 033 的 chunk-first 修复。

## 是否还有可解方式

有，但应换轴。继续调整 chunk/fact 全局顺序、增加 top-k、重复广泛 caption ingestion、叠加通用
trace/prompt，先验都已被当前或历史配对证据压低。以下两条分别面向“尽快验证 >90”与“产品侧
修真正缺口”。它们都不使用付费云 reranker/recall。

### 路线 A（最高优先级）：在 gold-blind 文本不一致集上做 evidence-grounded 候选裁决

历史 89.03 三跑可直接计算一个不进入运行时的 answer oracle：

- majority：1371/1540（89.03%）；严格 >90 需要 1387，差 16 题；
- 三个候选中**至少一个已被 judge 判对**：1411/1540（91.62%）；
- 事后按 judge 标签看，96 道题的三次 correctness 混合：40 道是“多数错但至少一个候选对”，
  56 道是“多数对但有一个候选错”。**这 96 道不能作为运行时 cohort**，因为它由 judge 标签定义；
- 已试的严格 gold-blind 文本规则最好为 1380/1540（89.61%，净 +9），仍差 7 题。

这说明 >90 的**候选空间真实存在**，但需要理解证据的裁决器，而不是字符串投票。下一实验应只
用 label-blind 规则选择调用集：将三个候选 lowercase 并移除非字母数字字符，normalized 文本不全
相同才点火。历史三跑上该规则点火 **771/1540**，不读取 correctness；它覆盖 88/96 个
correctness-mixed rows，其中 35 个可救、53 个可伤，另 8 个 normalized 相同但 judge 标签不一致的
rows 视为 judge instability，不让 selector 追逐。

对这 771 题各调用一次 answer-side v4-pro verifier：输入 question、原冻结 context 和随机排列的
3 个候选，要求引用 evidence 后选择候选索引；不传 gold、judge verdict、correctness 或历史多数
标签。无效/低置信输出回退多数答案，未点火题保持原多数答案。

若在结果冻结后的 88 个 mixed-verdict rows 中至少选对 **69 道（78.4%）**，相对 majority 净增
至少 16，总计即可达到至少 1387/1540；这是事后评分门，88-row membership 绝不能进入执行。
把选择索引映射到旧 candidate verdict 只能作为便宜的 Stage-0 诊断，因为 8 个同文本异 verdict
已证明 judge 有随机性。只有 Stage 0 通过后，才能为正式声明预注册“同一候选生成、同一时间窗口、
独立重判”的 paired control/treatment；不得直接把旧标签映射结果登记成新 >90 分数。

该路线属于 benchmark/SaaS answer-side 编排，不是本地默认引擎分，也不得回填成纯本地产品能力。

### 路线 B（产品侧）：模态 late-binding + operator-specific event ledger

这条路线解决两个真实缺口，但单项未必足够跨 90：

1. **Caption late-binding**：caption 作为独立、本地、带 turn lineage 的投影保存；只有当 query
   具有 photo/sign/poster/painting/tattoo/color/console 等视觉意图，并且对应 turn 已被正文检索命中
   时，才把 caption 附在该证据后。caption 不参与全库默认 embedding/RRF，不重复 018 的全局稀释。
2. **Event ledger**：只对 `how many / what gifts / which ... has` 等集合型问题点火；围绕主体、谓词
   和 session/date 扩展互补证据，把候选编译为 `{event, date, source, same_as}` 行，再让 answerer
   对唯一事件集合计数或列举。必须显式区分“新事件”和“旧事件再次提及”。

两者都必须先过零成本门：在 target 与匹配 guard 上报告完整 gold-turn recall、答案 payload
可见率、ledger 可构造率和理论可救题数；任何一个方向的可达净空间不足 16 题时，不得直接花钱跑
全量。历史通用 compiler/episode 聚合均为负，所以新实验必须限定到视觉/集合 operator，不得再次
全类别改写上下文。

## 推荐顺序

1. 先修 attribution 口径：any/full/turn-recall/payload-visible 四个字段，零付费。
2. 先跑路线 A 的 771 题 gold-blind 点火集；96/88 等 correctness 分层只在输出冻结后用于评分。
   91.62% 的全量候选 oracle 是当前唯一已经越过 90 的实测可达上界，但不是可直接报告的分数。
3. 若 A 未达到 72/96，再用路线 B 生成视觉与集合题的新候选，提高 candidate oracle；不要先改全局
   检索或排序。
4. 任何最终 >90 声明仍需冻结 prompt、随机候选排列、模型、调用量和选择回退，并做独立污染审计；
   付费云 reranker/recall 始终禁止。

## 最终处置

- 033：`closed-no-go`，不合并、不转正、不跑全量。
- chunk-first 合同测试可作为实现知识保留，但不能作为涨点证据。
- 下一条冲 90 路线应单独 SDD；不得在 033 上叠加新机制后重解释本次失败。
