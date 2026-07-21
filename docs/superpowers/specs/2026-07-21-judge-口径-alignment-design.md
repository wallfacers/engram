# 设计:LoCoMo Judge 口径对齐(mem0-aligned)

**日期**:2026-07-21 · **状态**:brainstorm 定稿,待进入 SDD(specify) · **前置背景**:[docs/competitive-benchmarks.md](../../competitive-benchmarks.md) §6

## 背景与动机

读 Mem0(`mem0/evaluation/benchmarks/locomo/prompts.py:218-245`)、OmniMemEval(`OmniMemEval/scripts/utils/prompts.py:216-240`)与 engram(`cmd/locomo-bench/runner.go:451-457`)三方 LoCoMo LLM-judge 源码后确认:分母(cat 1-4=1540,cat-5 排除)、拒答(force-answer)、聚合(micro-avg)三轴与竞品**相同**;唯一实质差异是 **judge 宽松度**——engram 明显更严:

- **无"部分给分"**:Mem0 "gold 列表命中 ≥1 项即 CORRECT",engram "遗漏 gold fact 即 false"。列举/计数/多跳题系统性吃亏。
- **无"±14 天日期容差"**:Mem0 ±14 天算对,engram "日期不同就 false"。temporal(321 题)吃亏。
- engram judge 注释自称 "aligned with mem0",实际漏了以上两条核心宽松规则。

因此 65.4% vs 88.83%(同 1540 分母)的 ~23pp 差距中,有一块是 **judge 严格度伪影**,而非真实力差。本设计消除这块伪影,让对比公平。

## 目标与非目标

**目标**:把 engram LoCoMo judge 宽松度**逐条对齐 Mem0**;提供**近免费、防放水**的验证;按宪法 IV 声明口径变更。

**非目标(明确不做)**:量化实际新分(重判/重跑,另开、需授权)、翻默认、答题 prompt 5 步升级(另一特性)、任何检索/抽取/引擎改动。

## 约束

- 纯 `cmd/locomo-bench` 改动;引擎(`memory embedding provider store internal`)**零改**,`git diff` 须空。
- 宪法 IV:judge 口径是 metric 定义变更 → 声明新基线、eval 结果单独提交、明标"口径对齐非涨点"。
- 死规则(禁付费云 rerank)无关(不涉检索)。
- 必须有防放水验证(不得把错答案判对)。

## 设计

### 1. judge 规则改写
只改 `runner.go` 的 `judgeSystemPrompt` 规则文本,**保留现有 JSON 契约 `{"correct": true|false}`**(`parseJudgeVerdict` 已测,不动)。移植 Mem0 `_JUDGE_TEMPLATE`(无 evidence 变体)7 条:

1. 部分给分:gold 列表命中 ≥1 项即 CORRECT,零命中才 WRONG。
2. 同义改写等价。
3. 额外细节不扣分。
4. 日期 ±14 天 / 时长 ±50% / 相对日期匹配。
5. 语义重叠(情绪同价:proud=fulfilled=accomplished)。
6. 同 referent(指同一命名实体即对)。
7. 重事实轻措辞。

收口条款:**仅当"零命中 gold 项"或"完全跑题"才判 WRONG**。

### 2. 口径隔离(宪法 IV)
- 新增 flag `--judge-mem0-aligned`,**默认 off**。老严格 judge 仍为默认,直到某次授权重跑正式声明新基线后才翻默认。
- 运行 fingerprint(`main.go:1018` 附近)追加 `;judge=mem0-aligned`(仅当 flag on)。作用:新 judge 的分永远不会被静默拿去与老 judge 的 65.4% 基线配对/复用缓存——口径不同 → fingerprint 不同 → 自动隔离。

### 3. Golden 夹具(防放水核心护栏,TDD 主交付)
- `cmd/locomo-bench/testdata/judge_golden.jsonl`,~25-30 条,字段 `{question, gold, predicted, expected_correct, rule, note}`。
- 三类必备:
  - **该判对(expected_correct=true)**:列表 1/2 命中、±14 天日期、时长 ±50%、情绪同价、同义、额外细节、同 referent 换描述。
  - **anti-放水陷阱(expected_correct=false)**:错名字、矛盾事实、错数字、零主题重叠、"我不知道"、日期差 >14 天。
  - **边界**:日期恰好 14 天、列表零命中。
- 两层测试:
  - **CI 离线确定性层**(进 `CGO=0` CI,零 LLM):断言新 prompt 文本含 7 条规则关键子句 + `parseJudgeVerdict` 正确性。
  - **近免费 golden 层**:`TestJudgeMem0AlignedMatchesGolden` 用 relay judge 真跑夹具、断言逐条 label 匹配。**判据:任一 anti-放水陷阱被判对 = 放水 = 测试红 = 增量不通过。** 因需 endpoint,按现有约定用 env gate(不进离线 CI,手动近免费跑,成本几分钱)。

### 4. 新基线声明协议
1. 本增量:mechanism-only 提交,**不声称任何分数**。
2. 未来授权步:同 transcript 双口径对跑(或重判),eval-log 新开一节 "Judge 口径 v2 (mem0-aligned) — 新基线 X%(老严格 judge 为 65.4%)" + false→true flip 抽查;**eval 结果单独提交**。
3. 明写:**这是对齐竞品 judge 口径,非算法涨点,不得与其它杠杆收益叠加宣称**。之后方可翻默认。

## 验证形态(TDD)

- 失败测试先行:先写 golden 夹具 + `TestJudgeMem0AlignedMatchesGolden`(此时旧严格 judge 会在宽松案例上判错 → 红),再改 `judgeSystemPrompt` 令其转绿;anti-放水陷阱同时守住不许转"放水绿"。
- 离线层:prompt-含-规则子句断言 + `parseJudgeVerdict` 单测,进 CI。
- 引擎零改:`git diff --name-only -- memory embedding provider store internal` 空。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 部分给分放水(把错答案判对) | anti-放水陷阱夹具作硬判据;honest 标注非涨点 |
| 新 judge 分被误当真实力涨点 | fingerprint 隔离 + eval-log 明标口径变更 + 默认 off |
| 旧 transcript 已失效无法零成本重判 | 本增量不依赖重判;量化步骤单列、需授权 |
| golden 夹具需 endpoint | env-gate,不进离线 CI;离线层仍守 prompt/parse 确定性 |

## 落地边界

单一实现计划可覆盖:改 1 个 prompt 常量 + 加 1 个 flag + 1 处 fingerprint + 1 个夹具文件 + 2 个测试。无需拆分。
