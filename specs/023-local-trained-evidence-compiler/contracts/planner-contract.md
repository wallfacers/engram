# Local Planner 接入契约（023 增量）

**Version**: 023.v1

**消费的冻结合同**: [022.v1 Evidence Compiler Contract](../../022-benchmark-parity-memory-architecture/contracts/compiler-contract.md) 的 `Optional Planner` 接口
**实现**: `cmd/locomo-bench/local_planner.go`（T002，commit `2ed473a`）
**Spec**: [023 spec.md](../spec.md) FR-016/017/019/020/021/022/034

## Boundary

`localPlanner` 是 harness 侧 adapter，消费 022 冻结的 `evidencecompiler.Planner` 接口：

```go
type Planner interface {
    Propose(ctx context.Context, query string, candidates []Candidate) (Proposal, error)
}
```

它只向一个自托管 sidecar（vllm/ollama，OpenAI 兼容）转发 query + 冻结 candidates，
并把 snake_case 响应映射回 `evidencecompiler.Proposal`。它：

- **无 Store / Search / Bundle 写 / answerer 权限**（FR-016）；不重新检索、不扩大候选池（FR-017）；
- 所有 proposal 必须经 022 原样 validation、lineage allowlist、source/span 复原、token cap
  和 fail-closed 规则（FR-018），023 不得放宽校验；
- 未配置、超时、崩溃、合同不兼容或 proposal invalid 时退回 022 确定性 Compiler（FR-019）；
- engine 零改动：`git diff --name-only -- memory embedding provider store internal` 必须为空。

## Sidecar 接入形状（Integration Shape）

Planner 模型作为**本地 sidecar** 接入，与 embedder/LLM 的 sidecar 模式一致（复用
`provider.Provider` 抽象），OpenAI 兼容 `/v1` 端点。

### 配置（flag / env）

| flag | env | 语义 |
|---|---|---|
| `--compiler-arm planner` | — | 启用 planner 编译臂；unset = 确定性 extractive fallback |
| `--planner-base-url` | `PLANNER_PROVIDER` 相关（经 `buildBenchProvider("openai", …)`） | sidecar 端点 base URL |
| `--planner-model` | — | sidecar 服务的模型名（如 `Qwen2.5-7B-Instruct`） |
| `--planner-timeout` | — | proposal 超时（0 → `defaultPlannerTimeout` = 6s） |

构造条件：`--compiler-arm planner` **且** `--planner-base-url` 非空 **且**
`--planner-model` 非空 → 实例化 `localPlanner`；否则 planner 保持 `nil`，Compiler 静默
降级确定性 extractive 路径（`--compiler-arm planner` 缺 sidecar 配置时明确 Warn 并降级）。

### 推理服务约束

- sidecar 必须可自托管、离线可达（FR-021）；正式与推荐路径不依赖任何托管服务。
- 服务方必须是同一模型（`--planner-model` 与产物冻结值一致，见下节模型摘要核对）。
- 每次 Propose 一个 proposal；sidecar 输出经 `stripJSONFence` 容忍 ```` ```json ```` 包裹。

## Wire Format（model-facing snake_case）

模型输出的 proposal JSON 与 `local_planner.go` 的 `parsePlannerProposal` 对齐，是
adapter 的私有 wire format（与 [data-model.md §5](../data-model.md) 的 target 一致，
训练输出可直接进 adapter，无二次转换）：

```json
{
  "need": {
    "entities": ["Alice"],
    "time_constraints": ["2024-05-01"],
    "operands": [{"name": "count", "satisfied": false}],
    "list_cardinality": {"known": false, "count": 0},
    "update_state": "",
    "gap": null
  },
  "actions": [
    {"kind": "KEEP", "candidate_id": "c0", "source_id": "s7"},
    {"kind": "EXTRACT", "candidate_id": "c2", "source_id": "s2",
     "span": {"source_id": "s2", "start_char": 4, "end_char": 10, "span_digest": "sha256:…"}}
  ]
}
```

- `actions[].kind` 只能是冻结 union：`KEEP` / `EXTRACT` / `DROP` / `MERGE` /
  `FETCH_SOURCE`；未知 kind → proposal invalid → fallback（永不部分采纳）。
- `need.gap` 最多一个合法 `StructuredGap`（kind ∈ entity/time_range/second_operand）；
  低置信度或自由文本 "need more context" 不是有效 gap。
- 时间字段 `YYYY-MM-DD` 或 RFC3339；`parsePlannerTime` 解析失败 → fallback。
- schema 改动必须同步改 Go `parsePlannerProposal`（data-model.md §5 关键一致性）。

## 合同版本校验 + 模型摘要核对（FR-022）

每个可评测 Planner 产物 MUST 具有并 fail-closed 核对：

| 摘要 | 含义 | 漂移行为 |
|---|---|---|
| 合同版本 | `023.v1`（消费 `022.v1` proposal 合同） | 不匹配 → 拒绝加载/正式 replay |
| prompt digest | `plannerSystemPrompt` + `renderPlannerPrompt` 的 digest（训练资产） | 与冻结值不一致 → run invalid |
| tokenizer/chat-template 摘要 | Qwen2.5 tokenizer（vocab 151,936）+ ChatML template digest | 与冻结值不一致 → 拒绝 |
| 模型摘要 | 底模 + adapter digest（model-card.md，T022） | 与已批准配置不一致 → 拒绝 |
| 数据版本 | 训练数据 build_version（data-model.md §5） | 与产物声明不一致 → 拒绝 |

`protocol.json` 的 `models.planner` 必须显式记录
`{enabled, id, revision, provider, prompt_digest}`；空也必须 `enabled=false` 表达，
不能省略产生歧义（022 evaluation-artifacts §protocol）。Planner 产物声明与已批准配置
不一致时，不得静默使用近似配置（spec US3-SC4）。

## 超时语义 + Cancellation 传播（FR-019）

`localPlanner.Propose` 用 `context.WithTimeout(ctx, p.timeout)` 包裹 sidecar 调用：

| 情形 | 判定 | 结果 |
|---|---|---|
| 调用方 `ctx.Canceled` / `DeadlineExceeded` | `ctx.Err()` 非 nil（调用前/调用后） | **原样传播**，fallback 与 answerer 调用均为 0，不伪装成功降级 |
| Planner 自身规划超时 | `timeoutCtx.Err() == DeadlineExceeded` 但 `ctx.Err() == nil` | `errPlannerUnavailable: planning timeout` → 确定性 fallback |
| sidecar 调用失败 / 输出无法解析 / invalid proposal | 普通 error | `errPlannerUnavailable` → 确定性 fallback |

`errPlannerUnavailable` 故意**不是** `context.Canceled`/`DeadlineExceeded`，使 022
orchestrator 的 fallback 分支触发而非 propagate 分支。确定性 fallback 与 answerer
调用数均保持 022 parity（FR-020）。

## Prompt 资产一致性（训练 ⇄ 推理）

`plannerSystemPrompt` 与 `renderPlannerPrompt(query, candidates)` 是 **023 训练资产**：

- `train_lora.py` 的 `SYSTEM_PROMPT` + `render_user()` 必须与上述 Go 常量**逐字一致**；
  改任何一处两处一起改并重跑配对（否则训练分布与推理分布错位）。
- 每轮配对 eval 记录 planner prompt digest 到协议 fingerprint，用于 replay 校验。

## 错误与降级契约

| Failure | Result |
|---|---|
| Planner 未配置（`--compiler-arm planner` 无 sidecar） | Warn + 确定性 Compiler，Search/write parity 绿 |
| sidecar 不可达 / provider error | `errPlannerUnavailable` → 确定性 fallback；Store/retrieval 不受影响 |
| 规划超时（Planner 自身） | fallback；逐题记录 `fallback_reason` |
| 调用方 cancellation/deadline | 原样传播；fallback 与 answerer 均为 0 |
| proposal JSON 无法解析 / 未知 action kind / 非法 gap | fallback（永不部分采纳） |
| 合同版本 / 模型 / prompt / tokenizer digest 漂移 | 拒绝加载或 run invalid，不静默近似 |

正式配对时，Planner 臂实际 fallback 的题目必须单列 `fallback_rate`，按实际确定性路径
计分，不从 treatment 分母静默删除（022 evaluation-artifacts §Stage-specific Arms；
023 spec Edge Case）。逐题记录 fallback reason。

## Required Tests

实现面已由 `local_planner_test.go` 覆盖（9 测试，全绿）；契约面在此冻结测试矩阵：

1. 合法 proposal（Need + KEEP/EXTRACT/MERGE + span/sentences）映射正确；
2. ```` ```json ```` code fence 容忍；
3. provider error / 无法解析 / 未知 action kind → `errPlannerUnavailable`（fallback，
   非 cancel/deadline）；
4. 调用方 cancellation 原样传播（`errors.Is(err, context.Canceled)`）；
5. Planner 自身规划超时 → fallback（plain error，非 deadline 传播）；
6. 空 config / 空 model 拒绝构造；
7. 渲染的 user prompt 携带 query + 每个候选 id/text（contract-shape guard）；
8. 配对时：Planner 臂 fallback 题单列 `fallback_rate`，候选逐字节一致、answerer 调用 =1；
9. 无 Planner 配置时 Bundle/answer-input digest、错误语义与 022 确定性路径 parity。
