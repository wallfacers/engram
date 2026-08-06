---
title: 结构化内容方向地图
summary: 记忆系统里「结构化内容」的三个层面（写侧/存侧/读侧装配），标注已证伪与仍开放的方向；C1（引用链证据门，MemChain）+ D1（token 精确装填，Retain or Consolidate）为下一批本地杠杆最优先项。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-06
canonical_for: [structured-content-directions]
tags: [research, memory, locomo, longmemeval, local-first, memchain, retain-or-consolidate]
---

# 结构化内容方向地图

本文把「记忆系统里的结构化内容」拆成三个层面，逐项标注已证伪 / 已排除 / 仍开放，作为下一批本地杠杆的决策正本。
背景：029（agentic 多步导航）US2 NO-GO 后，「结构化导航」（MemCog 树+图层级）被门禁排除；但**被排除的只是「让 agent 有结构可导」这一种存储形态**，读侧装配结构几乎全部未测，且是本地纯 Go 可做的。

## 一、三个层面

| 层 | 含义 | 代表结构 | 已证伪? |
|---|---|---|---|
| **写侧结构** | 内容被组织成什么 | event 替换 chunk、语义分段（episode）、fact 原子化 | event/图替换 **027/028/014 三次证伪**；语义分段 **未测** |
| **存侧结构** | 索引/组织形态 | 树+图层级（结构化导航）、纯图（MemOS）、多粒度金字塔 | 树+图=029 US3 跳过；图/金字塔=MemOS/NapMem SaaS 线 |
| **读侧装配结构** | 检索之后证据如何组织/约束 | 引用链、查询条件编译、类别条件分组、token 精确装填 | **绝大部分未测**，且是本地杠杆主战场 |

## 二、已证伪 / 已排除（勿再碰）

| 方向 | 证据 | 状态 |
|---|---|---|
| 写侧 event/图替换原文 | 027 / 028-US1 / 028-US2 / 014（时间域第 3-4 次证伪） | 永久排除 |
| 检索侧单次表示改进 | reranker/doc2query/时间窗/IRIS 等 021 六次证伪 | 排除 |
| 结构化导航（树+图层级） | 029 US2 NO-GO 门禁跳过 US3 | 本线排除（结构价值仍开放，见 §三） |
| 图遍历 assoc（multi-hop） | 014 assoc graph e2e NO-GO；Mem0g multi-hop 47.19 < base 51.15 | 排除 |
| 付费云 rerank/recall 涨点 | DEATH RULE | 永久排除 |

## 三、仍开放的结构化方向（按证据强度排）

| 序 | 结构化内容 | 论文证据（alphaXiv 核实） | 本地可实现性 | 状态 |
|---|---|---|---|---|
| **C1** | **引用链证据门（cited-ID grounded trace）**：检索后、作答前，把候选证据重写成带来源引用的紧凑证据链 | MemChain（2607.24097）去 trace **−13.96pp**，post-retrieval 最强单结构；去 evidence plan −6.21pp | ✅ 校验+丢弃纯 Go 确定性；trace 生成需 sidecar opt-in（L1-2） | **未测 · 最优先** |
| **D1** | **token 精确装填（exact-token packer）**：按真实 tokenizer 计算装填，原文优先，压缩（MERGE）默认关 | Retain or Consolidate（2607.17545）：LoCoMo 紧预算 Abstract/Merge 48.0 vs 保留 12.9（+35pp）；**宽松预算 Merge −0.107 显著为负** | ✅ 纯 Go 精确 token 计算（L1-1） | **未测 · 预算下提质** |
| **A2** | **语义分段（episode 边界）**：按语义事件切 chunk，非固定 512-token | EverMemOS（2601.02163）语义分段 89.16 vs 固定 84.52，同候选同预算 +4.6pp | ✅ Ollama sidecar opt-in，默认 900-char（L2-1） | **未测 · 唯一未测的写侧粒度结构** |
| **C4** | **类别条件装配**：temporal 题按时间序、multi-hop 题按实体图组织证据 | 021 IRIS 教训：`temporal≠graph`，须 category-conditional | ✅ 纯 Go | **未测 · 契约已定型** |
| **C5** | **时间锚定装配**：相对→绝对时间标注 + 去歧契约 | 014 曾 **+2.5pp 但不显著**，已 revert | ✅ 纯 Go + prompt 契约 | **backlog 多-rep 确认** |
| **C2** | **查询条件编译（query-conditioned KEEP/DROP）** | LazyMem 无模型退化；022 compiler-arm extractive **83.83% e2e 未转化** | ✅ 纯 Go（L1-3） | **已测 · formal contract 价值在，涨点无** |

## 四、029 直接教训：证据装配结构（地基）

029 US2 的根因 A 是**该 store 裸混合检索以短 fact 为主，导航组装路径无 chunk-quota 保底**，导致 answerer 上下文被 fact 稀释（chunk fraction 1%，~500 vs 基线 3654 tokens）。
任何读侧结构（C1/C4/C5）都建立在「证据先按结构装配」的地基上：`--chunk-quota` 已存在，把它固化为「先 chunk 后 fact + 类别条件排序」的装配结构是纯 Go、零模型、立即可做。

## 五、建议执行顺序

1. **地基**：证据装配结构（chunk-quota 固化 + 类别条件排序）——纯 Go，零模型
2. **C1**：引用链证据门——校验/丢弃确定性纯 Go；trace 生成用本地 sidecar（DeepSeek-flash 已证可行）
3. **D1**：token 精确装填——真实 tokenizer 记账，原文优先，MERGE 默认关
4. **A2**（可选，低优先）：语义分段 bake-off——需 Ollama sidecar opt-in

C1 + D1 的组合即 lever-batch 的 L1-2 + L1-1（Compiler 骨架线），避开全部已证伪路径。

## 与现有文档的关系

- 借鉴批次与约束：[lever-batch-local-vs-saas.md](lever-batch-local-vs-saas.md)（L1-2 / L1-1 定义）
- 029 实测定论：[../evaluation/reports/029-agentic-memory-navigation-verdict.md](../evaluation/reports/029-agentic-memory-navigation-verdict.md)
- 论文逐篇核实：[high-scoring-memory-systems.md](high-scoring-memory-systems.md)
