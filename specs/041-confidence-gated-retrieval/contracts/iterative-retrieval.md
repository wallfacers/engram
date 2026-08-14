# Contract: Iterative Retrieval Loop

`cmd/locomo-bench/iterative_retrieval.go` 的公开函数。独立 opt-in 路径（research Decision 5），复用既有函数不改签名。

## `runConfidenceGatedQuestion(ctx, retriever, qa, opt, answerCall, judgeCall) (correct bool, final string, record IterationDecisionRecord)`

单题迭代主循环。**不触碰** `materializeFormalB1Question` / `formalFrozenQuestion` / frozen `evalBudgetProtocol`。

**流程**：
1. **浅轮**：`retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, ShallowTopK, ChunkQuota, nil)` → hits₁
2. `input₁ = buildAnswerContextPrompt(qa.Question, hits₁, qa.QuestionDate, qa.Category, opt.temporalDateScaffold)`；`answer₁ = answerCall(ctx, answerSystemPromptForEval(qa, opt), input₁)`
3. `(hesit₁, deepened₁) = detectHesitation(answer₁)`
4. **若 `deepened₁ == false`** → `final = extractFinalAnswer(answer₁)`；`record{FinalFromRound: 1, Deepened: false}`；直接 judge。
5. **若 `deepened₁ == true`**：
   - **深轮**：`retrieveWithQuotaDiagnostics(ctx, retriever, qa.Question, DeepTopK, ChunkQuota, nil)` → hits₂（可能包含 hits₁ 之外的新证据）
   - `input₂ = buildAnswerContextPrompt(qa.Question, hits₂, ...)`；`answer₂ = answerCall(ctx, answerSystemPromptForEval(qa, opt), input₂)`
   - `(hesit₂, _) = detectHesitation(answer₂)`（记录用）
   - `final = extractFinalAnswer(answer₂)`；`record{FinalFromRound: 2, Deepened: true}`
6. `correct = judgeCall(ctx, buildJudgePrompt(qa.Question, qa.Gold, final))` 解析 verdict。

**FR-005 降级**：若 answer₁ 无 thinking 结构（`LOCOMO_NO_THINKING=1` 或上游无 reasoning）→ `detectHesitation` 只跑 final 规则 + `isIDK`；若 `deepened` 因信号缺失而无法可靠判定，调用方按 `--confidence-gated` 关闭语义处理（即行为等同浅轮固定 top-k）——**不强行加深也不强行收缩**。

**FR-004 上限**：`MaxRounds=2` 硬编码于循环（契约层只允许 1 或 2；`--confidence-max-rounds` 校验 `>=2`）。第二轮后不再迭代。

**cost 记录**：`answerCall` 每次调用返回 `provider.Usage`；两轮的输入/输出 token 分别累加进 run 的成本账本（复用现有 `record` 机制）。平均输入 token 口径见 research Decision 4。

## `evaluateConfidenceGated(opt, runDir) (summary, error)`

批量入口（`--confidence-gated` 开启时的顶层 eval 循环）：对全量/子集题逐题 `runConfidenceGatedQuestion`，落 `results-hybrid.jsonl` + `conf_gate_decisions.jsonl`，产出 `summary`：
- 总正确率（与基线配对口径一致：同 judge、同聚合）
- **加深率** `Deepened / Total`
- **平均输入 token**（浅轮 + 深轮累加，`token_counter`）
- 平均证据条数（浅/深 hits 均值）
- 对每题：round 数、final 来源轮次

## 输出产物（run-dir）

| 文件 | 内容 |
|---|---|
| `results-hybrid.jsonl` | 逐题 `correct/gold/predicted`（与既有格式一致，`predicted` 存最终答案原始文本） |
| `conf_gate_decisions.jsonl` | 每题 `IterationDecisionRecord`（data-model.md 实体 3） |

两者都 gitignore（run-dir 现有规则）。

## 与冻结协议共存（research Decision 5）

- `--confidence-gated` 开启时 **禁止** formal B1 冻结路径（CLI contract 校验）。
- 不修改 `evalBudgetProtocol` / `formalFrozenQuestion` / `materializeFormalB1Question` 的任何字节。
- `eval_treatment_freeze_test.go` 等冻结测试不受影响（不触碰它们的输入路径）。
