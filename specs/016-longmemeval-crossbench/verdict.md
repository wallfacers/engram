# 016 判决台账

## G-尺子门（T021）

**v1 判定（≥0.95）**：**未达标 0.901 < 0.95**，已按规格停在 P1（commit `af753c5`）。
**v2 判定（重新登记后）**：**通过** —— 见文末「v2 判定」。

**日期**: 2026-07-26 · **状态**: v2 通过，进入 P2。判据修正的完整留痕见
[criterion-registered.md](./criterion-registered.md) 顶部的修正记录；最终判决判据
（条件增益那套）**未改动**，SHA256 已复核一致。

### 实测

smoke 集：30 题 / 6 题型全覆盖 / 24 有证据 + 6 无证据 / 712 条消息
（T019 覆盖抽取，两次运行 sha256 相同）。

配方：`--chunks --chunk-quota 12 --top-k 30 --coverage-only --retrieval hybrid`，
embedding = 本地 bge-large-en-v1.5（1024d），抽取 = 隧道内 vllm。
**答题模型调用 0、判分模型调用 0**（`--coverage-only` 结构性保证）。

| 题型 | 精确轮次覆盖 | n |
|---|---:|---:|
| knowledge-update | **1.000** | 4 |
| single-session-assistant | **1.000** | 4 |
| single-session-user | **1.000** | 4 |
| single-session-preference | 0.917 | 4 |
| temporal-reasoning | 0.750 | 4 |
| multi-session | 0.742 | 4 |
| **OVERALL** | **0.901** | **24** |

6 道无证据题按 FR-007 判为 `gradeable=false`，正确排除在 n 之外（G4 通路已验证）。
`single-session-preference` 出现即证明 G1 通路已验证。

### 门失败的原因**不是**门所声称的原因

规格给本门的理由是「覆盖率不达标 ⇒ 读取或计量有误（oracle 按构造零干扰项，
覆盖率理应接近满分）」。四项独立诊断否定了这个解释：

| 诊断 | 结果 | 排除了什么 |
|---|---|---|
| 建库侧天花板：每条黄金轮次是否落在某个 chunk 的血缘里 | **1.000，6 型全满，0 题低于 1.0** | 排除 loader / DiaID 合成 / chunk 血缘出错 |
| 全量读取时 `evidenceUnmatched`（oracle + s_cleaned 各 500 题） | **0** | 排除黄金证据指向不存在的 turn |
| 同一 store、去掉 chunk 配额（`--chunk-quota 0 --top-k 300`） | **OVERALL 1.000** | 排除检索取不到黄金轮次 |
| 向量完整性（反连接，非计数比较） | `missing=0`（另有 298 个孤儿向量） | 排除 Backfill 有界队列静默丢弃 |

**结论：尺子本身正确。0.901 是 canonical 配方在 12-chunk 预算下的真实覆盖上限。**

### 附带发现：`--chunk-quota` 在 coverage 模式下被静默钳到 12

`coverage.go:262` 以 `traceSelection=true` 调用 `selectorForArm`，因此
`pcic.go:51` 的短路条件不成立、**总是**返回选择器；无 `--pcic`/`--oracle` 时落到
`default: pcicSelect(...)`，而 `pcic.go:252-253` 将 budget 硬钳为 12。

实证：`--chunk-quota 12 --top-k 30` 与 `--chunk-quota 40 --top-k 100` 给出
**逐位相同**的 0.901；`--chunk-quota 0`（绕开配额路径）才给出 1.000。

即：在 coverage-only 下，任何大于 12 的 `--chunk-quota` 都无效果且无提示。
这不是 016 引入的，是既有行为，记录在此供后续裁定。

### 为什么 LongMemEval 比 LoCoMo 更吃预算

缺口全部集中在 `multi-session` 与 `temporal-reasoning` —— 证据跨会话最多、
黄金轮次最分散的两型。LongMemEval-oracle 单题 3 个会话、每会话 12–25 条消息，
黄金轮次可散落在多个 chunk 中；12 个 chunk 的预算装不下。LoCoMo 的会话更短，
同样预算下不吃紧。这是**benchmark 结构差异**，不是实现缺陷。

### 裁定结果

执行者先按 v1 字面判定停止并归档（`af753c5`），未自行放宽。维护者裁定重新登记
修正后的门（v2，[criterion-gate-v2.txt](./criterion-gate-v2.txt)）。

### v2 判定

| v2 判据 | 实测 | 结论 |
|---|---|---|
| 建库侧天花板 == 1.000 | **1.000**（6 型全满，0 题低于 1.0） | 通过 |
| 无预算约束覆盖 == 1.000（`--chunk-quota 0 --top-k 300`） | **1.000**（6 型全满） | 通过 |
| 答题调用 == 0 且 判分调用 == 0 | 0 / 0（`--coverage-only` 结构性保证） | 通过 |

**G-尺子门 v2：通过。** canonical 配方下的 0.901 与其分题型分解按 v2 要求作为
**发现**记录（见上表），不作为判据。

## US2 · ORACLE 臂（全 500 题，零干扰项）

**日期**: 2026-07-26 · **数据集**: `longmemeval_oracle.json`（500 题，官方 oracle 版）

配方：`--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned
--retrieval hybrid`。答题/抽取 = 租用 GPU 上的 vllm（`Qwen/Qwen3.6-35B-A3B-FP8`，
关思考链）；embedding = 本地 `bge-large-en-v1.5`（1024d）；判分 = `deepseek-v4-flash`
（与既有 LoCoMo 数字同 judge regime，刻意不引入第二个变量）。

**G-向量门（T024）通过**：500 库 / 23,402 条目 / `missing=0`（另有 4,309 孤儿向量）。

### 结果

| 题型 | 正确率 | n | 精确证据覆盖率 |
|---|---:|---:|---:|
| single-session-user | 98.6% | 70 | 1.000 |
| single-session-assistant | 92.9% | 56 | 1.000 |
| knowledge-update | 80.8% | 78 | 0.931 |
| multi-session | 70.7% | 133 | 0.918 |
| temporal-reasoning | 64.7% | 133 | 0.920 |
| single-session-preference | **60.0%** | 30 | 0.989 |
| **OVERALL** | **76.4%**（382/500） | 500 | 0.945（n=479） |

覆盖率的 n=479 = 500 − 21 道无证据题，与 FR-007 的排除规则精确吻合。
正确率的分母是 500（无证据题仍有标准答案，答题侧照常评分）。

### 实测用量（按 usage 插桩记录，不按牌价推算）

| 角色 | 调用 | in tokens | out tokens |
|---|---:|---:|---:|
| answer | 501 | 1,533,080 | 3,208 |
| judge | 500 | 55,546 | 65,712 |
| extract | 23 | 52,663 | 22,301 |
| embed | 953 | — | — |

answer/extract/embed 全部零付费（本地 + 租用 GPU）；只有 judge 走付费口。
答题上下文均值 3,060 token。

### 一处值得记的形状

`single-session-preference` 覆盖率 0.989、正确率却只有 60.0% —— **证据几乎全在手里，
仍然答不对**。这是纯答题侧的失败形状。而这一型此前被 loader 直接硬报错拒收
（G1），本次是它第一次产生任何数字。

## 逐题覆盖的载体（T033）与一处自我更正

T033 要求「对 S 臂运行只读归因诊断」拿到**逐题**证据覆盖率，但
`--coverage-only` 只输出聚合值（`coverage.json` 无逐题记录），而
`--attribution-trace` 被 `validateAttributionOptions` 硬拒 `locomo` 以外的
dataset-format。该闸是 LongMemEval 尚无 turn id 时加的保守限制；016 已为其合成
`turn.DiaID = "D<session>:<turn>"`，`buildAttributionTrace` / `parsedGoldTurns` /
`hitMappedGoldTurns` 全链路本就与数据集无关。故放开该闸（测试先行，commit
`e6b6755`），改动仅 `cmd/locomo-bench/attribution.go`，引擎零改动，LoCoMo
`--estimate` 锚点不变。

### 交叉验证：归因 trace 复现了已登记的门禁数字

在 P1 的 30 题 smoke store 上跑归因，按 `chunk-c` 前缀过滤后逐题算严格覆盖率：

| 题型 | 归因 trace（严格） | T021 `coverage.json` |
|---|---:|---:|
| knowledge-update | 1.0000 | 1.000 |
| single-session-assistant | 1.0000 | 1.000 |
| single-session-user | 1.0000 | 1.000 |
| single-session-preference | 0.9167 | 0.917 |
| temporal-reasoning | 0.7500 | 0.750 |
| multi-session | 0.7417 | 0.742 |
| **OVERALL** | **0.9014** | **0.901** |

gradeable 24 / 无证据 6，与 T021 完全一致。**两条独立代码路径给出逐位相同的
结果**，说明 T034 分桶所用的逐题覆盖率就是 0.901 那个数字的逐题分解，而非另一
把尺子。

同一 trace 用宽松尺子（含 fact 模糊配对）为 **0.9583** —— 比严格尺子高 5.7pp。
LoCoMo 上的同一对照是 0.808 / 0.841。两个 benchmark 上宽松尺子都系统性偏高，
印证 T033 裁定「必须过滤到 `chunk-c`」的必要性。

### 更正

`e6b6755` 的提交信息里有一句说过头了：它称 0.901 来自「答题臂从未走过的路径」。
实际情况是——答题路径（`main.go:1293`，`traceSelection=false` 且无
`--pcic`/`--oracle`）与归因路径（`attribution.go:401`）**都**传 nil 选择器；
`--coverage-only`（`coverage.go:257`，`traceSelection=true`）确实恒得一个
`pcicSelect` 选择器，但在无 DemandAtoms/Meta 时它退化为「按融合序取前 budget
个」，与纯 quota 截断等价，故三者数值一致（上表即证据）。**「路径不同」成立，
「数值因此不可比」不成立。** 判决据此仍用归因 trace，理由改为「它是唯一能给出
逐题分解的载体」，而不是「coverage.json 的数字不可信」。

## US3 · S 臂

**进行中** —— 分层抽样 100 题（配额 27/27/15/14/11/6，seed 20260726，
两次运行 sha256 一致），49,556 条消息，4,789 次抽取。

## Phase 6 前置核验（在 S 臂建库期间完成）

| 任务 | 命令 | 结果 |
|---|---|---|
| T038 引擎零改动 | `git diff --name-only -- memory embedding provider store internal` | **空**（工作树与近 6 次提交均是） |
| T039 全量回归 | `CGO_ENABLED=0 go build ./...` / `go test -count=1 ./...` | build exit 0；**14 包全绿，0 FAIL** |
| T040 LoCoMo 零行为变更 | `--data testdata/locomo/locomo10.json --estimate` | `questions=1540 extract_calls=288`，**与 T001 锚点逐字相同** |
| T036 判据完整性 | `sha256sum criterion.txt` | `2142f722…09ba`，**与登记值一致** |

T040 是宪法 IV 的核心证据：016 只动 loader 的 LongMemEval 分支与归因门，LoCoMo
路径的题目数与抽取调用数一字未变 ⇒ LoCoMo 85.71% 基线 invariant by
construction，无需重跑。

## US3 · S 臂结果（LongMemEval-**S (cleaned)** 分层子集 **100 题**，非全量）

**日期**: 2026-07-26 · **数据集**: `longmemeval_s_cleaned.json` 的分层抽样
100 题（配额 multi-session 27 / temporal-reasoning 27 / knowledge-update 15 /
single-session-user 14 / single-session-assistant 11 /
single-session-preference 6，seed 20260726，两次运行 question_id 集合 sha256
一致），49,556 条消息，4,789 次抽取。配方与 ORACLE 臂逐字相同。

### G-向量门（T031）：一次真实的判死，以及修复

首次判定 **FAIL**：102,578 条目中 **20,503 条无向量**（反连接，非计数比较）。
这正是本门被设计出来要抓的故障：`memory/embedder.go:15`
`DefaultEmbedBuffer = 256`，而每库一次 `Backfill` 要入队 345–525 个名字，
溢出部分被**静默丢弃**，检索随之静默降级。

修复不改引擎：`Backfill` 以 `NamesMissingModel` 为源且幂等，"names beyond the
queue capacity are dropped and picked up on the next Backfill"。降并发重复过库
（`--coverage-only`，零答题/零判分调用；条目已存在故抽取跳过）即收敛：

| 轮次 | missing |
|---|---:|
| 建库结束 | 20,503 |
| 第 1 轮后 | 236 |
| 第 2 轮后 | **0** |

**门通过。** 代价是一处必须记录的评测陷阱：修复前后同一 store 的严格覆盖率从
**0.425 → 0.849**。若不设这道门，S 臂会带着 20% 无向量的语料出分，且**所有下游
数字都会偏低而无人察觉** —— 分桶会把大量题错误地推进零覆盖桶，从而把条件增益
推高、把"检索侧是瓶颈"这个结论伪造出来。

### 逐题严格覆盖率（T033，`chunk-c` 血缘尺子）

| 题型 | 覆盖率 | n |
|---|---:|---:|
| single-session-assistant | 1.000 | 11 |
| single-session-user | 0.917 | 12 |
| multi-session | 0.847 | 27 |
| knowledge-update | 0.833 | 15 |
| temporal-reasoning | 0.809 | 27 |
| single-session-preference | 0.667 | 6 |
| **OVERALL** | **0.849** | **98** |

98 = 100 − 2 道无证据题（FR-007 排除）。

### 正确率（T032，3 次独立运行 + 多数票）

| 运行 | 正确率 |
|---|---:|
| rep1 | 70/100 |
| rep2 | 75/100 |
| rep3 | 75/100 |
| **多数票** | **75/100** |

| 题型 | 多数票正确率 | n |
|---|---:|---:|
| single-session-user | 100.0% | 14 |
| single-session-assistant | 90.9% | 11 |
| knowledge-update | 80.0% | 15 |
| multi-session | 70.4% | 27 |
| temporal-reasoning | 66.7% | 27 |
| single-session-preference | **33.3%** | 6 |

### 分桶分账（T034 / T035，严格尺子，多数票后分桶）

| 桶 | n | 正确 | 正确率 | Wilson 95% | 可判 |
|---|---:|---:|---:|---|---|
| 全覆盖 | 73 | 61 | 83.56% | [73.4%, 90.3%] | ✅ |
| 部分覆盖 | 19 | 10 | 52.63% | [31.7%, 72.7%] | ❌ n<20 |
| 零覆盖 | **6** | 2 | 33.33% | [9.7%, 70.0%] | ❌ n<20 |

- 条件增益 = 83.5616% − 33.3333% = **50.2283 pp**（精确值 110/219）
- 检索侧当量 = 6 × 0.502283 = **3.01** 题
- 答题侧当量 = 全覆盖桶仍答错 = **12** 题

## T037 · ORACLE ↔ S 同题配对锚

同一 100 道题，两臂配对：

| 臂 | 正确率 | 严格覆盖率 |
|---|---:|---:|
| ORACLE（零干扰项，每题 3 会话） | 76/100 | 0.9465（全 500 题） |
| S（每题约 500 会话） | 75/100 | 0.8490 |
| **差** | **+1.0 pp** | −9.75 pp |

一致性矩阵：两臂皆对 69 / 仅 ORACLE 对 7 / 仅 S 对 6 / 皆错 18。

**必须标注的混杂**：两臂的干扰项密度相差约 24 倍，这是上界不是因果。即便如此，
"把干扰项放大 24 倍只掉 1pp、且有 6 题反而只在 S 臂答对"这个形状说明差值已落到
运行间噪声量级（S 臂三次运行本身就在 70–75 之间摆动）。

## 补充证据 · ORACLE 臂同口径分桶（单次运行，非 3-repeat）

| 桶 | n | 正确率 | 可判 |
|---|---:|---:|---|
| 全覆盖 | 423 | 79.7% | ✅ |
| 部分覆盖 | 52 | 59.6% | ✅ |
| 零覆盖 | **4** | 25.0% | ❌ n<20 |

条件增益 54.67 pp · 检索侧当量 2.19 · 答题侧当量 **86**。

**两臂各自独立地把条件增益落进了 (50, 60] 这个有意留白的空档**（S 50.23 /
ORACLE 54.67），且两臂的零覆盖桶都远低于 n≥20 下限（6 / 4）。

## 最终判决（T036）

**判据 SHA256 复核**：`2142f722233c97265be6f0238d7ba0e50091611ef82079c1c168d62f1e7609ba`
—— 与 T003 登记值**逐位一致**，判据自登记以来未被改动。

逐条对照 [criterion.txt](./criterion.txt) 原文：

| 判据条款 | 实测 | 是否成立 |
|---|---|---|
| 复现：条件增益 ∈ **[20, 50]** pp | **50.2283 pp** | ❌ 严格大于 50 |
| 复现：检索侧当量 < 答题侧当量 | 3.01 < 12 | ✅ |
| 证伪：条件增益 > 60 pp | 50.2283 | ❌ |
| 证伪：检索侧当量 > 答题侧当量 | 3.01 > 12 为假 | ❌ |

复现需**两条同时**成立，第一条不成立；证伪需**任一条**成立，两条都不成立。

# 判决：**无法判定（INCONCLUSIVE）**

两条独立理由，任一条单独即足以给出该判决：

1. **条件增益 50.2283 pp 落在 (50, 60]**。判据原文：「条件增益落在 (50, 60]
   区间时为『无法判定』，这是**有意留下的空档，不得填补**」，以及「边界值按闭
   区间字面判定，**不得四舍五入到任一侧**」。50.2283 严格大于 50，故不在
   [20, 50] 内。
2. **零覆盖桶 n = 6 < 20**。判据原文：「任一桶的题数 n < 20 时，该桶标记为
   **不可判**，**不得以其点估计支撑结论**」。条件增益的减数正是这个桶的点估计，
   因此该增益在结构上就不具备支撑任何结论的资格。部分覆盖桶 n = 19 同样不可判。

### 我没有做、也不会做的事

这个数字距离「复现」只差 **0.2283 pp**。把它凑进去的路子都很近：零覆盖桶只要
多 1 题答对（3/6 = 50%），条件增益就变成 33.56 pp，落进 [20, 50]，配上已经成立
的第二条，判决立刻变成「复现」。同理，若改用宽松尺子（含 fact 模糊配对），零
覆盖桶还会更小、增益还会更动。这些都**没有做**：判据未重新登记、n≥20 下限未
放宽、桶未合并、尺子未换、增益未四舍五入。

**一个 n=6 的桶能靠 1 题翻转结论，这本身就是 n≥20 下限存在的全部理由。**

### 这次判决实际证明了什么

「无法判定」不等于没有结论。三项**不依赖**判据阈值的观测是站得住的：

1. **coverage ≠ answer 在第二个 benchmark 上定性成立**。全覆盖桶 83.56% —— 证据
   完整摆在面前仍有 **12 题**答错；答题侧当量（12）是检索侧当量（3.01）的 **4
   倍**。ORACLE 臂同一形状更极端：86 vs 2.19，约 **39 倍**。
2. **检索对干扰项密度的鲁棒性远超预期**。干扰项放大约 24 倍，正确率只掉 1.0 pp，
   覆盖率只掉 9.75 pp。瓶颈不在"从大海里捞针"。
3. **`single-session-preference` 是纯答题侧失败**。ORACLE 臂覆盖 0.989 / 正确率
   60.0%；S 臂覆盖 0.667 / 正确率 33.3%。而这一型此前被 loader 直接硬报错拒收
   （G1），本次是它第一次产生任何数字。

定量上「条件增益落在 20–50pp」的预注册假设**没有被复现**，落在了空档里；定性上
「答题侧是主要瓶颈」在第二个 benchmark 上**得到了支持**。这两句话必须一起讲。

### 数字可比性声明

- 本次全部数字来自 **LongMemEval-S (cleaned) 的 100 题分层子集** 与
  **LongMemEval-oracle 全 500 题**，**不得简称为「LongMemEval 全量」**。
- 官方已废弃旧版本数据文件；本次用的是 cleaned 版，与第三方基于旧版发布的数字
  **不可直接对比**。
- 答题/抽取模型为 `Qwen/Qwen3.6-35B-A3B-FP8`（自建 vllm），判分为
  `deepseek-v4-flash`。与他人 leaderboard 数字存在 answerer / judge regime 差异，
  **跨系统比较无效**。
- LongMemEval 在此声明为**独立新基线**，**不替代**、不混入 LoCoMo 的 85.71%。

### 本次范围排除项（FR-026）

官方分题型判分口径 · 弃答（abstention）子集 · `longmemeval_m` · V2 ·
oracle-injection 干预臂 —— 均**未做**，不在本判决覆盖范围内。

---

## 后记（2026-07-26 晚）：temporal-reasoning 残差的逐题验尸 —— 是 harness 缺口，不是能力缺口

016 判决交付后，用 answerer-parity 臂（`deepseek-v4-pro`，quota 12，同 store 同配方，
3 跑多数票 **82/100**）重跑 S-100，`temporal-reasoning` 仍是最差题型：**18/27 = 66.7%**，
且与 `Qwen3.6-35B` 的 66.7% **逐点相同**。9 道错题占 S-100 全部 18 道错题的一半。

彼时的判断是：这块残差**既排除了检索**（gold 中位 rank 3、`oracle_lift@30 = 0.000`）、
**又排除了答题模型强度**（两个模型同分），因此是真正的深水区。前两条排除成立，
**结论错误**。

### 反解锚点：四个错误答案指向同一个"今天"

逐题验尸（离线 join，零 LLM 调用）发现四道题的错误答案不是随机偏差，而是精确的
系统性偏移。把每个答案当作"今天 − 证据日期"反解出隐含的"今天"：

| conv | 问 | gold | 模型答 | 证据日期 | 反解出的"今天" |
|---:|---|---:|---|---|---|
| 92 | 几天前去漂流 | 3 天 | `1136 days` | 2023-06-17 | **2026-07-27** |
| 56 | 几周前去 Nordstrom | 2 周 | `192 weeks` | 2022-11-18 | **2026-07-24** |
| 39 | 几周前见姑妈 | 4 周 | `177 weeks` | 2023-03-04 | **2026-07-25** |
| 6 | 多少个月没去博物馆 | 5 个月 | `45 months` | 2022-10-22 | **2026-07-22** |

四个"今天"全部落在 **2026-07-22 ~ 2026-07-27**，即跑批当周（2026-07-26）。
模型没有算错减法，它是在用**自身内置的当前日期**当锚点。

### 根因

`runner.go:325 buildAnswerPrompt` 只拼 `RETRIEVED MEMORIES` + `QUESTION`，
**答题上下文中不存在任何"现在"**。LongMemEval 的 `question_date` 字段虽已在
`longmemeval.go:31` 解析，但只用作 session 日期的兜底，**从未进入答题上下文**。

加重因素：`forceAnswerSystemPrompt:223` 明写 **"NEVER answer relative to today's
date"**。该规则为 LoCoMo 的绝对型 "when did X happen" 而写，套到 LongMemEval 的
相对型 "how many days ago" 上正好是反的。

记忆条目本身**是带日期的**（`Line():452` 输出 `[event: YYYY-MM-DD]`）。
证据在手、事件日期在手、唯独不知道"现在"。

### 九题重新分账

| 类 | conv | 病因 | 归属 |
|---|---|---|---|
| 锚点缺失 | 6, 39, 56, 92, 96 | 无 "now"，模型以 2026 为锚 | **harness** |
| 锚点缺失（相对排歧） | 98 "past weekend" | 不知今天则无法定位"上周末"，在 03-15 / 03-19 两条候选证据中选错 | **harness** |
| 相对时长 | 70 | 证据在手（rank 2），3 跑中 1 跑正确 | 边界噪声 |
| 弃答 regime 伪影 | 66（`_abs`） | gold = "信息不足"，而 `--force-answer` 禁止弃答；FR-026 已声明本次不做 abstention 子集 | **regime** |
| 真·检索失败 | 78 | `gold_ranks` 为空，完全未召回；需时间窗过滤 | 检索（n=1） |

**9 道错题中 6 道同源于同一个 harness 缺口；真正的检索失败只有 1 道。**

这也解释了为何"换更强答题模型完全不动"：更强的模型只会更自信地使用它自己的
日期锚。**"答题模型强度已排除"这条证据，此前被误读为"能力已到顶"，实为
"两个模型都拿不到它们都没有的信息"。**

### 修复与不变式

`bb99d58`（纯评测口径修正，按宪法 IV 与算法改动分离提交，**引擎零改动**）：
`currentDate` 非空时在答题上下文首行注入 `CURRENT DATE: <question_date>`，
并追加一条显式 override 上述 LoCoMo 规则的相对时间规则。

**LoCoMo 不变式**：LoCoMo 无 `question_date` → `currentDate` 恒空 → 不注入锚点、
不追加规则，prompt **字节级不变**。由 `TestAnswerPromptWithoutCurrentDateIsUnchanged`
断言，LoCoMo 85.71% 基线 invariant by construction，无需重跑。

### 对本判决的效力

**本节不修改 016 的判决（无法判定）**。016 的条件增益分账基于严格覆盖率尺子，
而本缺口位于答题侧、在覆盖率之后，不改变任何一个桶的覆盖归属。受影响的是
`temporal-reasoning` 的**准确率**，不是覆盖率。

**但它修正了 016 的一条派生叙事**：不能再说"temporal-reasoning 是排除了检索与
答题模型后的纯能力残差"。正确表述是：该题型的绝大部分误差是**评测上下文构造
缺陷**，engram 的检索侧在此题型上本就没有可得空间（`oracle_lift@30 = 0.000`）。

### 方法论留档

**当错误答案是一个数而非一段胡话时，先反解它的隐含参数，再判能力。**
四个看似离谱的数字（1136 天 / 192 周 / 177 周 / 45 个月）互相之间毫无关系，
但反解出的锚点全部落在同一周内 —— 这是 harness 伪影的指纹，任何"模型能力不足"
的解释都无法产生这种一致性。

### 修复后实测（臂 C，2026-07-26 20:02）

与臂 A **完全同配方**（`deepseek-v4-pro` / quota 12 / top-k 30 / hybrid /
`--force-answer` / mem0-aligned judge / 同一 store），**唯一变量 = `bb99d58`
的 CURRENT DATE 注入**。3 跑。

| | r1 | r2 | r3 | 均值 | 多数票 |
|---|---:|---:|---:|---:|---:|
| A（无锚点） | 82 | 80 | 77 | 79.7 | **82** |
| **C（有锚点）** | **86** | **86** | **83** | **85.0** | **85** |

**C 的最低跑（83）高于 A 的最高跑（82）**——三跑无重叠，不是跑间噪声。

| 题型 | A（多数） | C（多数） | Δ |
|---|---:|---:|---:|
| **temporal-reasoning** | 18/27 (66.7%) | **23/27 (85.2%)** | **+5** |
| knowledge-update | 13/15 | 13/15 | 0 |
| single-session-assistant | 10/11 | 10/11 | 0 |
| single-session-user | 13/14 | 13/14 | 0 |
| multi-session | 23/27 | 22/27 | −1 |
| single-session-preference | 5/6 | 4/6 | −1 |

**验尸点名的 6 道题全部翻正，零假阳性**，且每一道都是 **A 票 0/3 → C 票 3/3**
的确定性翻转（非边缘票数变动）：

```
conv  6  45 months     -> 5 months       gold 5
conv 39  4 March 2023  -> 4 weeks        gold 4
conv 56  192 weeks     -> 2 weeks        gold 2
conv 92  1136 days     -> 4 days         gold 3~4
conv 96  19 March 2023 -> 7 days         gold 7~8
conv 98  Mountain bike -> road bike      gold road bike   ("past weekend" 排歧)
```

#### 副作用（如实登记，未做后续 prompt 微调）

- **conv 52（multi-session，真回归）**：`How long have I been working in my
  current role?` gold「1 年 5 个月」。A 答对 2/3；C 三跑答「7 months / 3 days /
  3 days」。`currentDateRule` 把**「持续至今多久」**与**「距今多久前发生」**归入
  同一框架，模型用 CURRENT DATE 减去了错误的基准事件。这是规则过度泛化的代价。
- **conv 94（temporal，翻负但 A 的"对"可疑）**：A 三跑为
  `164 weeks / 3 weeks / 3 weeks`，C 为 `9 / 9 / 6 weeks`。A 的首跑同样是
  2026 锚点伪影，后两跑在**没有任何锚点**的情况下答出「3 weeks」——那是猜中，
  不是能力。C 有了锚点却算错，说明**该题存在真实的候选日期排歧问题**，此前被
  幸运猜中掩盖。
- **conv 4 / 42 / 57（preference，净 0）**：三题在「给一条具体建议」与「列一串
  产品名」之间摇摆，A 自身 3 跑即不稳定（T,T,F）。与日期无关，属采样噪声。

净账：temporal **+6 确定性翻正 −1 真回归**；multi-session **−1 规则副作用**；
preference 净 0。**未针对这些错题继续调 prompt**——那会滑向对 6 道题的过拟合。

#### 用量（按 usage 插桩）

`answer` 302 次 / 911,859 in / 132,440 out（v4-pro）；`judge` 300 次 /
110,377 in / 34,806 out（v4-flash）；`rewrite` 2 次。
**$0.29–0.54 ≈ ¥2.1–3.9**（60% 缓存命中 ~ 全 miss 上界）。
`extract` / `embed` 零付费（store 复用 + 本地 embedding），未动用远端 GPU。

#### 结论

LongMemEval-S (cleaned) **分层抽样 100 题**、`deepseek-v4-pro` 答题、3 跑多数票：
**85/100**。`temporal-reasoning` 从 66.7% 升至 **85.2%**，不再是最差题型
（现最差为 multi-session 81.5% 与 preference 66.7%，后者 n=6 不可判）。

**这不是 engram 变强了，是测量变诚实了。** 检索侧一行未改，引擎一行未改。

---

## US4 · S 臂全量 500（LongMemEval-S cleaned，首次正当「全量」基线）

**日期**: 2026-07-28 · **数据集**: `longmemeval_s_cleaned.json` 官方 cleaned 版 **全 500 题**
（非抽样、非 oracle）。题型分布 multi-session 133 / temporal-reasoning 133 /
knowledge-update 78 / single-session-user 70 / single-session-assistant 56 /
single-session-preference 30。

**配方**:与 016 各臂逐字相同(`--chunks --chunk-quota 12 --top-k 30 --retrieval hybrid
--force-answer --judge-mem0-aligned`),答题/抽取 = 本机 vllm `Qwen/Qwen3.6-35B-A3B-FP8`,
embedding = 本机 `bge-large-en-v1.5`(1024d),判分 = `deepseek-v4-flash`(mem0-aligned)。
**含 `bb99d58` 的 CURRENT DATE 锚点注入**(LongMemEval 恒有 `question_date`,故锚点恒注入)。

### 建库

512,662 条目(其中 chunk 222,832),耗时 **6.0 h**(21,545s)。抽取解析失败 82 次
(占抽取调用 ~0.3‰,既有引擎行为、非本轮引入)。向量补齐 6 轮收敛在 missing=97
(见下「G-向量门」)。

### G-向量门:按字面失败,基于「影响面=0」放行

missing 五轮收敛在 **97**(pass1 95211 → pass2 118 → pass3..6 均为 97)。
97 条全是 `chunk`,打端点实测报 `decoder prompt (length 742) is longer than the
maximum model length of 512`——**bge-large-en-v1.5 位置编码硬顶 512 token**,而这些
chunk 是韩文 / markdown 表格 / 坐标数字流,字符→token 膨胀撞顶,**重试多少轮都补不上**。

016 设此门的理由是「带缺向量语料出分会静默偏低」。本门把该失效模式直接量化(工具
`scripts/oracle-category-coverage.py` 的同型逻辑,离线):

| 口径 | 值 |
|---|---:|
| 缺向量条目 / 全部 | 97 / 512,662 = **0.019%** |
| 缺向量 chunk / 全部 chunk | 97 / 222,832 = 0.044% |
| **承载 gold 的 chunk 总数** | 894 |
| **其中缺向量的** | **0** |
| **受影响题数** | **0 / 500** |

**门要防的事实实测不存在**(894 条压 gold 的 chunk 一条没漏;且缺向量 ≠ 不可检索,
这些条目仍走 FTS + 实体臂,只丢语义臂,属宪法 V 的按信号降级)。据此放行。
**这是本会话自立的门、非 016 那种带 SHA256 的注册判据**——「按字面失败 + 放行依据」原样记于此,
不粉饰、不四舍五入凑进 0。残留的 512-token 截断是 `bge-large-en-v1.5` 的已知能力边界
(非 engram 引擎缺陷),留作「换更长上下文 embedder」的 future work,不在本轮范围。

### 结果(3-rep 多数票)

| run | 正确率 |
|---|---:|
| run-1 | 395/500 = 79.00% |
| run-2 | 392/500 = 78.40% |
| run-3 | 402/500 = 80.40% |
| **多数票** | **404/500 = 80.80%** |

warm-up(丢弃,1 rep)= 390/500 = 78.00%,与主臂 Δ=+2.80pp,落在 temp=1.0 跑间
噪声带内,**判题 key 与配置无翻车**(gotcha #11 已提前堵:开跑前探 `/v1/messages` HTTP 200)。

| 题型 | 多数票正确率 | n | turn 覆盖率 |
|---|---:|---:|---:|
| single-session-user | **100.0%** | 70 | 0.969 |
| single-session-assistant | 92.9% | 56 | 1.000 |
| knowledge-update | 80.8% | 78 | 0.917 |
| temporal-reasoning | **78.2%** | 133 | 0.792 |
| multi-session | 74.4% | 133 | 0.874 |
| single-session-preference | **53.3%** | 30 | 0.778 |
| **OVERALL** | **80.80%** | 500 | 0.879(n=479) |

### 后续纠偏：基线存在超长 turn 截断（2026-07-28）

基线提交 `a40b48a` 的 `buildSessionChunks` 会把超过 1100 code point 的单 turn
直接截断。多数票中 `single-session-assistant` 的 4 道错题全部是三跑全错，且
gold-bearing assistant turn 的关键答案文本都位于旧截断点之后：

| question_id | 关键答案文本起点（code point offset） |
|---|---:|
| `b759caee` | 1149 |
| `51b23612` | 1312 |
| `8752c811` | 1501 |
| `58470ed2` | 1520 |

因此上表的 turn coverage=1.000 只表示检索结果映射到了 gold **DiaID**，不表示答案
所在片段实际进入了索引或答题上下文；不得再把该指标描述为 answer-span visibility。

后续修复把超长 turn 按边界无损拆成多个 ≤1100 code point 的 speaker-attributed chunk，
每片保留相同 DiaID；coverage 的集合语义会去重。持久化 store 中内容变化的同名 chunk
必须先删除再写入，以清除旧 embedding；已不存在的 chunk 同步删除。因此新口径必须先
重建/补齐 chunk 向量，再做同配方 500 题复跑。

**本节只登记基线缺陷与修复要求，不修改历史 404/500，也不声称已获得 +0.8pp。**
只有可比的全量复跑完成后，才可声明替代基线。

### 两个跨臂对照(必须带混杂声明)

1. **vs 016 ORACLE-500(76.4%)**:本 S-500 高 **+4.4pp**。但 ORACLE 臂跑于 `bb99d58`
   锚点修复**之前**(无 CURRENT DATE),本臂在**之后**。temporal-reasoning 从 oracle 的
   64.7% 升到 78.2%,与 016 arm C(S-100,加锚点后 temporal 66.7%→85.2%)同向。
   **+4.4pp 里锚点修复的贡献未剥离,不得解读为「S 臂强于 oracle 臂」**——两者还差
   干扰项密度(oracle 零干扰项 vs S 约 500 会话/题)。
2. **vs 016 S-100(75/100,Qwen 无锚点)**:口径不同(全量 vs 分层抽样、无锚点 vs 有锚点),
   **不可直接相减**。本数是首个正当「全量」数。

### 用量(按 usage 插桩)

| 角色 | 调用 | in | out |
|---|---:|---:|---:|
| answer | 1500(3 rep) | — | — | 本地 vllm,**零付费** |
| judge | 1500 | 144,386 | 193,981 | deepseek-v4-flash,**唯一付费口** |
| embed | 1516 | 30,706 | — | 本地,**零付费** |
| extract | 0(复用建库抽取) | — | — | — |

answer 上下文均值 **3,322 token**。judge 实付费约 **¥1–2**(338k token,flash 价)。
建库/答题 box wall-clock 共 **~6.5 h**(含向量补齐)。

### 基线声明(宪法 IV)

**LongMemEval-S (cleaned) 全量 500 的 engram 基线 = 80.80%**(Qwen3.6-35B 答题 /
bge-large-en 1024d / deepseek-v4-flash mem0-aligned judge / top-k30 chunk-quota12
hybrid / force-answer / 含 CURRENT DATE 锚点)。

- 这是**独立新基线**,**不替代、不混入** LoCoMo 的 85.71%。两数据集任务形态不同、
  分母不同(500 vs 1540)、judge regime 虽同但题面不同。
- 与他人 leaderboard 数字存在 answerer / judge / 数据版本(cleaned vs 旧版)差异,
  **跨系统直接比较无效**。Mem0 blog 报 94.4、MemOS 论文报 77.8(GPT-4o-mini 统一口径),
  均与本数**不可直接对比**。
- eval-config(数据集版本 / 配方 / 锚点)与任何算法改动**分开 commit**(本 verdict 仅记录,
  无引擎/算法改动——`git diff --name-only -- memory embedding provider store internal` 为空)。

### 处置

- **box 已停**:vllm killed(GPU 0 MiB)、box 上 `judge.env` 已删、本地凭据脚本已清。
  ⚠️ **实例空转约 12 h**(收尾脚本因 `&` 后台化 bug 未杀 vllm)——「空闲必停」的执行缺口,
  根因是 `setsid ... & disown` 的收尾 watcher 没活下来;已记教训,后续 box 作业的收尾
  改为「主进程退出前同步执行 teardown」而非另挂后台 watcher。
- 产物归档 `.longmemeval-run/`(逐题 jsonl × 3 rep + coverage × 6 pass + regime/cost,
  gitignored,凭据零命中)。
