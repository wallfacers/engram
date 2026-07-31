# Contract: IRIS 证据缺口迭代检索循环

**Feature**: 021-iris-evidence-gap-retrieval

**Surface**: `cmd/locomo-bench` 评测适配器

**Status**: implemented, default-off, experiment `closed-no-go`

本文冻结 021 已实现 IRIS temporal MVP 的适配器行为。它是历史评测合同，不代表默认
产品能力；最终实验 verdict 以
[实验裁决索引](../../../docs/evaluation/experiment-verdicts.md)为准。

## 1. CLI 与适用范围

| Option | Contract |
|---|---|
| `--iris` | 默认 `false`；仅显式启用 IRIS |
| `--iris-depth` | 默认 `3`；表示包含初始检索在内的最大检索轮数，小于 1 时回退到 3 |
| category | 当前接线仅对 LoCoMo category 2 temporal 问题执行 IRIS；其他类别走既有路径 |

IRIS 只存在于 benchmark harness，不修改 `memory/`、`embedding/`、`provider/`、`store/`
或 `internal/` 的引擎合同。

## 2. EvalSufficiency 输入与输出

输入由原问题和当前累计、去重后的候选 Evidence 组成。每条候选按现有
`retrievedMemory.Line()` 格式编号渲染；无候选时显式写入 `(none)`。

模型输出 schema：

```json
{
  "tier": "EXACT | INFERRABLE | PARTIAL",
  "confidence": 0.0,
  "missing": "仍缺少的具体事实、日期、事件或实体；EXACT 时为空"
}
```

解析器允许 JSON 外包裹少量文本，但必须执行以下归一化：

- tier 转为大写；未知 tier 降级为 `PARTIAL`；
- confidence 限制在 `[0,1]`；
- 无可解析 JSON 时，仅显式出现 `exact`/`inferrable` 才采用对应 tier，否则为
  `PARTIAL`。

## 3. 终止规则

| Tier | General | Temporal category 2 |
|---|---|---|
| `EXACT` | 立即停止，不依赖 confidence | 立即停止，不依赖 confidence |
| `INFERRABLE` | `confidence >= 0.70` 时停止 | `confidence >= 0.85` 时停止 |
| `PARTIAL` | 继续（未达到深度上限时） | 继续（未达到深度上限时） |

EvalSufficiency 调用失败时停止迭代并使用当前候选答题，不能阻塞整题或无限重试。

## 4. Diagnosis-driven refine

只有尚未满足终止条件且 `missing` 非空时才请求一次 refine。输入必须同时包含原问题和
具体缺口；输出是一条锚定原问题、针对缺口的短检索 query，不得包含解释。

- refine 调用失败：保留原问题，不采用失败输出；
- refine 返回空白：回退原问题；
- 新检索失败：停止迭代，保留已有候选。

## 5. 循环与候选预算

1. 用原问题执行初始检索，得到 `hits0`；初始检索失败则向调用方返回错误。
2. 对累计候选按 `(Name, Content)` 去重。
3. 在剩余轮次中执行 sufficiency → 可选 refine → 同 `topK` 新检索；累计集合只供
   sufficiency 判断，不直接全部发送给 answerer。
4. 最终 answerer 候选由 slot merge 产生：至少保留 1 个、通常保留
   `floor(topK/2)` 个 round-0 anchor；随后填入不在 round 0 的 fresh hits；剩余位置再按
   原顺序补 round-0 hits。
5. 最终集合再次去重且长度不得超过 `topK`。IRIS 不得通过扩大 answer context 获益。

每题仍只调用一次最终 answer 路径；IRIS 的额外调用仅用于 sufficiency/refine/retrieval。

## 6. 失败与产品边界

- IRIS、refine 或 sufficiency 不得成为引擎或默认检索的必需依赖。
- 模型侧失败按上述规则停止迭代并退化到当前候选；不得扩大预算或切换付费 reranker。
- 021 实验结果为 `closed-no-go`：IRIS temporal MVP 在固定低预算口径下显著回归，因此
  `--iris` 保持 default-off，不得从本合同推导 promotion 或产品能力。

## 7. 验证入口

行为测试位于 `cmd/locomo-bench/iris_test.go`，至少覆盖：

- tier 解析、归一化与 confidence clamp；
- general/temporal 阈值；
- refine 错误与空输出回退；
- round-0/fresh slot merge、去重和 `topK` 硬上限；
- sufficiency endpoint 失败时的停止语义。
