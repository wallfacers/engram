# 阶段 0 诊断报告：gold 在不在池（027 US1）

**日期**: 2026-08-05
**状态**: 完成 — **GO（B 方向有救，进阶段 1）**
**数据**: [diagnosis-cohort.json](diagnosis-cohort.json)

## 错题来源

- `009-full-B-cattopk/run-1`（1330/1540 = **86.4%**，本地最接近当前基线；cat-top-k + bge-large，009 杠杆总账净 86% 的组成）
- 022 B1 正式基线（85.19%）在远端 AutoDL，本地无其 run 产物；两组错题集合应高度重叠（同一 store 家族 + hybrid 检索）
- **84 题** = temporal 59 + multi-hop 25

## 诊断结果（三信号）

| 信号 | 结果 | 方法 |
|---|---|---|
| **写入覆盖** | **84/84 = 100%** | gold 的 evidence 原文（`qa.evidence` 定位到消息）在 store `memory_entries` 中可 token 重叠匹配 |
| **session recall**（top-30） | **89.6%** | hybrid 检索（bge-large + fts + entity），coverage-only（零 answer/judge 调用），命中条目覆盖 gold session |
| **turn recall**（top-30） | **2.4%** | 同上；命中映射到 gold 确切 turn 的 chunk |

## 判定：GO

**STOP 门（gold 不在对话 / 不在池）不成立**：

1. **gold 在写入侧**（100% 覆盖）——信息没有在抽取/分块中丢失
2. **gold 在检索可达的池**（session 89.6%）——检索 top-30 命中了 gold 所在会话的相关条目
3. **错题机制** = 关系/时序表达 + 检索精确命中弱（turn 2.4%）——**这正是 B 的写入侧 event 结构（双视角事实+关系、时间锚定、跨事件合并）针对的问题**

结论：B 方向的前提（gold 在池但答不对）成立，**值得进阶段 1 先导**（本地 sidecar 抽 event，`--only-questions` 配对验证端到端转化）。

## 诚实边界

- **turn recall 2.4% 含伪影**：fact 命中计入 session 但不计入 turn（turn 只认 chunk→turn 映射）。真实的「gold 信息在 top-30 命中内容」介于 2.4% 与 89.6% 之间。阶段 1 的配对结果（端到端正确率）是最终裁判，不需在此精确化。
- **per-question top-30 命中内容未逐条导出**（harness 未存 hits），人审抽样仅覆盖写入覆盖层（`diagnose_phase0.py` 输出了 6 条 strong 样例）。
- **与记忆对照**：coverage 诊断 2026-07-22（oracle 0.965，差距在 chunk 排序）与本结果方向一致——信息在池，问题在检索/打包粒度。

## 阶段 1 的启动条件（已满足）

- [x] 本地 sidecar 就绪：bge-large-en-v1.5（1024d）embedding @ 8002（`embed_server.py`）
- [x] 复用 store：`009-bge-chunks-store`（fact + chunk，`--chunks` 建）
- [x] 先导白名单：`phase0-ids.txt`（84 题）＋ `--only-questions` formal 子集模式
- [ ] 引擎侧 `memory/eventstore/`（event 抽取 + fail-closed + 可重建投影）——阶段 1 实现（tasks.md Foundational）
- [ ] event 抽取 LLM sidecar（本地 vllm 7B/35B）——阶段 1 启动时准备

## 复用资产（供阶段 1）

- `diagnose_phase0.py`：gold evidence 原文 vs store 覆盖的 token 重叠判定
- `coverage-run-hybrid/coverage.json`：top-30 candidate oracle 原始输出
- 8002 bge-large sidecar：阶段 1 检索复用
