# Quickstart: 自适应检索深度验证

本文件是「验证/运行指南」，不承载实现代码。逐题字段与汇总指标见 [contracts](contracts/cli-adaptive-topk.md)，实体推导见 [data-model](data-model.md)。

## 前提

- 已构建的评测 store（复用 032 / topk150 探索的 store，bge-large 1024d）。
- `locomo.json`（完整数据）与本地 embedding sidecar（沿用 `offline-coverage-bakeoff` 的跑法）。
- 引擎零改动；所有验证在 `cmd/locomo-bench` 上完成。

## 阶段 0 —— 诊断先行（US1 生死前提，零模型调用）

跑检索-only 的 headroom 诊断（不调 answerer / judge）：

```bash
go run ./cmd/locomo-bench \
  --data locomo.json --store-dir <store> \
  --chunks --chunk-quota 12 --top-k 150 \
  --adaptive-topk-diagnose --run-dir ./.adaptive-headroom
```

**预期产物**：`adaptive-headroom.jsonl`（逐题）+ `adaptive-headroom.json`（汇总）。

**判定**：读汇总的 `dropped_gold_ratio` 与 `knee_rate`。
- 若 `dropped_gold_ratio` 超过冻结阈值（收缩会丢 gold 的题太多）→ **STOP**，方向不可行，不改任何检索路径。
- 若 `knee_rate` 过低（RRF 离散分数普遍无拐点）→ **STOP**，gap-knee 检测在本栈不可靠。
- 两者都在安全区 → 进入阶段 1。

## 阶段 1 —— 端到端配对（宪法 IV）

对照臂（当前 91.10% 基线，逐字节一致）：

```bash
go run ./cmd/locomo-bench \
  --data locomo.json --store-dir <store> \
  --chunks --chunk-quota 12 --top-k 150 \
  --run-dir ./.adaptive-control
```

自适应臂（同 recipe + 单 flag）：

```bash
go run ./cmd/locomo-bench \
  --data locomo.json --store-dir <store> \
  --chunks --chunk-quota 12 --top-k 150 \
  --adaptive-topk --run-dir ./.adaptive-treatment
```

**预期**：对照臂结果与当前基线逐题一致（SC-003 证明 opt-in 无侵入）；自适应臂平均证据消耗下降，答案正确率相对对照臂无统计显著下降（SC-002），且 chunk-quota 保底不变（FR-003）。

## 判定线（成功标准）

- 阶段 0 通过 → 自适应方向具备实现 headroom。
- 阶段 1 配对「不显著回退」→ 可考虑转正（转正是独立决策，须宪法 IV 门禁）。
- 任一阶段失败 → 保持 `--adaptive-topk` 默认关，记录 verdict，不进入默认路径。
