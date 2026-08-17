# 确定性次模证据装填 (045) — 门链 Verdict (2026-08-17)

Spec: [045-submodular-packing](../../../specs/045-submodular-packing/spec.md) ·
Plan: [plan.md](../../../specs/045-submodular-packing/plan.md)

## 一句话结论

**NO-GO**。次模装填 (`--submodular-pack`) 的 e2e 配对 probe 在 **预算内显著恶化**
(−14.22pp, McNemar p<0.0001, single-hop −22.6pp),在**超预算** 2.5× 时仅 +0.65pp
(不显著, p=0.485)。045 核心命题——"token 预算锚定对照臂实际体量、同预算提质"——被
证伪。执行链上发现并修复两个实现缺陷 (T009 接线漏普通路径、模板估算低估 2.5×),修后
probe 给出的是该机制在 LoCoMo 上的真实上限。Phase 5-6 (3-rep 正批 / LME 迁移) 不执行。

## 评测配置

- 对照臂 = 现行 k30 unified 配方 (043 stepA 复刻):`--retrieval hybrid+unified --repeats 1
  --chunks --chunk-quota 12 --no-idk-retry --concurrency 32`,top-k 30 (默认)。
- 机制臂 = 同配方 + `--submodular-pack --pack-budget-anchor paired --anchor-run <ctl>`。
- store = 032-store 快照;answerer = Qwen3.6-35B-A3B-FP8 (vllm, thinking on, 32768);
  judge = deepseek-v4-flash (032-run.env);embed = bge-large `--max-num-seqs 1`。
- 数据集:LoCoMo 1,540 可答题。所有臂 1-rep (probe 协议)。

## US1 离线装填保真门 (T007)

`--aic-gate` 全量 1,986 题(含不可答)三口径 AIC:

| 口径 | AIC | tokens | 说明 |
|---|---:|---:|---|
| current (k30) | 0.1042 | 781 | 现状基线 |
| packed | 0.1546 | 778 | **+48% vs current** |
| top150 参考 | 0.2291 | 2622 | 900-pool 理想上限 |
| 门判定 | packed/top150 = 67.5% < 95% | — | **NO-GO** |

核查确认门**稳健**:error=0、日志干净、预算锚正常、空 gold 无污染、可答子集 (1,542)
比值不变 67.5%、packed 在 300 vs 900-pool 下 1,986/1,986 逐题结果相同 (预算内只选顶部
~58 条)。NO-GO 成因 = 门要求 packed 用 28% token 达到 top150 95% 绝对覆盖率
(需 3.4× 信息密度, packed 仅 2.3×), 对任何预算受限装填都不现实——**门口径过严,
非装填失效**。维护者决策: 以 e2e probe 为最终裁判。

## Probe (T010) — 三次执行, 两处实现缺陷

| probe | pack 状态 | token parity | 配对差 | McNemar | 判定 |
|---|---|---|---|---|---|
| #1 probe-pack | **未生效** (T009 接线漏普通路径) | 1.00 | −0.7pp | — (无效) | 无效, 重跑 |
| #2 probe-pack2 | 生效, **超预算 2.5×** (模板估算低估) | 2.49 | **+0.65pp** | 0.485 | 不显著, 非提质 |
| #3 probe-pack3 | 生效, **预算内** (模板修复) | 1.09 | **−14.22pp** | **<0.0001** | **NO-GO** |

### 缺陷 1 — T009 接线漏普通 answer 路径 (probe #1 根因)

`retrieveCandidates` (pack 分派) 只在 eval_runner.go 的 **formal B1 路径**
(`materializeFormalB1Question`) 被调用;普通 answer 路径 (`answerAndJudge` →
`retrieveQuestionWithDiagnostics`) 直接走 `retrieveWithQuotaDiagnostics`, **从未经过
pack 分派**。机制臂 = 对照臂重复, input_tokens 1,540/1,540 逐题一致, runDir 无 pack
audit。修复: `retrieveQuestionWithDiagnostics` 加 `questionID` 参数 + pack 分派
(multiquery.go), main.go 调用处传 `qa.QuestionID`。probe #2/#3 确认 audit 存在 +
input 逐题全不同 (pack 真实生效)。

### 缺陷 2 — packEstimateTokens 低估 2.5× (probe #2 根因)

`packEstimateTokens` = `len(runes)/4`, 只估算 content。`buildAnswerContextPrompt` 每项
有 ~34 tokens 模板开销 (event/recorded 标签、结构标记、编号)。probe #2 实测:
est content 4364 ≈ real content 4347 (rune/4 对 content 准), 但真实 prompt 10875 =
content + 模板 (34/项 × ~192 项)。预算控制只算 content → packed 装填到 2.5× 锚。
修复: `packItemTemplateTokens = 34` (校准自 probe #2) 加入 `EstTokens`。

### 机制结论 — 预算内碎片化, 同预算提质证伪

probe #3 (预算内, 1.09×) 的 packed 每项 cost 含模板后, cost 主导贪心 (gain/cost) 倾向
**短碎片项** (content 仅 ~14 tokens/项), 91 项碎片上下文缺失完整信息 → **single-hop
−22.6pp** (87.5→64.9), 全类别下行。超预算 (192 项, 2.5×) 时靠量勉强 +0.65pp 不显著。
**同一预算下次模装填不比 quota 截断好——008 铁律再次成立** (检索侧结构改动不转化)。

## 修复清单 (本地已改, 待提交)

1. `submodular_packing_cli.go`: audit `aic_top150_full` 字段补赋值 (US1 门工件缺陷,
   不影响 gate 判定)。
2. `multiquery.go` + `main.go`: T009 接线修复 (普通 answer 路径接 pack 分派)。
3. `submodular_packing.go`: 模板估算修复 (`packItemTemplateTokens = 34`)。

全量 `CGO_ENABLED=0 go build ./...` + `go test ./cmd/locomo-bench` 绿。

## Reverify ride-along (T015) — 未完成

box 上执行 `--reverify-042` (2-conv slice, conv 0/1, 304 题) **卡住无产出**:
进程存活但 `reverify.log` 0 字节、无任何 slog 输出,answerer 32 并发满载但请求不
推进,CPU 仅 9%。终止后无 ReverifyReport 工件。疑似 stream 通道 (rvStreamer SSE)
阻塞——与 042 已知的 vllm SSE 长 thinking 卡死同类(当时 16384 触发,现 32768 仍可能
在 temperature=0 logprob + stream 双通道组合下触发)。reverify 回答的是 042 的
measurement-artifact 问题,与 045 主链 NO-GO **独立**;卡住原因与修复留给 042 侧
(建议:降 concurrency 或禁用 stream 通道单独复测 logprob 通道,`rvSliceConvs` 已是
conv 0/1)。

## 后续候选 (若维护者重启)

- 次模装填在 LoCoMo 上无预算内增益, 且碎片化倾向是结构性缺陷 (cost 主导)。若要继续,
  方向是 content-aware cost (非纯模板常数) + category-conditional 权重, 或转向
  AdaGReS 式冗余去重 (纯 Go embedding 次模, 见 retrieval-budget-reduction-directions)。
- US1 门的 95% 绝对覆盖率门槛与预算受限装填不可兼得, 若重开门需改口径
  (信息密度或 vs-current 门槛)。

## 收尾

- Phase 5-6 (T011/T012) 不执行 (probe NO-GO)。
- box: 小文件备份 `/root/autodl-tmp/eval-backup-<ts>/` → vllm 按 PID 停 → `shutdown now`。
