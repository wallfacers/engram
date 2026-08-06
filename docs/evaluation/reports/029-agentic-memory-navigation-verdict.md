# 029 Verdict: Agentic 多步记忆导航（完整报告）

**Feature**: 029-agentic-memory-navigation · **日期**: 2026-08-06 · **Spec**: [spec.md](../../specs/029-agentic-memory-navigation/spec.md)
**子集**: 84 题（temporal 59 + multi-hop 25）× 3 reps majority · 同 store/answerer/judge/预算
**Verdict**: **NO-GO**（US1 GO → US2 NO-GO，US3/US4 不执行）

---

## 一、US1 零成本诊断：救回空间（GO）

**结论**: `rescueable_share = 0.655`（55/84）≥ 20% → **GO 进 US2**。详见 [diagnosis/us1-verdict.md](../../specs/029-agentic-memory-navigation/diagnosis/us1-verdict.md)。

| 类别 | 计数 |
|---|---|
| topk_hit（单次 top-30 已捞到） | 29 |
| rescueable（模拟多步可救） | 55 |
| not_in_pool / gold_unresolved | 0 |

归因: rewrite 换查询 42（76%）/ deep 换粒度 12（22%）/ follow_entity 1（2%）。gold 深池 rank 52–138，印证深层召回不足。

**方法**: `locomo-bench --nav-diagnose`（Go harness 真实混合检索）+ `tools/nav_diagnose.py`（确定性模拟三分类，不偷看 gold）。

## 二、US2 配对：推理驱动多步导航（NO-GO，008 铁律）

**配对**: 基线（单次检索，现有路径）vs 多步导航（`--nav`，4 步上限，每步工具 JSON 决策）。同 store/子集/answerer/judge。基线 answer-context cap 3600 tokens（对齐基线实测 3654）。

### 4 次机制归因重跑（全部显著负）

| # | 变体 | 修复点 | nav | Δ | McNemar p |
|---|---|---|---|---|---|
| 1 | 首测 | —（Qwen3.6 思考模型输出思考文本+JSON 混合，解析失败→fallback） | 25.0% | −22.6pp | 0.0026 |
| 2 | enable_thinking | vllm `chat_template_kwargs.enable_thinking=false`（0.6s/步纯 JSON）+ `extractNavJSON` 候选遍历 | 32.9% | −16.7pp | — |
| 3 | minEvidence 补足 | final evidence 补足 ≥12 条（导航选择→seen→单次检索） | 34.5% | −13.1pp | 0.043 |
| 4 | chunk-first 组装 | final evidence chunk（>300 chars）优先排序 | 29.8% | −17.9pp | 0.0059 |

**基线**: 47.6%（temporal 50.8% / multi-hop 40.0%）。

### 根因归因

1. **store 检索以短 fact 为主，chunk 需 `chunk-quota` 强制保底（结构性）**。
   裸 `Retriever.Search` 的 top-N 结果 fact 主导：导航 final evidence 平均 ~500 tokens、chunk 占比仅 1%（实测 0.01）；基线 3654 tokens 来自 `--chunk-quota 12` 强制 12 个 chunk slots。导航组装没有 quota → answerer 上下文永远劣化，这是 4 次变体全部显著负的结构性原因。基线的高 token 不是"检索质量"，是 quota 机制。
2. **模型自主导航不转化 US1 理论空间（机制性）**。
   73% 的题模型不 `stop`（4 步 search 到上限 fallback）；模型改写查询命中 gold 的能力远低于 US1 确定性模拟（单词/短语变体）。**「换查询可救」需要 oracle 式改写，35B 自主导航达不到**。US1 的 `rewrite 76% 归因` 在真实导航下不成立——模型写的自然语言查询对 FTS trigram 关键词检索反而不利。

### 模型侧发现（029 特有）

- **Qwen3.6 是推理模型**：默认输出思考文本+JSON 混合（无 `reasoning_content` 分离），导航每步需 `enable_thinking=false` 才快（0.6s vs 13s/步）+ 纯 JSON。
- **导航 decide 复用 answerer 会串思考**：`--nav` 需要独立的 vLLM HTTP caller（`nav_http.go`，harness 内，引擎零改动）。

## 三、衔接与判定

- 写侧 event：027/028/028-US2 三次证伪（时间锚定/蒸馏）。
- 检索侧单次表示/排序：021/013/014 等六次证伪。
- **029：检索侧多步导航（推理介入检索）首次实测证伪**——不是排序/表示问题，是"推理驱动检索本身在当前 stack 负收益"。
- **US1 诊断的方法论教训**: 确定性模拟（oracle 式改写）高估真实模型导航的救回能力；零成本诊断 GO 不保证配对转化（008 铁律仍是唯一 GO 门）。

## 四、出货影响

- `--nav` / `--nav-max-steps` / `--nav-k` / `--nav-diagnose` 全部默认关；关闭时输出与现状逐字节一致（SC-004，T027 验证）。
- 导航代码保留为评测 harness 基础设施（工具 JSON 解析、轨迹 schema、预算记账、fail-closed）。
- **引擎零改动**: `git diff --name-only -- memory embedding provider store internal` 为空。
