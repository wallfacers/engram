# Research: Confidence-Gated Iterative Retrieval（041 Phase 0）

决议基于：040 verdict（`docs/evaluation/reports/040-adaptive-topk-verdict.md`，56 题诊断 + 89% 犹豫发现）、三篇论文综合（`memory/iterative-retrieval-paper-synthesis.md`：TeaRAG 2511.05385 / When Should Active RAG Retrieve 2607.24010 / Know Before You Fetch 2606.29959）、以及 `cmd/locomo-bench` 既有代码结构（runner.go / chunks.go / eval_runner.go 已读）。

## Decision 1: 犹豫信号规则集（确定性文本启发式，零模型成本）

**Decision**: 犹豫信号 = 对 answerer 生成文本（thinking + 最终答案）应用**确定性文本规则**，加权求和得犹豫强度分，阈值判定「加深/停」。不用 logits（需 answerer 端点暴露，配置成本高）、不做模型自主判断（029 教训）。

**Rationale**: 论文综合结论「确定性不确定信号 > 模型自主判断」。流式犹豫检测（从正常生成提取）已在 040 verdict 事后分析验证可行（"not sure"/"no information"/猜测语气可辨识）。文本规则可单测、确定性（FR-002）、从正常生成提取（FR-003，无额外 closed-book probe）。

**规则集（初版，参数化可调）**：

| 权重 | 信号 | 示例 | 来源 |
|---|---|---|---|
| 3（强） | 明确不确定词 | "not sure"/"uncertain"/"not confident" | thinking+final |
| 3（强） | 拒答 | isIDK（"don't know"/"no information"/"not mentioned"） | final（复用 `isIDK`） |
| 3（强） | 多候选列举后无决断 | thinking 里 "either X or Y"、"possibly X, or Y"、多个备选值未收敛 | thinking |
| 2（中） | 猜测语气 | "could be"/"might be"/"may be"/"possibly"/"maybe"/"perhaps" | thinking+final |
| 1（弱） | 低确信修饰 | "I think"/"I guess"/"I believe"/"probably"/"likely"/"approximately"/"around" | thinking+final |
| 1（弱） | 空洞输出 | 空回答 / 极短（<3 tokens）/ 疑问式收尾 "?" | final |

**Alternatives considered**:
- **logits/log-prob 置信度**（Know Before You Fetch 用 length-normalized sequence log-prob）：更强但要 answerer 端点暴露 log-prob（vllm 可，但 config 成本高），且需额外处理多轮。列为可选增强（US3 之后），首版不依赖。
- **prompt 显式 MORE_INFO 契约**（TeaRAG 式）：让 answerer 自己输出「需要更多信息」→ 仍是模型自主判断 → 029 翻车点，否。
- **流式实时检测（边收边判，可提前终止生成）**：省生成 token，但 answerer 最终答案通常在末尾、提前终止收益不明，且复杂度高。首版**事后检测**（生成完整后扫文本）——语义上满足 FR-003（复用正常生成，无额外 probe）。

## Decision 2: 检测器验证方法（US1 生死前提）

**Decision**: 用**已有 run 的 results-hybrid.jsonl**（top-k 30 与 150，`correct/gold/predicted` 已含）做全量 1540 题标注验证，不跑新模型。口径 = 检测器对每题 predicted（3-rep 多数票）给出犹豫判定，对照 correct 标签算混淆矩阵。**门槛（预赛冻结，US3 校准微调）**：
- **答错题犹豫率（hesitation recall on wrong）≥ 60%**（040 实测 89%，留 30pp 余量——检测器是自动规则，不如人工宽松）
- **答对题犹豫率（false-positive hesitation）≤ 30%**（否则过度加深，省不了预算）

**Rationale**: 040 的 89% 犹豫是基于 56 题的**事后人工判读**，自动规则必然比人工紧。必须先证明自动检测器在两个方向上都有区分度，才谈迭代。两个方向都必须在**全量**（非仅 56 题）上验证——答对题也要跑，因为假阳性 = 过度加深 = 预算白烧。

**Alternatives considered**:
- 仅看 56 题（答错子集）：只测 recall，不测假阳性，会高估可用性。否。
- 新跑一次浅检索 run 取 thinking：现有 30-run 已含 thinking predicted（LOCOMO_NO_THINKING=0 跑的），复用，零成本。

## Decision 3: 迭代结构（两轮，浅 30 → 深 150）

**Decision**: **两轮迭代**：shallowTopK=30（当前默认）→ deepTopK=150（91.10% 高分线）。第一轮 30 检索作答，检测犹豫；犹豫 → 第二轮 150 检索重答；自信 → 停。**maxRounds=2**，第二轮后无论是否仍犹豫都用第二轮答案（超限即停，FR-004）。多轮渐进阶梯（30→60→150）为后续扩展，首版不做。

**Rationale**: 两轮与 040 verdict 的数据完全对齐（30 与 150 都是已验证的 run 口径，可直接配对）。浅=默认值（保持 flag 语义一致）、深=已验证高分线（避「跳到 150 最稳」，verdict 原文）。maxRounds=2 把每题的 answer call 数从 1 提到 ≤2，边界清晰。

**Alternatives considered**:
- **多轮渐进**（30→60→90→150）：更深预算省更多，但每轮一次 answer call + 每次重答都改变 generation 状态，预算计数和正确率权衡更复杂。首版不做，US3 后评估。
- **单轮深浅结合**（浅 top-k 检索 + 深 top-k 同时给出，answerer 自行选择）：仍是自主判断，否。

## Decision 4: 成本计量（上下文 token，非 answer call 次数）

**Decision**: 省预算的计量口径 = **平均输入 token（证据 token）**（用现有 `token_counter`），辅以「平均证据条数」。**不是** answer call 次数——迭代的加深题会多一次 answer call（LLM 生成成本），但 vllm 本地 answer 生成便宜（near-free box），主要成本是喂给 answerer 的上下文 token。

**经济学（预期）**：~85% 题自信停 30（~30 条证据）、~15% 题犹豫加深（30+150=180 条）。平均 ≈ 0.85×30 + 0.15×180 = **52.5 条 ≈ 2.9× 省**（vs 150）。若犹豫率控制不佳（假阳性高），加深比例上升，平均预算逼近 150——这正是 Decision 2 的假阳性门槛要防的。

**Rationale**: SC-002 说「平均证据消耗显著下降 + 正确率无显著回退」。token 口径是产品真实成本（上下文是主要税），比 top-k 条数更诚实。「省 4.8×（收敛 ~31）」是完美信号下的 oracle 上限（040 verdict），**非承诺值**；首版预期 ~2.9×，够显著即可。

**Alternatives considered**:
- 以 answer call 次数为成本：错误（加深题 call 数翻倍，但生成便宜）。
- 以 top-k 条数为成本：低估 token（每条证据长度不均）。

## Decision 5: frozen protocol 隔离（opt-in 新路径，不动 formal B1）

**Decision**: 迭代走**独立 opt-in eval 路径**，不复用也不修改 `materializeFormalB1Question` / `formalFrozenQuestion` / `evalBudgetProtocol{RetrievalCallLimit:1, AnswerCallLimit:1}`。新增 flag `--confidence-gated`（默认关）；开启时用既有函数（`retrieveWithQuotaDiagnostics` per-call topK + `answerCall` + `buildAnswerContextPrompt` + `answerSystemPromptForEval`）搭自己的浅→深循环，**不改这三个函数签名**。迭代决策审计写 run-dir 独立文件 `conf_gate_decisions.jsonl`。

**Rationale**: formal B1 冻结协议是 022 验收资产，`RetrievalCallLimit:1 / AnswerCallLimit:1` 是冻结字段；迭代是「2 次检索 + ≤2 次作答」，天然与之冲突。改冻结协议会破坏验收连续性（`eval_treatment_freeze_test.go` 会 fail-closed）。独立路径让 041 可单独评测、单独归因（宪法 IV「评测口径改动与算法分开提交」）。

**Alternatives considered**:
- 扩展 frozen protocol 加迭代字段：动验收资产，高破坏风险，否。
- 在 materialize 里加浅轮分支：污染冻结路径，否。

## 遗留风险（诚实声明）

- **检测器泛化风险**：040 的 89% 犹豫是人工判读，自动规则可能达不到 60% recall。这是 US1 的生死测试——过不了就停（spec US1 Acceptance 3）。
- **假阳性风险**：答对题上也触发犹豫 → 过度加深 → 平均预算逼近 150，省了个寂寞。Decision 2 的 ≤30% 门槛防此。
- **「自信地错」盲区**：~7% 无法被犹豫信号救回（spec SC-004），机制承认此盲区，理论上限 91.75% 是 oracle ceiling 非承诺。
- **thinking 依赖**：eval 须 `LOCOMO_NO_THINKING=0`（91.10% 线就是这么跑的）；若关 thinking，`isIDK` 仍是可用信号，但其余规则失效——回退固定深度（FR-005）。
