# ⚠️ 跑分测试注意：unified 契约已支持 standalone 单臂配置（2026-08-15）

> **给并行跑分窗口（6816fc89 会话 / 任何在此 repo 上跑分测试的窗口）**：
> 2026-08-15 提交 `6e23bc5` 改了 `cmd/locomo-bench` 的 unified 契约校验逻辑。
> 若你正在跑或准备跑 locomo-bench 的 unified 相关 run，**先看本节**。

## 改了什么

**之前**：任何含 unified 的臂（含单臂 `--retrieval hybrid+unified`）都被强制当作配对
实验，被 `isExactUnifiedPromptPair`（要求恰好双臂 `hybrid,hybrid+unified`）拒绝，
单臂 unified 跑不了，只能绕道全局 `--unified-answer-contract` + 单臂 `hybrid`。

**现在**（`unified_answer_contract_eval.go`）：
- 单臂 `--retrieval "hybrid+unified"` → **standalone unified run**，直接可用，
  契约经 `optionsForArm` 生效，repeats 不强制奇数。
- 全局 `--unified-answer-contract` + 单臂 → standalone（等价，保留兼容）。
- **双臂 `--retrieval "hybrid,hybrid+unified"` → 配对协议完全不变**（context parity
  fail-closed、奇数 repeats、隔离约束）。这是唯一的 score-bearing 对比协议。

## 对你的影响

1. **若你的 run 用双臂配对（`hybrid,hybrid+unified`）**：行为零变化，照跑。
2. **若你之前被迫用全局 flag + 单臂 hybrid 跑 unified-only**：现在可以直接
   `--retrieval "hybrid+unified"`，更直观。旧的全局 flag 写法仍兼容。
3. **单臂 unified 的 results/stats 文件名**：仍是 `results-hybrid+unified.jsonl` /
   `stats-hybrid+unified.json`（arm 名派生），与你见过的配对 run 一致。
4. **单臂 unified 无 context parity 校验**（`unifiedPairAudit=false`）——这是
   standalone 的固有语义（无 control 可比对），不是 bug。若要 parity 证据，
   用双臂配对。

## 验证

- 全量 `CGO_ENABLED=0 go test -count=1 ./...` / `go vet` / `check-docs` 全绿。
- 新增 `TestStandaloneUnifiedArmAppliesContract` + 更新 `TestValidateUnifiedPromptPairExperiment`。
- 引擎（`memory/ embedding/ provider/ store/ internal/`）零改动。
- commit `6e23bc5` 已在 master。

## 相关

- LME unified@k150 3-rep 补跑（本次窗口做的）：[lme-unified-k150-3rep-2026-08-15.md](lme-unified-k150-3rep-2026-08-15.md)
- 042 配对交接：[042-unified-k150-run-handoff-2026-08-14.md](042-unified-k150-run-handoff-2026-08-14.md)
