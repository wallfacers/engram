# engram 评测结果总表(2026-07-26)

**一句话**:engram 在 LoCoMo 全量 1540 题上 **89.03%**(deepseek-v4-pro 答题,3 跑多数),
在 LongMemEval-S(cleaned)分层抽样 100 题上 **85%**;同栈对跑下 MemOS 为 **82.40%**
(engram 同栈 85.71%,**+3.31pp**;换 v4-pro judge 后 80.26% vs 83.77%,**+3.51pp**)。

> ⚠️ **读表前必读**:本表的每一个数字都是 **(数据集 × 答题模型 × 判题模型 × 配方)**
> 四元组的函数,**不是"engram 的分"**。跨行比较只在**恰好一个轴不同**时有效。
> 与他人 leaderboard 数字**一律不可直接比**——见 §5。

---

## 0. 三系统汇总

### 表 A ── 同栈实测(**唯一可以直接比较的表**)

同一台 box 的 **Qwen3.6-35B-A3B-FP8** 答题 + **bge-large-en-v1.5** 嵌入 +
**同一 judge prompt / 同一 judge 模型**。竞品跑的是**其自家代码**,零改动。

| 数据集 | n | **engram** | **MemOS** | **Mem0** | Δ(engram−MemOS) |
|---|---:|---:|---:|---:|---:|
| LoCoMo(cat 1-4) · judge=v4-flash | 1540 | **85.71%** 🔬 | **82.40%** 🔬 | — 未测 | **+3.31** |
| LoCoMo(cat 1-4) · judge=v4-pro | 1540 | **83.77%** 🔬 | **80.26%** 🔬 | — 未测 | **+3.51** |
| LongMemEval-S (cleaned) 抽样 | 100 | **80** 🔬 | — 未测 | — 未测 | — |

🔬 = 本项目实测。**engram 在唯一做过同栈对跑的对手(MemOS)上领先 3.3–3.5pp,
且该结论在两个 judge 下都成立。**

### 表 B ── 各方最好成绩(**不可直接比较**,栈不同)

| 数据集 | **engram** 🔬 | **MemOS** | **Mem0** |
|---|---:|---:|---:|
| LoCoMo | **89.03%**<br>(v4-pro 答题,3 跑多数,n=1540) | 88.83%<br>📣 自报 | **92.5**<br>📣 自报 |
| LongMemEval | **85 / 100**<br>(v4-pro 答题,S-cleaned **抽样 100**) | 89.20<br>📣 自报 | **94.4**<br>📣 自报 |

📣 = 厂商自报,**未经本项目复现**。

> ❌ **表 B 的三列不能横向相减。** 已经实测到:
> - 换答题模型(Qwen35B → v4-pro)值 **+3.3pp**(LoCoMo)/ **+5pp**(LongMemEval);
> - 换判题模型(v4-flash → v4-pro)值 **−2~−3pp**;
> - MemOS 自报 88.83,同栈复现只有 **82.40** —— **−6.43pp 全是 regime 伪影**。
>
> 也就是说,**表 B 里 6pp 以内的任何差距,都可以纯由答题/判题模型选择造出来。**
> engram 的 89.03 高于 MemOS 自报的 88.83,**这不构成任何意义上的领先**。

### Mem0 为什么只有自报数

Mem0 2026-04 blog:LoCoMo 71.4 → **92.5**,LongMemEval 67.8 → **94.4**。但同一篇明写:

> *"Scores reflect Mem0's managed platform, which includes proprietary optimizations
> not available in the open-source SDK."*

且检索预算是 **top_200**。因此:

| 障碍 | 后果 |
|---|---|
| 托管平台 + 开源 SDK 不带的私有优化 | **无法同栈复现** —— 拿不到那个栈 |
| `top_200` 检索预算 | 属"加量"杠杆,按本项目规范**即便涨分也不进默认栈** |
| 其 `memory-benchmarks` 仓是空的 git submodule | 无法读源码核对口径 |

**结论:对 Mem0 的真实差距 = 未知数。** MemOS 那套"剥离 regime 伪影"的手法
**尚未对 Mem0 做过**,不能把 MemOS 的结论(自报分含 6.43pp 红利)套到 Mem0 头上——
也同样不能假定 Mem0 的 92.5 是干净的。**在做同栈复现之前,不宣称任何相对位置。**

---

## 1. 主表:数据集 × 答题模型

判题固定 `deepseek-v4-flash` + mem0-aligned prompt;检索固定 canonical recipe
(`--chunks --top-k 30 --chunk-quota 12 --force-answer`,retrieval hybrid);
嵌入固定 `BAAI/bge-large-en-v1.5` 1024d(本地)。

| 数据集 | n | 答题模型 | 聚合 | **总分** |
|---|---:|---|---|---:|
| LoCoMo | 1540 | Qwen3.6-35B-A3B-FP8 | 3 跑多数 | 85.71% |
| LoCoMo | 1540 | Qwen3.6-35B-A3B-FP8 | 3 跑均值 | 85.11% (ci95 84.38–85.84) |
| **LoCoMo** | **1540** | **deepseek-v4-pro** | **3 跑多数** | **89.03%** |
| LoCoMo | 1540 | deepseek-v4-pro | 3 跑 per-rep | 88.70 / 88.64 / 88.70 |
| LongMemEval-S (cleaned) | 100 | Qwen3.6-35B-A3B-FP8 | 3 跑多数 | 80% |
| **LongMemEval-S (cleaned)** | **100** | **deepseek-v4-pro** | **3 跑多数** | **85%** |

**答题模型轴的净效应**(同数据集、同检索、同判题):
LoCoMo **+3.32pp**(85.71 → 89.03,同为 3 跑多数);LongMemEval **+5pp**(80 → 85)。

> **n 的作用**:LoCoMo(n=1540)三跑落在 **88.64–88.70**,带宽 **0.06pp**;
> LongMemEval(n=100)三跑落在 **83–86**,带宽 **3 分**。同一套代码、同一个答题模型,
> 仅因样本量差 15 倍,可分辨的最小效应差了近两个数量级。**n=100 的任何 <5 分结论都不可信**(见 §4)。

### 1.1 LoCoMo 分类别

均为 3 跑多数投票,judge = deepseek-v4-flash。

| 类别 | n | **v4-pro** | Qwen3.6-35B | Δ |
|---|---:|---:|---:|---:|
| single-hop | 841 | 90.96% | 88.82% | +2.14 |
| multi-hop | 282 | 88.65% | 87.59% | +1.06 |
| **temporal** | 321 | **89.41%** | 81.93% | **+7.48** |
| **open-domain** | 96 | **71.88%** | 65.62% | **+6.26** |
| **OVERALL** | **1540** | **89.03%** | **85.71%** | **+3.32** |

答题模型强度的收益**极不均匀**:时序 +7.5pp、开放域 +6.3pp,而 multi-hop 只有 +1.1pp。
即"记忆层已经把证据取到了,弱答题模型在需要推理与取舍的题上兑现不出来";
反过来,**multi-hop 几乎不受答题模型影响 —— 那一类的瓶颈确实在检索侧**。

### 1.2 LongMemEval 分题型

| 题型 | n | Qwen undated | Qwen **dated** | v4-pro undated | v4-pro **dated** |
|---|---:|---:|---:|---:|---:|
| knowledge-update | 15 | 12 | 13 | 13 | 13 |
| multi-session | 27 | 19 | 19 | 23 | 22 |
| single-session-assistant | 11 | 10 | 10 | 10 | 10 |
| single-session-preference | 6 | 2 | 1 | 5 | 4 |
| single-session-user | 14 | 14 | 14 | 13 | 13 |
| **temporal-reasoning** | **27** | **18** | **23** | **18** | **23** |
| **总分** | **100** | **75** | **80** | **82** | **85** |
| per-rep | | 70/75/75 | 79/72/82 | 82/80/77 | 86/86/83 |

---

## 2. CURRENT DATE 锚点(`bb99d58`):跨答题模型的独立复现

`temporal-reasoning` 在**两个强弱悬殊的答题模型下**表现完全一致:

| 答题模型 | undated | dated | Δ |
|---|---:|---:|---:|
| Qwen3.6-35B-A3B-FP8 | **18/27** (66.7%) | **23/27** (85.2%) | **+18.5pp** |
| deepseek-v4-pro | **18/27** (66.7%) | **23/27** (85.2%) | **+18.5pp** |

**逐点相同。** 这正是"信息可得性缺口"而非"模型能力缺口"该有的形状:补上同一条
缺失信息,两个模型拿到同样的收益。诊断与验尸见
[`specs/016-longmemeval-crossbench/verdict.md`](../specs/016-longmemeval-crossbench/verdict.md) 后记。

**LoCoMo 不受影响**:LoCoMo 无 `question_date` → 锚点不注入、规则不追加、prompt
字节级不变(`TestAnswerPromptWithoutCurrentDateIsUnchanged` 断言)。

---

## 3. 判题模型轴

对**同一批答案重判**,答案一行不动 —— judge 是唯一变量。judge system prompt 从
`cmd/locomo-bench/runner.go` 的 `judgeMem0AlignedSystemPrompt` 正则抽出,保证与
bench 内跑的判分逐字一致。

**LongMemEval-S 100 题,deepseek-v4-pro 答题,3 跑多数:**

| judge | undated | dated | **dated 增益** |
|---|---:|---:|---:|
| deepseek-v4-flash | 82 | 85 | **+3** |
| deepseek-**v4-pro** | 79 | 83 | **+4** |
| **judge 轴净效应** | **−3** | **−2** | |

**LoCoMo 全量 1540,Qwen3.6-35B 答题,3 跑多数:**

| judge | engram | MemOS@同栈 | **同栈差距** |
|---|---:|---:|---:|
| deepseek-v4-flash | 85.71% | 82.40% | **+3.31** |
| deepseek-**v4-pro** | 83.77% | 80.26% | **+3.51** |
| **judge 轴净效应** | **−1.94** | **−2.14** | **+0.20** |

`temporal-reasoning` 的 dated 增益在两个 judge 下**都是 +18.5pp**
(flash 66.7→85.2;pro 63.0→81.5)。

**结论:换 judge 把绝对水位整体压低 2–3 分,但每一个 Δ 原样保留。**
engram 与 MemOS 的同栈差距在两个 judge 下分别是 **+3.31pp / +3.51pp** —— 差 0.2pp;
CURRENT DATE 的 temporal 增益在两个 judge 下都是 **+18.5pp**。
即"判题宽松度"是一个**加性偏移**,不改变任何机制结论的方向或量级。这也说明
**跨系统比较必须锁定 judge**,否则 3 分的差距可以纯由 judge 造出来。

---

## 4. 消融实验(LongMemEval-S 100,Qwen 答题,dated,各 3 跑多数)

**先看噪声地板**——`ref` 臂与 `base` 臂**配置完全相同**,只是重跑一遍:

| 臂 | per-rep | 多数 | vs base | vs ref |
|---|---|---:|---:|---:|
| `base`(dated, quota 12) | 79 / 72 / 82 | **80** | — | −2 |
| **`ref`(同配置重跑)** | 77 / 84 / 75 | **82** | **+2** | — |
| `--assoc`(实体图第 4 路) | 78 / 83 / 85 | **84** | +4 | +2 |
| `--temporal-score`(时间窗打分) | 77 / 78 / 73 | **78** | −2 | −4 |
| `--chunk-quota 0`(不预留 chunk 位) | 80 / 78 / 78 | **80** | 0 | −2 |

**同配置重跑差 2 分。per-rep 带宽 9–10 分。** 因此:

- `--assoc` **+4 / +2 —— 落在噪声内,判不了**。不是"没效果",是这个规模(n=100)
  测不出。要判它需要 n≥500 或 8+ reps。
- `--temporal-score` **−2 / −4 —— 方向为负,量级近噪声地板**。不足以判死,
  但没有任何证据支持开启;维持默认关。
- `--chunk-quota 0` **0 / −2 —— 净零**。此前免费覆盖门测得它 +3.8pp 覆盖率,
  端到端不转化 —— 008 铁律(覆盖率增益 ≠ 答题增益)第 N 次成立。

> **方法学**:没有 `ref` 自比照臂,这张表会被读成"assoc +4 有效、temporal −2 有害"。
> 加上 `ref` 才看清**两条结论都在噪声里**。任何单臂对单臂的消融,若没有同配置
> 重跑作为噪声标尺,结论不可信。

### 4.1 检索侧此前已判死的杠杆(免费覆盖门,零 LLM 调用)

| 杠杆 | 证据 | 判决 |
|---|---|---|
| `--assoc` @ `--chunk-quota 0` | 严格覆盖率 0.8866 → 0.6825(**−20.4pp**) | ❌ |
| 时间窗召回臂 | `ParseTemporalIntent` 仅在 29.6% 的时序 query 上触发 | ❌ |
| temporal 检索改进 | gold 中位 rank **3**、`oracle_lift@30` = **0.000** | ❌ 零可得空间 |

---

## 5. MemOS 对比

### 5.1 同栈对跑(唯一有效的比较)

固定同一台 box 的 **Qwen3.6-35B-A3B-FP8** 答题 + **bge-large** 嵌入 +
**同一 judge prompt / 同一 judge 模型**(deepseek-v4-flash),LoCoMo 全量 1540:

| 类别 | n | **MemOS@同栈** | **engram**(3 跑多数) | Δ(engram − MemOS) |
|---|---:|---:|---:|---:|
| single-hop | 841 | 82.64% | 88.82% | **+6.18** |
| multi-hop | 282 | **89.36%** | 87.59% | **−1.77** |
| temporal | 321 | 82.55% | 81.93% | −0.62 |
| open-domain | 96 | 59.38% | 65.62% | **+6.24** |
| **OVERALL** | **1540** | **82.40%** | **85.71%** | **+3.31** |

MemOS 的 tree/graph 记忆组织**只在 multi-hop 赢 1.77pp**——正是它该赢的地方;
single-hop / open-domain 各输 6pp+,temporal 打平。

### 5.2 leaderboard 数字 vs 同栈数字

| 口径 | MemOS | engram | Δ |
|---|---:|---:|---:|
| **自报栈**(各自的答题模型 + 各自的 judge) | 88.83% | — | 不可比 |
| **同栈** Qwen35B 答题 + **v4-flash** judge | **82.40%** | **85.71%** | **+3.31** |
| **同栈** Qwen35B 答题 + **v4-pro** judge | **80.26%** | **83.77%** | **+3.51** |
| engram 强答题模型(v4-pro 答题 + v4-flash judge) | — | **89.03%** | 不可比 |

**MemOS 88.83 → 82.40 的 −6.43pp 全部是 regime 伪影**(答题模型强度 + judge 宽松度)。
即 leaderboard 上"纯本地栈能上 88+"那个数字,换到 engram 的 answerer + judge 就不存在。

> ❌ **不得把 engram 的 89.03 与 MemOS 的 88.83 并排宣称"打平"或"领先"** —— 两者
> 答题模型和 judge 都不同,是两个不同四元组的数,并排毫无意义。**唯一有效的对比是
> 同栈行:+3.31pp(v4-flash judge)/ +3.51pp(v4-pro judge)。** 两个 judge 下差距
> 只差 0.2pp,说明该结论**不是 judge 挑出来的**。

### 5.3 同栈对比的诚实项(引用 §5.1 必须连同这几条)

1. **聚合不对称**:engram = 3 次答题 rep 多数;MemOS = **1 次答题** × 3 次判题 rep 多数,
   **单点无带宽**。82.40 落在 engram ci95 下界外 2.5pp ⇒ 方向稳,量级 ±1pp 待补 reps。
2. **上下文预算不等,且对 engram 有利**:MemOS 每题喂 ~1059 tok,engram ~3262 tok
   (**约 3 倍**)。两边都是各自默认值,但 **+3.31pp 里有多少来自"上下文更多"而非
   "记忆更好",本轮未剥离**。这是本结论最大的残留混淆项。
3. **其余不对称多数对 MemOS 有利**:MemOS 默认带本地 `bge-reranker-v2-m3`
   (engram 基线**无** reranker)、用了检索更完整的 `fine` 模式 → 在这两轴上
   "engram 领先"是**保守**的。
4. **未做配对检验**:只有两个独立总分,**没有 1540 题的 McNemar**。
5. **judge 轴已排除**:同栈差距在 v4-flash 下 +3.31pp、v4-pro 下 +3.51pp(差 0.20pp),
   即该差距**不依赖 judge 选择**——这是本轮新增的加固(重判 6163 次,答案一行未动)。

### 5.4 Mem0 呢?

Mem0 2026-04 blog 自报 LoCoMo **92.5** / LongMemEval **94.4**,但明写
**"Scores reflect Mem0's managed platform, which includes proprietary optimizations
not available in the open-source SDK"**,且用 **top_200** 检索预算。

- 托管平台 + 专有优化 ⇒ **不是可复现的开源栈**,无法同栈对跑。
- `top_200` 属"加量"(撑 top-k / 扩池),按本项目杠杆哲学**即便涨分也不进默认栈**。

**故 Mem0 的数字不进本表。** 未做同栈复现 = 不宣称任何相对位置。

---

## 6. 用量(全部按 usage 插桩,非牌价推算)

| 批次 | 付费调用 | 花费 |
|---|---|---:|
| LongMemEval v4-pro undated ×3 + dated ×3 | answer 906 / judge 900 | ¥6.0–12.1 |
| LongMemEval Qwen(dated ×3 + 消融 ×12) | judge 1500(答题免费,box) | ~¥1.5 |
| 判题模型轴(重判 ×2 组) | judge 602(v4-pro) | ~¥1.5 |
| LoCoMo v4-pro ×3(全量 1540) | answer ~4600 / judge ~4600 | ¥39 |
| 判题轴重判 LoCoMo(engram + MemOS) | judge 6163(v4-pro) | ~¥10 |
| MemOS 同栈(既有) | judge 4620 | ¥1.70 |

`extract` / `embed` 全程零付费(store 复用 + 本地 bge sidecar)。
答题侧 Qwen 跑在租用 GPU 上,不计 token 费。

---

## 7. 口径与边界(引用本表必须一并给出)

- **LongMemEval 是 `longmemeval_s_cleaned.json` 的分层抽样 100 题**,
  **不是全量 500**,**不得简称"LongMemEval"**。分层配额:multi-session 27 /
  temporal-reasoning 27 / knowledge-update 15 / single-session-user 14 /
  single-session-assistant 11 / single-session-preference 6。
  `single-session-preference` **n=6,任何该行的百分比都不可判**。
- **LoCoMo 两臂均为 3 跑多数**,可直接比。per-rep 带宽:v4-pro 88.64–88.70,
  Qwen 3 跑均值 85.11%(ci95 84.38–85.84)。
- **MemOS 仍是 1 次答题 rep**(×3 判题 rep),单点无带宽 —— 见 §5.3①。
- **LongMemEval 是独立新基线**,不替代、不混入 LoCoMo 的 85.71%。
- **判题模型换一下就值 2–3 分**(§3)。任何跨系统比较若未锁定 judge,差距在 3pp
  以内的结论一律无效。
- **消融的噪声地板是 ±2–4 分**(§4)。n=100 上小于 5 分的效应测不出来。
- engram 侧本轮**引擎零改动**:唯一的代码改动 `bb99d58` 在 `cmd/locomo-bench/`
  (评测口径修正,按宪法 IV 与算法改动分离提交),
  `git diff --name-only -- memory embedding provider store internal` 为空。

## 8. 产物

✅ **已归档 HF**(2026-07-26):私有集 `wallfacers/engram-locomo-artifacts` 下
[`matrix-2026-07-26/`](https://huggingface.co/datasets/wallfacers/engram-locomo-artifacts/tree/main/matrix-2026-07-26)
—— 85 文件(逐题 results / stats / cost / regime + 重判结果 + 驱动脚本 + MANIFEST),
已回读仓库列表核验,已核无凭据泄漏。store 目录(1.8G)不归档,可由脚本 + 数据集重建。


- LoCoMo:`.locomo-run/matrix/locomo-pro-r*/`(gitignored)、
  基线 `009-full-A-base` 在 HF `wallfacers/engram-locomo-artifacts`
- LongMemEval:`.longmemeval-run/p3-s100/{ans*,pro-A-r*,dated-C-r*,qwen-dated-r*,abl-*}/`
- MemOS:HF `wallfacers/engram-locomo-artifacts/memos-parity/`,方法学正本
  [`docs/memos-inhouse-locomo-repro.md`](memos-inhouse-locomo-repro.md)
- 时序验尸与 CURRENT DATE 修复台账:
  [`specs/016-longmemeval-crossbench/verdict.md`](../specs/016-longmemeval-crossbench/verdict.md)
