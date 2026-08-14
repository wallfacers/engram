# CLI Contract: Confidence-Gated Iterative Retrieval

`cmd/locomo-bench` 新增 flags（均默认关闭，关闭时行为与固定 top-k 逐字节一致——FR-001/SC-003）。

## New Flags

| Flag | 类型 | 默认 | 说明 |
|---|---|---|---|
| `--confidence-gated` | `bool` | `false` | 开启迭代机制。`false` 时所有 `--confidence-*` 忽略，行为与当前一致 |
| `--confidence-shallow-k` | `int` | `30` | 第一轮检索深度（浅） |
| `--confidence-deep-k` | `int` | `150` | 第二轮检索深度（深） |
| `--confidence-threshold` | `float64` | `3.0` | 犹豫强度 `Score` 门槛：`Score >= threshold` 触发加深。**3.0 经 032-think3 3-rep 校准冻结**（2/3 run 为 PASS 带最优点；更松 fp 爆 39%，更严 recall 掉 <50%）。 |
| `--confidence-max-rounds` | `int` | `2` | 迭代轮数上限（`>=2`；超限即停，FR-004） |
| `--probe-hesitation` | `bool` | `false` | US1 离线区分度探针：读 results-hybrid.jsonl，出混淆矩阵 + 门槛判定 |
| `--probe-hesitation-jsonl` | `string` | `<run-dir>/results-hybrid.jsonl` | 探针/校准的输入文件 |
| `--confidence-calibrate` | `bool` | `false` | US3 阈值 sweep：对阈值区间重算区分度 + 预算曲线，出 `confidence-calibrate.json` |

## Flag 约束（校验失败 → 报错退出）

- `--confidence-gated` 开启时，`--confidence-deep-k` 必须 `> --confidence-shallow-k`。
- `--confidence-gated` 开启时，禁止与 `--multi-query` / `--cat-top-k` 组合（这两个会改 `--top-k` 语义，组合语义未定义；错误信息明确提示）。
- `--confidence-gated` 开启时，formal B1 冻结模式（`--protocol formal-b1` 或等价冻结路径）**禁止**——迭代的 `RetrievalCallLimit`/`AnswerCallLimit` 为 2，与冻结协议冲突（research Decision 5）。错误信息提示「041 迭代走独立 opt-in 路径，不与冻结协议共用」。
- `--confidence-threshold` 必须 `>= 0`。
- 其余规则（`--top-k` 与冻结协议冲突等）沿用既有校验。

## 关闭态黄金规则（SC-003）

`--confidence-gated=false`（默认）时，本 feature 的所有代码路径**不得**触碰检索、prompt 拼装、作答、判题。零字节差异。用既有 golden 基线测试（`TestAnswerPromptGoldenBaseline` 同款）守护。

## 数据依赖

- `--confidence-gated` 复用现有 `--top-k`（作 shallow 语义参考）与 `--chunk-quota`（传给 `retrieveWithQuotaDiagnostics`）。
- `--confidence-shallow-k` 默认 30 = 现有 `--top-k` 默认，保持语义一致。
