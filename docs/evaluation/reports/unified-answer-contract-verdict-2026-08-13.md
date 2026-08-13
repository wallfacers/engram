# Unified Answer Contract — Paired Eval Verdict (2026-08-13)

Spec: [038-unified-answer-contract](../../../specs/038-unified-answer-contract/spec.md) ·
Protocol: [evaluation-protocol.md](../../../specs/038-unified-answer-contract/contracts/evaluation-protocol.md)

## 一句话结论

`--unified-answer-contract`(数据集无关的统一证据约束回答契约)在 **LoCoMo
全量(严格配对)non-inferiority 通过但 within-noise**(+0.6pp, p=0.477),**LME
(post-hoc)+4.3pp(preference 类 +38.9pp 大幅受益)**;但 **LoCoMo cat3 open-domain
-6.2pp / LME assistant -4.1pp**(`don't guess` 对合理推断过度保守),不能判定 GO。
**保持 default-off**,不推荐产品启用,除非先细化「推断 vs 拒答」边界。

## 评测配置

- **配对**:control = `hybrid`(legacy category 契约栈),treatment = `hybrid+unified`。
  唯一变量 = answer system-prompt digest(`unifiedAnswerContractPrompt` vs legacy)。
- **context parity**:fail-closed,`sha256_of_actual_provider_answer_user_bytes` 全题校验。
- **数据集**:LoCoMo 1,540 题(category 1-4)。store = 032-store 快照。
- **模型**:answerer = Qwen3.6-35B-A3B-FP8(vllm, thinking on),judge = deepseek-v4-flash。
- **retrieval**:top-k 30 / chunk-quota 12 / hybrid 三信号,`--no-idk-retry --trace-mediation=false`。
- **repeats**:3,多数票。**成本**:Qwen 本地免费 + judge 付费 API,judge 成本近 0。

## 环境修复(本次评测前置,非 harness bug)

1. **embed 并发非确定**:vllm embed 并发 ≥4 时对同文本返回微差向量 → 检索边界
   漂移 → context parity 校验失败。修复:`--max-num-seqs 1` 重启 embed(并发 48
   下 64/64 确定)。
2. **answer vllm SSE 卡死**:`max-model-len 16384` 触发长思考时 vllm 200 但 SSE
   流不结束 → harness `ParseSSE` 无限阻塞。修复:`--max-model-len 32768` 重启。

两者均为环境配置修复(非 harness bug),重启脚本:`start-embed.sh` /
`restart-answer-vllm.sh`。

## 结果

### Behavior smoke probe(17 case × 3 reps × 2 arms)

| 臂 | majority pass |
|---|---|
| control(legacy) | 15/17 |
| unified | **17/17** |

control 失败的 2 个 case 恰是契约要修的:`unresolved-drink-conflict`(legacy 从
冲突证据硬选一个值)、`useful-advice-without-profile`(legacy 无记忆时过度拒答)。
unified 均通过。dev smoke 仅验证 harness + 机制方向,非 promotion 证据。

### Pilot(60 题子集 × 3 reps)

context parity 3 run 全过。flips 3:2(net +1),within-noise(样本不足)。

### Full LoCoMo(1540 × 3 reps × 2 arms = 9240 answers)

- 3 run validation receipt 全 valid(qcount=1540, parity 全过)。
- **OVERALL(多数票):control 87.1% vs unified 87.7%(+0.6pp)**;McNemar p=0.477,
  verdict=within-noise。
- **flips:control→unified 32 / unified→control 39**(net +7/1540 给 unified)。

**Category 分解(多数票)**:

| category | n | control | unified | Δ |
|---|---|---|---|---|
| 1 multi-hop | 282 | 90.8% | 90.8% | 0.0 |
| 2 temporal | 321 | 87.5% | 86.6% | -0.9 |
| 3 open-domain | 96 | 63.5% | **57.3%** | **-6.2** |
| 4 single-hop | 841 | 89.3% | 91.2% | +1.9 |

**拒答式错误**:两臂相同(82 = 82,占 wrong 的 43%/45%)——unified 没有增加拒答,
整体赢来自 cat4。

### Open-domain 回落机制

case `conv-2-q-64`("What job might Maria pursue in the future?" gold="Shelter
coordinator, Counselor"):unified 长篇思考后结论「No specific future job is
mentioned → I don't know」,control 能从志愿者经历合理推断。unified 的
`don't guess` 规则对**基于记忆的合理推断**过度保守,牺牲了 cat3 推断类题。
flips 佐证:cat3 上 control-ok/unified-wrong 8 题 vs 反向 2 题。

## 结论

- **LoCoMo non-inferiority gate**:+0.6pp ≥ -0.5pp、无显著回归 → 通过。
- **critical slice gate**:cat3 open-domain 实质回落 -6.2pp(unexplained)→ 不通过。
- **unsupported false-answer gate**:拒答率未升,但未验证 unsupported 下降目标。
- **LME(post-hoc)**:见下节。

**verdict:NO-GO(整体),方向 valid(机制在 preference/single-hop 类有实质价值)**。
unified 契约的「减少 unsupported + 避免有害拒答」方向被 smoke probe 与 LME 证实
有效,但 `don't guess` 规则误伤合理推断类题(open-domain / assistant),须先解决
再考虑转正。

## LME(post-hoc,非配对)

**因 embed 上限退化为非配对**:LME store 的 chunk 有 0.04% 超过 bge-large 512
token 上限,embed 400 → 038 配对模式 fail-closed 拒绝出分。改为**两臂分开跑、
无 force-answer(对称)、容忍 embed 降级**。无 context parity 校验,为 post-hoc
兼容性诊断,不能支撑 promotion。

- control(legacy)**:86.1%** ci=[84.3, 87.8](per-run 86.8/85.4/86.0,稳定)
- treatment(unified)**:90.4%** ci=[85.7, 95.1](per-run 88.4/90.6/92.2,上升趋势)
- delta **+4.3pp**(非显著,ci 重叠;preference 类 +38.9pp 为主)

**Category 分解(多数票)**:

| question_type | control | unified | Δ |
|---|---|---|---|
| single-session-preference | 58.9% | **97.8%** | **+38.9** |
| multi-session | 82.5% | 89.2% | +6.7 |
| knowledge-update | 82.1% | 84.2% | +2.1 |
| temporal-reasoning | 87.5% | 88.2% | +0.7 |
| single-session-user | 98.1% | 99.0% | +0.9 |
| single-session-assistant | 96.4% | 92.3% | -4.1 |

**LME 解读**:unified 在 **preference 类大幅受益(+38.9pp)**——正是「避免有害拒答」
的核心目标(probe 的 `useful-advice-without-profile` 已预示),也解释了 LoCoMo
cat4(single-hop,含 preference)的 +1.9pp。但 assistant 类 -4.1pp 与 LoCoMo
cat3(open-domain)-6.2pp 同源:unified 对「需要综合/推断」的题过度保守。
unified 的 per-run 上升趋势(88.4→92.2)值得后续排查(repeats 漂移或学习效应)。

## 最终结论

- **LoCoMo(严格配对,1540)**:+0.6pp within-noise;cat3 open-domain -6.2pp。
- **LME(post-hoc 非配对,500)**:+4.3pp;preference +38.9pp;assistant -4.1pp。
- **方向一致**:unified 契约「减少 unsupported + 避免有害拒答」在 preference 类
  验证有效(preference 是 LoCoMo cat4 / LME 的双重核心类),但 `don't guess` 对
  推断类过度保守,是统一契约的两面。
- **verdict:NO-GO(不转正),default-off 保持**。建议后续迭代:细化「推断 vs 拒答」
  边界(只对敏感/未支持的事实拒答,允许 grounded inference),然后重跑
  LoCoMo + LME(严格配对需解决 embed 512 上限)。
- **held-out gate 未跑**(需 149+ 题 blinded 人工标注),未满足 promotion 前置。
