# Unified Answer Contract — Paired Eval Verdict (2026-08-13)

Spec: [038-unified-answer-contract](../../../specs/038-unified-answer-contract/spec.md) ·
Protocol: [evaluation-protocol.md](../../../specs/038-unified-answer-contract/contracts/evaluation-protocol.md)

## 一句话结论

`--unified-answer-contract`(数据集无关的统一证据约束回答契约)**必须作为唯一允许
的 prompt 路径推进**(消除 per-dataset / category 特调 = 产品正确性与可移植性,
特调高分无意义,见 [result-matrix](../result-matrix.md))。**方向已配对坐实**:
LME 严格配对(truncate + context parity + 同批 judge + 3-rep)+4.4pp
(p=0.000112, above-noise;control 85.8% → unified 90.2%),preference +30.0pp
不靠特调;LoCoMo 全量严格配对 +0.6pp within-noise(p=0.477)但 cat3 open-domain
-6.2pp。**「Request classification」修订(2026-08-14)已修复过度保守**:
显式分流 factual recall(严格 do-not-guess)vs inference/prediction/advice
(grounded inference + likely 标注),smoke 20/20(control 11/20)、3 个新边界
case 全过,且不破坏 LME 增益。下一步:LoCoMo 高配(top-k150)配对重跑验证 cat3
救回 + held-out 行为门。

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

## LME(严格配对,2026-08-14)

**truncate 修 embed 512 上限后恢复配对**:LME store 0.031% chunk 超 bge-large
512 token(0/122 承载 gold),`EMBED_TRUNCATE_PROMPT_TOKENS=-1` 后 embed 不再 400。
配对模式(hybrid vs hybrid+unified)+ context parity 校验 + 同批 judge + 3-rep
+ clean + 修订后契约(treatment digest `1d8a8d0f`)。3 run validation receipt 全
valid(qcount=500,context parity mismatch=0)。

- control(hybrid)**:85.8%** ci=[83.5, 88.1]
- unified(hybrid+unified)**:90.2%** ci=[89.2, 91.2]
- delta **+4.4pp**,McNemar p=**0.000112**(above-noise),ci 不重叠
- flips control→unified **33** / unified→control 8(net +25/500)

**Category 分解(多数票)**:

| question_type | n | control | unified | Δ |
|---|---|---|---|---|
| multi-session | 133 | 79.7% | 88.0% | +8.3 |
| temporal-reasoning | 133 | 88.7% | 88.0% | -0.8 |
| knowledge-update | 78 | 83.3% | 91.0% | +7.7 |
| single-session-user | 70 | 97.1% | 97.1% | 0.0 |
| single-session-assistant | 56 | 96.4% | 96.4% | 0.0 |
| single-session-preference | 30 | 66.7% | **96.7%** | **+30.0** |
| TOTAL | 500 | 86.2% | 91.2% | +5.0 |

**LME 解读(配对坐实)**:unified 增益集中在 **preference(+30.0)、multi-session
(+8.3)、knowledge-update(+7.7)**——即「有证据就该答」/「避免有害拒答」的核心
类,正是统一契约设计目标。temporal -0.8pp / assistant 0.0pp 持平——之前
post-hoc 的 assistant -4.1pp 是**配对伪影**(非配对 drift),配对模式下无回落。
90.2% 配对坐实(preference 30 题 control 只对 20,unified 29),非 judge 伪影
(context parity + 同批 flash judge + clean)。

## Request classification 修订(2026-08-14)

**问题**:unified 的 `don't guess` 对「基于记忆的合理推断」过度保守——LoCoMo
cat3 open-domain(conv-2-q-64 未来职业推断)被误判为 factual recall 而拒答
(-6.2pp)。根因不是契约冲突,而是**缺一条显式「请求分类」路由**:factual recall
(严格 do-not-guess)与 inference/prediction/advice(grounded inference)两条路径
语义上都在,但分类责任全留给模型。

**修订**:契约加「Request classification」节(代码 `runner.go`
`unifiedAnswerContractPrompt` + spec `answer-contract.md` 冻结文本同步):
- factual recall(事实/历史/状态/偏好)须来自证据,无证据 do-not-guess;
- inference/prediction/advice(未来计划/motives/建议/意见)combine 个人证据 +
  通用知识,给合理 grounded 答案并标 likely/possible;
- **do-not-guess 只适用 factual recall,不得压制合理 grounded inference。**

**验证**:smoke probe 20 case × 3 rep × 2 臂,unified **20/20**(control 11/20)。
新增 3 边界 case 全过:future-career-from-experience(未来职业推断要答,control
0/3 → unified 3/3)、motive-inference-labeled(0/3 → 3/3)、
future-fact-unsupported(无证据未来事实仍拒答 3/3,do-not-guess 未退化)。LME
配对(修订后契约)preference 仍 +30.0pp——修订不破坏「避免有害拒答」的赢。

**配套工具修复**:smoke probe `--max-tokens` 必须 ≥8000(2000 太小 → vllm SSE
流不终止挂死,见 `qwen-vllm-16384-sse-hang` 记忆);probe judge 解析容忍
`No.` 前缀 + markdown fence(`extractProbeJudgmentJSON`)。

## 最终结论

- **LoCoMo(严格配对,1540)**:+0.6pp within-noise;cat3 open-domain -6.2pp
  (根因已定位:`don't guess` 过度保守,已由分类修订修复,待高配重跑验证)。
- **LME(严格配对,500,2026-08-14)**:**+4.4pp(p=0.000112 above-noise)**;
  preference +30.0pp;multi-session +8.3pp;knowledge-update +7.7pp;
  temporal -0.8pp / assistant 0.0pp 持平(post-hoc 的 -4.1pp 为配对伪影)。
- **方向坐实**:unified 契约「减少 unsupported + 避免有害拒答」在 preference /
  multi-session / knowledge-update 类配对验证有效,不靠 per-dataset 特调。
- **Unified 必须要有(维护者立场)**:unified 是**唯一允许的 prompt 路径**,禁止
  退回 per-dataset / category 特调——特调高分是数据集目标额,不代表系统能力。
  「Request classification」修订是契约内通用规则(非 category 路由),同时满足
  「推断要答」与「unsupported 要拒」。
- **下一步(按 [result-matrix](../result-matrix.md) 优先级)**:
  1. LoCoMo unified **top-k150 高配配对重跑**(分类修订后,验证 cat3 open-domain
     是否救回 + 整体 non-inferiority gate)。
  2. held-out 行为门(149+ 题 blinded 人工标注)作为 promotion 前置。
  3. 按 [score-solidification](../score-solidification.md) 8 步升级:当前
     LME = verified(配对 + parity + 3-rep),LoCoMo = within-noise 待高配重跑。
- **分数层级**:LME unified 已达 **verified**(严格配对 + context parity + 同批
  judge + 3-rep + above-noise);LoCoMo unified 仍 within-noise,待 top-k150
  高配重跑。
