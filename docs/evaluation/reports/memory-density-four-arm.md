---
title: 024 记忆密度四臂配对消融（write_dedup × neighbor_extend）
summary: 在 022 冻结协议（cap 3600）下对两个纯本地机制做关/开 × 关/开的四臂配对验证。结论：两个机制单独与组合均为负结果，随机制叠加 accuracy 单调下降（−0.46 → −0.91 → −1.30pp），dedup 在 multi-hop 的正收益被 open-domain 的负收益抵消。按 FR-011，两机制保持默认关并记录 verdict。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-01
canonical_for: [memory-density-four-arm]
tags: [evaluation, locomo, 024, density, paired-ablation]
---

# 024 记忆密度四臂配对消融

## 背景与动机

budget-ablation 证明 engram 同栈领先完全由 answerer 上下文预算驱动：预算从 3614 tok 降到 MemOS 量级（1083 ≈ 1059）后领先反转为 −5.62pp。差距在**同预算下的信息密度**：MemOS 的候选无冗余事实（write-time dedup），且命中后带出相关兄弟 fact（depth-2 图邻居）。

本实验把 MemOS 的两个经源码验证、且与 engram 宪法兼容的机制，作为 022 的**后续增量实验**在冻结协议下配对验证：
- **write_dedup**（写入时冗余抑制）：新增 atomic fact 投影前用 trigram Jaccard（阈值 0.7，FTS 采样 OR 候选）检测与已有事实的语义冗余，抑制重复投影写入。evidence ledger 保持 append-only 无损。
- **neighbor_extend**（命中后邻居扩展）：检索命中 fact 后沿共享 evidence 取 depth-1 兄弟 fact 带进 answerer 上下文（有界、确定性顺序）。

两机制默认关、纯本地、可独立消融。详见 [specs/024-memory-density/spec.md](../../../specs/024-memory-density/spec.md)。

## 方法

- **冻结协议**：022 正式 B1 manifest，`protocol_id=locomo-b1-high`，answerer cap **3600 tok**，Qwen3.6-35B-A3B-FP8 answerer + deepseek-v4-flash judge + mem0-aligned + `--force-answer` + 3 次答题多数票 + 1540 题（LoCoMo cat 1–4）。
- **四臂**（2×2 配对）：

| 臂 | manifest | store 来源 | CLI flags |
|---|---|---|---|
| control | `024-control.json` | control-store（无抑制构建） | — |
| neighbor | `024-neighbor.json` | 复用 control-store | `--neighbor-extend` |
| dedup | `024-dedup.json` | dedup-store（抑制后构建） | `--write-dedup` |
| both | `024-both.json` | 复用 dedup-store | `--write-dedup --neighbor-extend` |

- **归因设计**：write_dedup 是写侧机制 → dedup/both 用抑制后重新构建的 dedup-store；neighbor_extend 是查侧机制 → neighbor/both 复用旧 store。**已知混淆变量**（research.md Decision 记录）：臂 1/2 与臂 3/4 不是同一 store，neighbor×dedup 组合比较含 store 差异。
- **manifest 冻结验证**：四份 manifest 在本地 HEAD 编译的二进制下重冻结（修复了旧二进制 prompt digest 漂移问题），control 与 022 b1-high 资产逐字节一致（answerer digest `8187fec`、embedding 指纹 `a691`、3-key mechanism flags 全对齐）；四份 `protocol_hash` 互不相同。
- **环境**：AutoDL 远程（vllm 8000 answer + 8010 embedding，`HF_HUB_OFFLINE=1` 离线加载），judge 走 DeepSeek API。四臂串行，全部 `EXIT=0`，summary `valid=true`。

## 结果

### 四臂 overall（1540 题，3 次答题多数票）

| 臂 | 机制 | 正确 | Acc | Δ vs control | mean_in_tok |
|---|---:|---:|---:|---:|---:|
| control | 无 | 1298 | **84.29%** | — | 3406 |
| neighbor | +extend | 1291 | 83.83% | **−0.46pp** | 3406 |
| dedup | +dedup | 1284 | 83.38% | **−0.91pp** | 3403 |
| both | 双机制 | 1278 | 82.99% | **−1.30pp** | 3403 |

### 分类别 accuracy（正确/题数）

| 臂 | multi-hop | open-domain | single-hop | temporal |
|---|---:|---:|---:|---:|
| control | 244/282 = 86.52% | 60/96 = 62.50% | 723/841 = 85.97% | 271/321 = 84.42% |
| neighbor | 243/282 = 86.17% | 59/96 = 61.46% | 719/841 = 85.49% | 270/321 = 84.11% |
| dedup | 253/282 = 89.72% | 56/96 = 58.33% | 710/841 = 84.42% | 265/321 = 82.55% |
| both | 247/282 = 87.59% | 58/96 = 60.42% | 710/841 = 84.42% | 263/321 = 81.93% |

（3-repeats 配对统计的 OVERALL 均值与 CI95 见各 run 日志尾部 `repeated stats` 块；summary.json 为单次多数票口径。）

### suppression audit（write_dedup 写入时判定）

| 臂 | decisions | suppressed | suspected_mis | mis_suppression_rate |
|---|---:|---:|---:|---:|
| dedup | 21,860 | 20 | 5 | 25% |
| both | 0（复用 dedup-store，无新增写入） | 0 | 0 | — |

## 结论

1. **两个机制均为负结果（FR-011 → 保持默认关）**。单独开任一机制 accuracy 都低于 control（neighbor −0.46pp、dedup −0.91pp），同开最低（−1.30pp），**单调下降、无叠加收益**。两机制不进入默认路径。
2. **write_dedup 在 LoCoMo 上几乎不触发**：21,860 次判定仅 20 次抑制（0.09%），说明同一事件的近似描述在 LoCoMo 原文中本就罕见；trigram Jaccard 0.7 阈值下的抑制面过窄，对候选密度无实质影响。疑似误伤率 25%（5/20）不可忽略——被抑制的 5 例需核查是否含独立信息（本 feature 不修，作为后续工作）。
3. **dedup 的 multi-hop 正收益被 open-domain 负收益抵消**：multi-hop 89.72%（+3.2pp）可能是抑制冗余后多跳证据更集中，但 open-domain 58.33%（−4.2pp）、temporal 82.55%（−1.9pp）、single-hop 84.42%（−1.6pp）全面下滑，净效应为负。单一类别的正收益不构成整体胜点。
4. **neighbor_extend 全面小幅下滑**：四个类别均低于 control（−0.3 到 −1.0pp），且 CI 更宽。LoCoMo 单条原始消息常被抽取成多个 fact，共享 evidence 的"兄弟"多为同消息内不同 fact 而非跨消息语义相关事实（research.md Decision 5 已预警），扩展带来的边际上下文未提升答题。
5. **既有基线未回归**：control 臂 84.29%（summary 口径）对应 022 b1-high 基线 82.1%（参照，非正式基线；B0 85.32% 为主对照）。四臂全部 valid，protocol_hash 归因成立。
6. **局限**：本实验只在 LoCoMo（1540 题）验证；LongMemEval-S 未跑（见 T021 说明）。neighbor×dedup 组合含 store 差异混淆变量。LoCoMo 的原始消息普遍非冗余可能低估 write_dedup 的真实收益（该机制在更冗余的长程对话上可能有正收益）。

## 失败的正确解读与后续方向（2026-08-01）

**本 feature 在"涨分"目标上是失败的，但作为机制验证是成功的。** 负结果是有信息量的：

1. **两个杠杆在 LoCoMo 上打不动分数，是因为靶子选错了，不是机制本身无效。** write_dedup 需要"同一事件反复近似转述"的冗余输入，而 LoCoMo 单条消息足够干净，冗余罕见（0.09% 触发率）；neighbor_extend 需要"跨消息语义相关"的邻居，而 SQL 血缘的"共享 evidence 兄弟"在 LoCoMo 里多是同消息内不同 fact，不是语义邻居。
2. **这指向下一步应该换杠杆，而不是调参。** 两条实证指引：
   - 要打"同预算信息密度"，候选需要的是**跨消息语义聚类**（真正语义相关的多跳证据聚到一起），而非"共享原文"的机械血缘——这对应 022 未验证的 **H1 semantic episode** 表示。
   - write_dedup 的正收益场景是**长程、多轮重复**的对话（agent 反复汇报同一状态），LoCoMo 测不出来；若后续有此类数据集（或真实 agent 日志），该机制仍可能有效，但**不构成当前默认路径**。
3. **预算差距仍未缩小。** budget-ablation 暴露的"同预算下 MemOS 1059 tok 82.47% vs engram 1083 tok 76.85%"依然存在；本次证明的不是"差距可缩小"，而是"这两个特定杠杆不是缩小差距的手段"。

## 对 022 的影响

本 feature 是 022 的后续增量实验（022 verdict 为 HOLD）。两个密度机制负结果**不影响 022 已冻结资产与基线**；022 的 B0 85.32% / LongMemEval-S 80.80% 主对照不变。budget-ablation 的结论（engram 同预算差距由信息密度驱动）仍然成立——本实验证明的只是"这两个特定机制的实现未能在该预算下追回差距"，不排除其他信息密度路径（如 022 H1 semantic episode、H2 compiler 正式化）。

## 复现

- 冻结 manifest：`specs/024-memory-density/benchmark-registration.md`（protocol_hash 已对齐 022）
- 四臂 run-dir 资产（AutoDL，已归档）：`/root/024-runs/{control,neighbor,dedup,both}/`
- suppression audit：`/root/024-runs/dedup/suppression-audit.json`
- 复现命令：`cmd/locomo-bench` + `--eval-protocol <manifest> --store-dir <store> [--write-dedup] [--neighbor-extend]`
