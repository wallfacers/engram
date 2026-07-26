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

## US3 · S 臂

**进行中** —— 分层抽样 100 题（配额 27/27/15/14/11/6，seed 20260726，
两次运行 sha256 一致），49,556 条消息，4,789 次抽取。

## 最终判决（T036）

**未执行** —— 等待 US3 完成。判据锁定于 [criterion.txt](./criterion.txt)
（SHA256 `2142f722…09ba`），**不得重新登记**。
