# CLI Flag Contracts: 确定性次模证据装填(045)

**Date**: 2026-08-16 | 契约先行(宪法 III):本文件冻结后才开始实现;破坏性变更须 bump 版本号。

## 旗标族(全部默认关;注册在 main.go 新段 `// 045: submodular packing`,不与 044 删除段重叠)

### `--submodular-pack`(bool,默认 false)

机制总开关。false = 现行配方逐字节不变(golden 锁)。true 时装配层从宽池做预算化次模选择。

- 冲突:`--trace-mediation` / `--consolidate` / `--evidence-assembly` / `--nav` / `--iris` / `--utility-stage` 等任何其他装配/机制旗标 → 启动即拒(fail-closed,进 unified_answer_contract_eval.go 冲突表新行)。
- 与 `--chunks --chunk-quota` 的关系:旗标开时 quota 截断被装填**替代**(不再计数配额);旗标关时完全现行。

### `--pack-pool-size`(int,默认 0 = 沿用现行宽池 max(6×topK,300))

仅诊断/消备用;probe 与正批 MUST 用默认值(零重调)。

### `--pack-weights`(string "rel:cover:fac:div",默认 "3:1:1:1")

四项权重。probe/正批/LME MUST 用默认值;改动只允许在显式标注的消融 run(工件记 `ablation: true`)。

### `--pack-budget-anchor`(enum: `paired`(默认) | `mean`)

逐题配对锚(默认)/对照臂全局均值兜底。probe 与正批 MUST `paired`。

### `--pack-aic-gate`(离线门 CLI,不进 eval 主命令)

`engram-locomo-aic --data <locomo.json> --store <032-store> --run-dir <out> [--slice conv0,conv1] [--top150-ref]`——独立子命令风格(与既有 diagnose CLI 族一致),产出:

```json
{
  "slice": {"convs": [0,1], "questions": 304},
  "recipe": {"topK": 30, "chunkQuota": 12, "pool": "current-wide"},
  "normalization": "lower+collapse-ws+substring (frozen 2026-08-16)",
  "current_k30": {"aic": 0.0, "tokens_mean": 0},
  "packed": {"aic": 0.0, "tokens_mean": 0, "singleton_fallbacks": 0},
  "top150_full": {"aic": 0.0, "tokens_mean": 0},
  "gate": {"rule": "packed.aic >= 0.95 * top150_full.aic AND packed.tokens <= anchor", "verdict": "GO|NO-GO"},
  "audit": {"unmatchable_in_pool": [], "per_question": "<jsonl-sidecar>"}
}
```

### `--reverify-042`(ride-along 子命令)

`engram-reverify-042 --data <locomo.json> --store <032-store> --labels <042-collect-dir> --run-dir <out> --concurrency N`——自包含(不 import counterfactual_utility*.go / confidence_deepen*.go);logprob 通道 `temperature=0` 显式;产出 ReverifyReport(schema 见 data-model.md)。

## 工件契约(run-dir 内,gitignored)

| 工件 | 内容 | seal |
|---|---|---|
| `packing_gate.json` | US1 门报告(上 schema) | manifest 冻结后 digest |
| `packing_audit.jsonl` | 每题:池大小/选中集/预算消耗/弃因 | 同上 |
| `probe_paired.json` | 1-rep 配对差 + McNemar + token parity | 同上 |
| `reverify_042.json` | ReverifyReport | 同上 |

## 错误语义

- 池空 → 该题回退现行装配,`packing_audit` 记 `pool-empty`,不失败整 run(宪法 V)。
- 预算锚缺失(对照臂该题无 usage)→ 全局均值兜底 + 审计标记,不中断。
- 旗标冲突 → 启动即 fatal(fail-closed),列出冲突对。
- AIC 门/重验的模型端点不可达(重验)→ 重验子任务失败即整个 ride-along 标 `inconclusive`,不阻塞主 probe。

## 版本

v1(2026-08-16 冻结)。加旗标 = minor;改默认值/语义 = major + 迁移注记。
