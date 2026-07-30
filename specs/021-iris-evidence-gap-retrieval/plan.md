# Plan: Feature 021 — IRIS 证据缺口迭代检索 (US1 MVP)

> 实现设计文档。spec 见 [spec.md](spec.md)。本 plan 聚焦 **US1（temporal IRIS MVP）**——验证核心假设：IRIS 主动迭代能否在同预算下救起 temporal。US1 不过则 STOP，不建 US2/US3。

## 目标与 GO 判据（数据驱动口径）

- 固定轴：009-bge-chunks-store + Qwen3.6-35B 本地 vllm + deepseek-v4-flash judge + `--force-answer` + tk7（chunk-quota 3）+ 3-run majority。
- US1 GO（temporal 子集 n=321，配对 exact McNemar vs flat-hybrid baseline）：temporal Δ **> 0** 且 p **< 0.05**；两臂 `answer_context_tokens_mean` 都 ≈1059（预算对齐，IRIS 不靠多塞证据）。
- 拿到 US1 实际涨点后，再用真实数据校准最终目标口径（overall 超 / 类别翻正）。

## 接入点（engine 零改，全在 cmd/locomo-bench/）

核心函数 `answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery`（main.go:1500）：

```
hits, ... = retrieveQuestionWithDiagnostics(...)        // line 1502  初始检索
prompt = answerPromptForRegime(...)                      // line 1509
predicted = answerWithAbstentionDecision(hits, ...)      // line 1514  答题
if isIDK(predicted): retryWithRewrite → retryWithWiderNet // line 1523  被动 IDK 重试
```

IRIS 在 **1502 与 1514 之间**插入主动闭环，**仅当 `opt.iris` 且 `qa.Category==2`（temporal，US1 范围）时启用**：

```
hits = retrieve(q, topK=7)                  // 初始
acc = hits
for i in 1..k (k=3):
    tier, conf, missing = EvalSufficiency(q, acc)    // 新 LLM 调用（answerCall 复用 Qwen）
    if Sufficient(tier, conf): break                 // EXACT/INFERRABLE 且过阈
    q' = RefineQuery(q, missing)                     // 诊断驱动（升级 rewriteCall）
    newHits = retrieve(q', topK=7)
    acc = Dedup(acc ∪ newHits)[:topK]                 // 仍 tk7 截断 → 送答题器预算对齐
predicted = answerWithAbstentionDecision(acc, ...)    // 1514 原路径，用累积 acc
```

- IRIS 满足后走原 answer 路径（1514）；若 IRIS 后仍 IDK，现有 `retryWithRewrite/WiderNet`（1523）作为兜底保留不变。
- `--no-idk-retry` 仍控制兜底；IRIS 有自己的 `--iris` 开关。

## 新增/改动文件

| 文件 | 改动 |
|---|---|
| `cmd/locomo-bench/iris.go` (新) | `EvalSufficiency`、`RefineQuery`、`irisRetrieve`（闭环）、sufficiency/refine prompt 常量、tier 解析。纯函数 + 复用 `usageModelCaller`/`*memory.Retriever`。 |
| `cmd/locomo-bench/main.go` | (1) `options` 加 `iris bool`、`irisDepth int`；(2) flag `--iris`/`--iris-depth`；(3) `answerAndJudgeWithAbstentionEvidenceDiagnosticsQuery` 在 1502→1514 间插 IRIS（category==2 且 opt.iris）；(4) 把 `retrieveQuestionWithDiagnostics` 的单次调用抽出为可复用的 `retrieveOnce(query, topK, quota)`（IRIS 多轮复用）。 |
| `cmd/locomo-bench/iris_test.go` (新) | EvalSufficiency tier 解析、IRIS 终止条件、dedup+截断、缺证据时不死循环（离线 stub caller + fake retriever）。 |

## EvalSufficiency 契约（核心新组件，最大风险点）

**输入**：question `q` + 累积 memories `acc`（渲染为编号列表，复用 `retrievedMemory.Line()`）。
**输出**（严格 JSON）：`{"tier":"EXACT"|"INFERRABLE"|"PARTIAL", "confidence":0.0-1.0, "missing":"<还缺什么证据的自然语言描述>"}`。
**Prompt 要点**（temporal 专门化）：
- 评估累积证据**作为整体**能否回答 q；temporal 类须检查是否有足够带 `[event: date]` 的时间锚点来确定顺序/区间。
- EXACT=精确直接答案在证据中；INFERRABLE=足够线索可合理推断；PARTIAL=相关证据在但不全。
- `missing` 指出**具体缺什么**（驱动 refine），而非泛泛。

**终止**：EXACT 或（INFERRABLE 且 conf ≥ θ_general=0.70 / temporal θ=0.85）→ 停；PARTIAL 或未过阈 → refine 再检索；达 k 轮 → 用现有 acc 答题（不 abstain，因 --force-answer）。

**RefineQuery**：复用现有 `rewriteCall`（main.go 的 modelCaller），但 prompt 用 `missing` 驱动（"原问题 X，当前证据缺 Y，写一个针对 Y 的检索查询"），锚定原问题防漂移。仅最终 query 生成用 LLM；策略选择规则化。

## 预算对齐（"真实超越"的定义保证）

- 每轮检索仍 `topK=7`；累积 `acc` 合并去重后**截断到 topK=7** 再送答题器（`buildAnswerContextPrompt` 不变）。
- 因此 `answer_context_tokens_mean` 保持 ≈1059（与 baseline 同级）。IRIS 的赢必须来自**选对证据**（充分性驱动），不是**多塞证据**。US1 验收须核对两臂 token 均值对齐。

## 测试计划（TDD）

1. `iris_test.go`：EvalSufficiency JSON 解析（3 tier + missing）；Sufficient() 终止逻辑（EXACT 必停、PARTIAL 必继续、temporal 严阈）；`irisRetrieve` 在 k 轮内终止、acc 不超 topK、dedup 正确；fake caller 返回 PARTIAL×k 时不死循环、最终用 acc 答。
2. 离线 stub（`usageModelCaller` 返回固定 tier；fake retriever 返回固定 hits）——零网络/零模型。
3. 集成：`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/` 全绿；`go vet`；`git diff --name-only -- memory embedding provider store internal` 为空（engine 零改硬验证）。

## 风险（US1 是其直接闸）

- **Qwen EvalSufficiency 可靠性**：Qwen3.6-35B 能否可靠做证据充分性判别（EviMem 用 GPT-4o-mini）。若 tier 不可靠（全 EXACT 或全 PARTIAL），IRIS 退化。US1 数据直接暴露这点。
- **成本/延迟**：每题最多 k 次 sufficiency + k 次 refine + 1 答 ≈ 2k+1 次 LLM 调用（k=3 → 7 次）。本地 vllm near-free；须报告每题平均调用数。
- **预算漂移风险**：若 acc 合并后超过 topK 未严格截断，会偷偷堆预算 → 假超越。测试 + US1 token 核对拦截。

## 执行顺序

1. `iris.go` + `iris_test.go`（TDD：先写解析/终止测试，再实现）。
2. `main.go` flag + options + 接入（category==2 且 opt.iris）+ `retrieveOnce` 抽取。
3. `go test ./cmd/locomo-bench/` + vet + engine-零改 diff → 绿。
4. 交叉编译（CGO=0 linux/amd64）→ 用户开 box → sftp → 跑 `--only-category 2` hybrid vs hybrid+iris，3 repeats。
5. `assoc_mcnemar.py`（已就绪）配对 McNemar + token 对齐核对 → US1 verdict → 校准目标。
