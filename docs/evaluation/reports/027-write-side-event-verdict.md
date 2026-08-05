# 027 写入侧 event 时序结构 — 阶段 1 先导配对 verdict

日期：2026-08-05 · Feature：[spec 027](../../specs/027-write-side-event-structure/spec.md)

## 结论

**NO-GO（阶段 1 STOP）。** 用「双视角 event 投影（facts + relations + temporal）替换原文 chunk」作为答题上下文，在 temporal + multi-hop 错题子集上端到端**大降**：chunk 臂 majority **50.0%** vs event 臂 **23.8%**，**−26.2pp**，配对 McNemar **p=0.0016**（显著，非噪声）。008 铁律下不进入默认路径，US3 全量配对不再执行。

## 背景

027 假设（源自 SEGTREEMEM/StructMem 调研）：写入侧把对话抽取成**带时间锚定 + 显式 relation 的 event 结构**，能补上检索侧反复证伪的 temporal/multi-hop 缺口——差距在写入侧结构化而非检索侧。

- **阶段 0（US1，已 GO）**：84 题（temporal 59 + multi-hop 25）gold 原文 100% 在池、session recall 89.6% → 信息在池，错题机制 = 关系/时序表达 + 精确命中弱。
- **阶段 1（US2，本次）**：7B sidecar 抽取 5870/5882 event（99.8%），渲染替换 chunk 做端到端配对。

## 方法（配对纪律）

| 项 | 值 |
|---|---|
| 子集 | 84 题（temporal 59 + multi-hop 25），`--only-questions` formal-subset |
| 候选 | 同 store（009-bge-chunks-store），top-30 hybrid，chunk-quota 12 |
| 臂差异 | 仅表示：chunk（原文 fold）vs event（同 store 事件投影渲染） |
| answerer / judge / token cap | 同（Qwen3.6-35B-A3B-FP8 本地 vllm / DeepSeek mem0-aligned / --force-answer） |
| repeats | 3（answerer temp=1.0 噪声 → majority + McNemar） |
| 判定 | per-question majority（3 reps ≥2 对），配对差异 McNemar exact binomial |

## 结果

| 指标 | chunk majority | event majority | Δ | 配对 McNemar |
|---|---|---|---|---|
| **OVERALL** | 42/84 **50.0%** | 20/84 **23.8%** | **+26.2pp** | a=34, b=12, **p=0.0016** |
| temporal | 29/59 49.2% | 14/59 23.7% | +25.5pp | a=24, b=9, **p=0.0135** |
| multi-hop | 13/25 52.0% | 6/25 24.0% | +28.0pp | a=10, b=3, p=0.092（边缘） |

单 rep 均分：chunk 48.0% ci95[38.5, 57.5] vs event 23.8% ci95[18.7, 28.9]——CI 完全不重叠。

配对表（chunk✓event✓=8 / chunk✓event✗=34 / chunk✗event✓=12 / chunk✗event✗=30）：**chunk 独对 34 题，event 独对仅 12 题**，两个类目方向一致（无抵消）。

成本：两臂各 3 reps 共 1512 次调用，judge/answer 全本地或低价位，event 臂 `answer_context_tokens_mean=2383` 低于 chunk 的 3654（event 渲染后上下文更短）。

## 失败机制

**主导：绝对时间锚定丢失。** 7B 抽取把原文绝对日期泛化成相对词（如 `[EVENT] (rel: "Yesterday")` → 答题器答 "Yesterday"，而 gold 是 "January 9, 2023"）。量化（event run-1 的 63 错题）：

- gold 含绝对日期却答错：**25 题**
- predicted 含相对时间词（yesterday/last week/…）：**47/63 = 75%**

这正是 build-event 时已知的时间锚定弱点（5870 event 仅 5% 带绝对时间）。chunk 保留原文绝对日期，所以 temporal 类目 chunk 显著更好（49.2% vs 23.7%）。multi-hop 类目 event 也未救回：relation 结构无法补偿信息保真损失（52.0% vs 24.0%）。

次要：`fail-closed` 与 schema 校验工作正常（渲染器加载 5870 event、零 invalid），但不是这次失败原因——失败是**结构内信息保真**问题，不是工程 bug。

## 与既有证据链

时间域杠杆累计第 **7 次** NO-GO，这次从写入侧证伪：

- 014 答题侧时序契约 · 017 确定性 date scaffold · 021 IRIS/temporal 检索侧 ×2（b=15 c=45, p=0.000135）· 021 graph ×1（p=0.011）
- **027 写入侧 event 结构（本次，p=0.0016）**

**共识：在 7B 级本地抽取下，写入侧"结构化"以丢失原文保真（绝对时间、字面细节）为代价，端到端净负；检索侧保留原文 chunk 才是信息最保真的写入形态。** MemOS 的写侧结构化（tree/graph + 训练模型）依赖更强的抽取模型/训练，在 7B 非训练抽取条件下不可复制。022 的 "Event/gap/窄 projection 待消融面" 中 **Event 表示一支现收口为 NO-GO**。

## 出货影响

- 事件投影 / `--representation event` / `--build-event-project`：**default-off、不进入默认路径**（FR-010）。
- `memory/eventstore/` 引擎包保留（测试全绿、契约完整、fail-closed），作为**可重建投影能力**的基建；不含时间锚定修复时不构成检索/答题杠杆。
- 若未来想重试写侧结构化：**必须在抽取侧解决绝对时间锚定**（更强的抽取模型 / 确定性 date 解析回填 / 原文时间戳直通），且仍须过同一配对门。
