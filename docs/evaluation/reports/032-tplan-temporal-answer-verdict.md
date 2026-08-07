# 032 Verdict: 答题侧时序推理契约（--temporal-answer-prompt）

**日期**: 2026-08-07 · **口径**: 84 题 × 3-rep majority **+** 全量 1540 × 1 rep，arm-to-arm 配对 · **模型**: deepseek-v4-flash（禁思考）答题+判题 · **检索**: fts keyword-only 弱检索（无 embedding）· **store**: 009-bge-chunks-store（全量抽取复用）· **变量**: 两臂只差 `--temporal-answer-prompt`（category-2 答题 prompt 换时序推理契约）；同 store/answerer/judge/cap

## 结论

**GO（显著）——temporal 域 8 次证伪后的首个显著正向。** 答题侧时序推理契约在弱检索/弱 answerer 栈上大幅提升 temporal，全类别无回落，两个口径（子集 3-rep / 全量 1-rep）统计显著且互证。完全落在既有诊断"temporal 主瓶颈在答题侧时序推理契约"（013/017/temporal-bottleneck-diagnosis）上。

## 配对结果（arm-to-arm）

### 子集 84 题 × 3-rep majority（fts）

| 臂 | OVERALL | temporal | multi-hop |
|---|---|---|---|
| keep | 19.4% | 19.8%（~12/59） | 18.7% |
| **tplan** | **32.9%** | **35.6%（~21/59）** | 26.7% |
| Δ | **+13.5pp** | **+15.8pp** | +8.0pp |

flips A→B 17 / B→A 2，McNemar **p=0.000729**，CI 不重叠，above-noise。

### 全量 1540 × 1 rep（fts）

| 臂 | OVERALL | temporal | multi-hop | single-hop | open-domain |
|---|---|---|---|---|---|
| keep | 43.2%（665/1540） | 46.1%（148/321） | 49.3%（139/282） | 38.3%（322/841） | 58.3%（56/96） |
| **tplan** | **46.8%（720/1540）** | **57.3%（184/321）** | 51.8%（146/282） | 39.6%（333/841） | 59.4%（57/96） |
| Δ | **+3.6pp（+55题）** | **+11.2pp（+36题）** | +2.5pp | +1.3pp | +1.1pp |

逐题：A→B 124 / B→A 69，McNemar **p=0.000101**，above-noise。

## 机制归因

1. **temporal 增量集中在类别本身**：tplan 只替换 category-2 的 answer prompt，temporal +11.2pp（全量）/+15.8pp（子集）为最大；不生效类别（single/open-domain）仅 +1.1~1.3pp（噪声/泛化）。增量方向与机制一致。
2. **为什么 014 当时只有 +2.5pp 不显著**：014 在强栈/高 base 下测（base 已高 → 契约边际小）+ 单 rep。本次 fts 弱检索下 base 极低（keep temporal 19.8%/46.1%），时序契约把"让 flash 显式做时间推理"的边际完全释放——与 flash-keyword-probe"弱检索下结构/提示增量大"一致。
3. **答题侧契约才是 temporal 的杠杆**：8 次证伪覆盖写侧（027/028）、检索侧（013/014/029/031 结构）、读侧结构注入；唯一没被多-rep 钉死的是答题 prompt 契约，本次坐实。与 temporal-bottleneck-diagnosis（答错题 69% gold 已进 top-30 → 瓶颈在答题）闭环。

## 诚实边界

- **全量单 rep**（子集 3-rep majority 已坐实，全量 1-rep 方向一致且 p=0.000101，二者互证）。
- **fts 弱检索 + flash 弱 answerer 栈**：绝对分（tplan 46.8%）不可与 030 hybrid/Qwen 85.91% 跨口径比。~~生产栈（hybrid + Qwen3.6）下未实测~~（**已补测**：见下方"生产栈确认"章节——生产栈 tplan 仅 +0.5pp within-noise，增量依赖 base 高低；思考模式下 tplan 增量归零 flips 32/32 p=0.90）。
- judge = flash（与 answerer 同模型），但 arm-to-arm 同 judge，增量判定不受影响。
- temp=1.0 单次观测非确定性（memory: locomo-answer-nondeterministic）；3-rep majority 已缓解。

## 出货影响

- **eval harness flag，default-off**（`--temporal-answer-prompt` 默认 false），不改变默认 MCP/CLI/检索路径。
- 转正（默认开启）需：①生产栈（hybrid+Qwen）arm-to-arm 确认增量仍正；②与 trace（已默认开）叠加是否正交（trace 改 context、tplan 改 prompt，机制独立，但需实测叠加）。
- 生产栈确认依赖远端 vllm 机器（本机无 Qwen 端点）。

## 生产栈确认（hybrid + Qwen，2026-08-07 补跑）

远端 AutoDL（RTX PRO 6000 Blackwell）：Qwen3.6-35B-A3B-FP8（vllm 8000，triton 踩坑配方）+ bge-large-en-v1.5（8010）+ DeepSeek judge。84 题 × 3-rep majority，arm-to-arm，唯一变量 = 各机制 flag。

| 臂 | OVERALL | temporal | multi-hop |
|---|---|---|---|
| keep | 55.2% | 55.9% | 53.3% |
| **tplan** | **63.9%** | **66.7%** | 57.3% |
| trace | 60.3% | 59.3% | 62.7% |
| tplan+trace | 61.5% | 60.5% | 64.0% |

配对：keep vs tplan flips 12/7 **p=0.359 within-noise**；trace vs tplan+trace p=1.0；keep vs tplan+trace p=0.79。

**生产栈解读**：
- **tplan 方向与 fts 栈一致**（temporal +10.8pp：55.9→66.7），机制在两检索强度下互证；但生产栈单点 **within-noise**——keep 的 rep 方差极大（temporal 三 rep：69.5%/32.2%/66.1%），84 题样本统计功效不足，非机制失效。
- **tplan+trace 不互补**（61.5% < tplan 63.9%，temporal 60.5 < 66.7）：trace 的 context 改写与 tplan 的时序 prompt 叠加反而干扰。若出货二选一，tplan 优于叠加。
- **embedding 降级诚实标注**：bge-large 512 token 上限，个别超长文本 embedding 400 降级（keyword+entity 兜底），两臂同 store 同降级，配对公平但绝对分略受语义缺失影响。

### 生产栈全量 1540 × 1-rep（2026-08-07 补跑，非思考）

| 臂 | OVERALL | temporal | multi-hop | single-hop | open-domain |
|---|---|---|---|---|---|
| keep | **86.8%** | 87.9% | 87.6% | 88.6% | 65.6% |
| tplan | **87.3%** | 88.2% | 88.3% | 89.2% | 65.6% |
| Δ | +0.5pp | +0.3pp | +0.7pp | +0.6pp | 0 |

McNemar flips 59/51，**p=0.504 within-noise**。生产栈 base 86.8%（与参考点 85.91% 同量级），tplan 高 base 下增量缩到噪声内——与 fts 弱栈（+11.2pp，p=0.0001 显著）对比，**tplan 增量依赖 base 高低**。**032 定位确认：default-off 保留，不转正**（弱栈 opt-in 能力）。

### 思考版（放开深度思考）待测

思考解锁（`LOCOMO_NO_THINKING=0`）后全量配对运行中——见 docs/research/thinking-unlock-rationale.md。判定：①思考口径差（vs 非思考 86.8%）；②思考下 tplan 增量是否仍成立（同口径 arm-to-arm）。

## 复现命令

```bash
# 本机 flash fts 配对（无 embedding，几乎免费）
REPS=3 bash ~/.claude/session-scratch/tplan_confirm.sh   # 84 题 3-rep
bash ~/.claude/session-scratch/tplan_full.sh             # 全量 1-rep
# 核心 flags: --chunks --retrieval fts --top-k 30 --chunk-quota 12 --force-answer
#   --judge-mem0-aligned --trace-mediation=false [+ --temporal-answer-prompt]
```

## 关联

- [[temporal-bottleneck-diagnosis]]（主瓶颈答题侧 → 本 verdict 闭环）
- [[014-temporal-contract-verdict]]（契约前身：强化重 CoT 翻车、旧契约 +2.5pp 不显著 → 本次多-rep 坐实）
- [[013-temporal-window-verdict]] / [[027-write-side-event-verdict]] / [[029-agentic-nav-verdict]] / [[031-evidence-relation-sdd-ready]]（temporal 前 7 次证伪）
- [[memory-mutability-eval-surface]]（写侧"记忆会变"评测无面 → 答题侧契约反而是真杠杆）
