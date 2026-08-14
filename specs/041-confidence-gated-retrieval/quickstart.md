# Quickstart: Confidence-Gated Iterative Retrieval（041）

验证本 feature 端到端成立的可运行场景。实现细节见 `tasks.md`，契约见 `contracts/`。

## 前提

- `CGO_ENABLED=0 go build ./...` 零错误（硬门禁）。
- 引擎零改动：`git diff --name-only -- memory embedding provider store internal` 为空（宪法 II）。

## 场景 1：关闭态黄金（SC-003，纯 opt-in）

```bash
go run ./cmd/locomo-bench --data testdata/locomo/locomo.json --run-dir .locomo-run-control --top-k 150
go run ./cmd/locomo-bench --data testdata/locomo/locomo.json --run-dir .locomo-run-gate  --top-k 150 --confidence-gated=false
```

**期望**：两个 run-dir 的 `results-hybrid.jsonl` 逐题 `predicted/correct` **逐字节一致**（无 `--confidence-*` flag 参与时，代码路径与当前固定 top-k 完全相同）。用 diff 验证。

## 场景 2：犹豫检测器确定性 + 区分度（US1 生死前提）

```bash
# 在既有 run（top-k 30 / 150）的 results-hybrid.jsonl 上跑检测器（离线，零新模型成本）
go run ./cmd/locomo-bench --probe-hesitation --data <...> --hybrid-jsonl <topk30-run>/results-hybrid.jsonl
```

**期望**（research Decision 2 门槛）：
- **确定性**：同一 predicted 重复跑 → 同一 `(hesit, deepened)`（单测守护）。
- **答错题犹豫率 ≥ 60%**（040 人工判读 89%，自动规则留余量）。
- **答对题犹豫率 ≤ 30%**（假阳性 = 过度加深 = 预算白烧）。
- 报告输出混淆矩阵（犹豫×答对/答错 2×2）+ 加深率。
- 任一门槛不满足 → **US1 停线**，不进入场景 3（spec US1 Acceptance 3）。

## 场景 3：迭代机制配对评测（US2，宪法 IV 门禁）

```bash
# 基线（当前高分线，top-k 150）
go run ./cmd/locomo-bench --data testdata/locomo/locomo.json --run-dir .locomo-run-base --top-k 150
# 迭代（浅 30 → 深 150）
go run ./cmd/locomo-bench --data testdata/locomo/locomo.json --run-dir .locomo-run-iter --top-k 30 --confidence-gated --confidence-shallow-k 30 --confidence-deep-k 150
```

**期望**（配对，3-rep 多数票，同 judge）：
- 正确率 vs 基线（90.13%）**无统计显著回退**（配对显著性口径，同批同 judge）。
- **加深率**落在预期区间（~15%，即假阳性受控）。
- **平均输入 token** 显著低于基线（首版预期 ~2.9× 省，research Decision 4）。
- run-dir 产出 `conf_gate_decisions.jsonl`（每题审计）。

## 场景 4：边界与降级

| 场景 | 期望 |
|---|---|
| `--confidence-gated` + `--multi-query` | 报错退出（CLI 契约约束） |
| `--confidence-gated` + formal B1 冻结 | 报错退出（research Decision 5） |
| `--confidence-deep-k` ≤ `--confidence-shallow-k` | 报错退出 |
| `LOCOMO_NO_THINKING=1`（无 thinking） | 不崩；犹豫信号降级到 final 规则 + isIDK；最终回退固定深度语义（FR-005） |
| answer₁ 空回答 | 按弱信号走，不特殊崩溃 |

## 快速冒烟（无 LLM，单测）

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'Confidence|Hesitation|ConfidenceGate'
```

覆盖：检测器确定性、规则边界（空/无 thinking/拒答）、迭代流程（stub answerer 返回可控犹豫/自信）、关闭态零字节差异、flag 校验。
