# US3 Verdict: 条件压缩（配对）

**Feature**: 030-evidence-mediation · **日期**: 2026-08-06 · **子集**: 84 题（029 实际子集）× 3 reps majority

## 结论

**保守 PASS（008 铁律：压缩 arm 不显著回退）。** cons **41.7% vs keep 47.6%**（paired flips A→B=7 / B→A=6，McNemar **p=1.0** within-noise）。但**验证缺口**：精确 tokenizer 未启用（缺 `--counter-fingerprint`）→ 装配用 estimate ledger；consolidate 在装配器 cap 截断下基本未触发（keep≈cons 上下文），压缩机制实际检验不足。

## 配对设计

同 store / 子集（029 实际 84 题）/ answerer（Qwen @8000）/ judge（DeepSeek mem0-aligned）/ cap 3600。keep 臂 = `--evidence-assembly`；cons 臂 = `--evidence-assembly --consolidate`。consolidate 仅当装配 TotalTokens > cap 时触发 sidecar 压缩，预算内 no-op（byte-identical）。

## 结果

| Arm | OVERALL (J) | mean±ci | multi-hop | temporal | context tok |
|---|---|---|---|---|---|
| keep（装配器） | 47.6% | 42.9% [29.3,56.4] | 44.0% | 49.2% | 3684 |
| cons（+consolidate） | 41.7% | 42.9% [35.0,50.7] | 48.0% | 39.0% | 3675 |

`--compare`：flips A→B=7 B→A=6，McNemar p=1.0，verdict=within-noise。**cons 不显著回退。**

## 验证缺口（诚实声明）

1. **精确 tokenizer 未启用**：cons 日志 `"assembly exact tokenizer unavailable... requires base URL and fingerprint"`。`--token-counter-base-url` 传了但 `--counter-fingerprint` 缺（022 formal protocol 校准产物，机器上未留存）。→ 装配 TotalTokens 走 **estimate ledger**（`tokens_estimated=true`），over-cap 判定为近似。
2. **consolidate 基本未触发**：装配器 cap 截断（`assembleEvidence` drop-to-cap）已保证 TotalTokens ≤ cap → 超预算题极少 → consolidate no-op。keep（3684）与 cons（3675）上下文几乎相同印证。**条件压缩在 cap=3600 下无触发空间**；仅在更紧预算（更小 cap）下才有实际检验意义。
3. over-cap 占比 / 压缩后 ≤cap 的数值验证不可用（`--assembly-diagnose` journal 未开，且 estimate 模式不准）。

## 判定

008 铁律（cons ≥ keep，不显著回退）**满足** → 条件压缩**保守 PASS**（无伤害，但也未在 cap=3600 下实际检验压缩效果）。建议若后续评估压缩价值：用更紧 cap（如 1500-2000）重跑，或启用 `--counter-fingerprint` + `--assembly-diagnose` 获得精确 over-cap 分布。US3 代码保留为 opt-in（`--consolidate` 默认 false）。
