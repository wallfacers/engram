---
title: LME 超长 chunk 成因与截断影响评估 — 2026-08-14
summary: 实测 LME-S 500 数据：395,121 chunk 中仅 122 个（0.031%）超 bge-large 512 token 上限，成因为内嵌 SVG/XML/base64 代码、浮点数值表、非英文本三类杂讯记忆（token 密度为英文 2-3 倍）。用 answer_session_ids 精确验证：0/122 来自 answer 所在 session，与 question 无语义关联 → 截断对答题零影响，仅需让这些 chunk 能成功 embed 即可恢复 038 配对。无需换模型重建 store。
status: verdict
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
tags: [evaluation, longmemeval, embedding, fail-closed, diagnostic]
---

# LME 超长 chunk 成因与截断影响评估

## 背景

038 unified-answer-contract 配对被 fail-closed 整批拒绝出分，根因是 LME store 的
0.04% chunk 超过 bge-large-en-v1.5 的 512 token 上限，embed 请求 400 → 两臂
context 不一致。本文用本地真实数据实测：**这些超长 chunk 到底是什么、截断是否
影响答题**，以决定修法（截断 vs 换模型重建 store）。

## 方法（本地全量实测，非抽样）

- **数据**：`testdata/longmemeval/longmemeval_s_cleaned.json`（LME-S cleaned 500 对话）。
- **切分**：精确复刻 `cmd/locomo-bench/chunks.go` 的 `buildSessionChunks`
  （`--chunk-target-chars 900` / `--chunk-max-chars 1100`，超长 turn 无损拆分）。
- **token 计数**：`BAAI/bge-large-en-v1.5` 真实 tokenizer。
- **gold 定位**：`answer_session_ids` ⊆ `haystack_session_ids` 精确对应，
  超长 chunk 逐条回溯到所在 session，判定是否承载 answer。

## 结论一：为什么超长 —— 三类杂讯内容，不是长英文

| 内容类型 | 数量 | token 密度（vs 英文 ~0.25） | 示例 |
|---|---:|---:|---|
| 浮点数值表格 | 44 | ~0.60 | 多行 `-0.489 35.693 5.100` |
| 内嵌 SVG/XML/base64 代码 | 26 | 0.69–0.92 | `data:image/svg+xml,...`、`<svg>`、hex 串 |
| 非英文本（阿拉伯/韩语等） | 15 | 0.81–0.92 | `أطلق العنان لإمكانات التداول`、`전체 계획` |
| 其他/混合 | 37 | — | — |

- chunk 按 **1100 code points（字符）**切分；纯英文 1100 字符 ≈ 277 token，永远到不了 512。
- 超长只发生在 **token 密集内容**：代码标签/URL 编码、每数字 1-2 token 的浮点表、
  每字符 1 token 的非英文本——这些都是对话记忆中的杂讯，不是答案载体。

## 结论二：截断对答题零影响

1. **0/122 超长 chunk 来自 answer 所在 session**（`answer_session_ids` 精确验证，
   无一命中）——没有任何超长 chunk 承载 gold answer 来源。
2. 122 个分布在 **49/500 对话、37 个 session**；与所在对话 question 的显著词重叠
   仅 4 个停用词级弱匹配（from / days / first / about），无语义关联。
3. 机制上：**embedding 只影响检索，不影响答题**——answer 读的是检索到的 chunk
   **原文**，truncate 不触碰存储原文。这些杂讯 chunk 截断后甚至可能更少被
   检索到（顺带去噪）。

## 结论三：038 fail-closed 是杂讯放大

0.031% 的杂讯 chunk（代码/数值表/外语）无法 embed → 整批配对 fail-closed。
这是"极小比例杂讯放大为系统性失败"，不是检索或答题能力问题。

## 修法建议

- **截断即可，无需换模型重建 store**。vllm OpenAI-compatible embeddings API 支持
  `truncate_prompt_tokens`（`-1` = 自动截断到模型 max length）；vllm 为左截断
  （保留尾部 k token），对杂讯内容无所谓。
- 两臂对同一 chunk 用同一向量 → **context parity 保持，配对公平**。
- 实现两选一：
  1. `embedding.Client` 加可选 truncate 配置（类似现有 `Dimensions`，默认不传）——
     属 engine 最小改动，需 TDD + 宪法 IV 评估；
  2. vllm 服务端 `pooler_config` 配置截断——零代码改动。
- 重建 store 的成本（~51 万条重嵌、13G、跨模型历史分数不可比）**无必要**。

## 诚实边界

- 分析基于 LME-S 数据 + 复刻的 chunk 切分逻辑；box 实际 store（512,662 entries）
  的 chunk 构建可能略异，但超长占比 0.031%（本机）与 verdict 记载的 0.04% 同量级。
- "截断不影响答题"的结论依赖 gold 定位精度：`answer_session_ids` 指向 answer
  来源 session，是 LME 官方字段，定位可信；语义重叠检测为补充证据。
- 未实测截断后 122 个 chunk 的实际检索排名变化（无 box）；但因其不承载 gold 且
  与 question 无关，对正确答题路径无影响。
