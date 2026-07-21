# Feature Specification: LoCoMo Judge 口径对齐(mem0-aligned)

**Feature Branch**: `007-judge-metric-alignment`

**Created**: 2026-07-21

**Status**: Draft

**Input**: User description: "把 engram 的 LoCoMo LLM-judge 宽松度逐条对齐 Mem0,剥离 65.4% vs 88.83%(同 1540 分母)差距里的 judge 严格度伪影。纯 cmd/locomo-bench 改动,引擎零改。默认 off 的 flag + fingerprint 口径隔离 + anti-放水 golden 夹具。只做免费部分,不量化新分,宪法 IV 声明新基线。" 设计定稿:[docs/superpowers/specs/2026-07-21-judge-口径-alignment-design.md](../../docs/superpowers/specs/2026-07-21-judge-口径-alignment-design.md)。

## 背景

读三方 LoCoMo LLM-judge 源码确认:分母(cat 1-4=1540,cat-5 排除)、拒答(force-answer)、聚合(micro-avg)三轴,engram 与 Mem0 / OmniMemEval(MemOS)**相同**;唯一实质差异是 **judge 宽松度**——engram 明显更严(缺"部分给分"与"±14 天日期容差")。因此 65.4% vs 88.83/92.5(同 1540 分母)的 ~23pp 差距中含一块 **judge 严格度伪影**。本特性消除该伪影使对比公平,并诚实标注"这是口径对齐,非算法涨点"。证据见 [docs/competitive-benchmarks.md](../../docs/competitive-benchmarks.md) §6。

本特性是评测口径特性,"用户"= 维护者。它触及**评分口径**(judge)→ 宪法 IV metric 变更:必须声明新基线、eval 结果单独提交。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 免费口径对齐 + 防放水护栏 (Priority: P1)

维护者在 `cmd/locomo-bench` 中把 judge 宽松度对齐到 Mem0 口径,并**近免费**验证新 judge "不放水"(不会把真错答案判对)。交付一个"已验证不放水的新 judge + 口径隔离"能力,不量化实际新分。这是 MVP。

**Why this priority**: 这是整个免费增量的全部价值——不花答题钱就把"judge 严格度伪影"从差距中剥离出来,并用陷阱夹具证明剥离是公平的、非放水。没有它,后续任何"新基线"数字都不可信。

**Independent Test**: 打开 `--judge-mem0-aligned`,对 golden 夹具跑新 judge:所有"该判对的宽松案例"判 CORRECT、所有"anti-放水陷阱"判 WRONG;离线确定性层(prompt 含规则子句 + `parseJudgeVerdict`)在 `CGO=0` CI 通过;`git diff` 引擎目录为空。全程零答题调用,仅数十次夹具判分。

**Acceptance Scenarios**:

1. **Given** flag off(默认),**When** 跑任意 bench 评测,**Then** judge 行为与当前老严格 judge 逐字一致(零回归),fingerprint 不含 `judge=mem0-aligned`。
2. **Given** flag on,**When** 对含"列表 1/2 命中"的预测判分,**Then** 判 CORRECT(部分给分生效)。
3. **Given** flag on,**When** gold 日期与预测日期相差 ≤14 天,**Then** 判 CORRECT;相差 >14 天,**Then** 判 WRONG。
4. **Given** flag on,**When** 预测给出错名字 / 矛盾事实 / 错数字 / 零主题重叠 / "我不知道",**Then** 一律判 WRONG(anti-放水陷阱守住)。
5. **Given** flag on,**When** 生成 run fingerprint,**Then** 追加 `;judge=mem0-aligned`,与老口径 run 不同 → 不会被静默配对/复用缓存。

---

### User Story 2 - 授权后量化新基线 (Priority: P2,双门控:GO 前提 + 显式花钱授权)

在 US1 交付(新 judge 已验证不放水)之后,维护者在**显式成本授权**下,用新 judge 重判/重跑,量出 65.4% 在新口径下的实际值,声明为新基线。

**Why this priority**: 有价值但花钱,且逻辑上依赖 US1 先证明不放水。默认不执行。

**Independent Test**: 授权后,同 transcript 双口径对跑(或对存活 transcript 重判),产出 eval-log 新节 "Judge 口径 v2 (mem0-aligned) — 新基线 X%(老严格 judge 65.4%)" + false→true flip 抽查;eval 结果单独提交。

**Acceptance Scenarios**:

1. **Given** US1 已交付且成本已授权,**When** 双口径对跑,**Then** 记录新基线 X% 与 flip 明细,并**明标"口径对齐非涨点,不得与其它杠杆收益叠加"**。
2. **Given** 未授权,**When** 任何时候,**Then** US2 不执行,不产生答题成本。

---

### Edge Cases

- 日期**恰好** 14 天边界 → 判 CORRECT(容差含端点)。
- 列表**零命中** → 判 WRONG(部分给分下限)。
- 预测比 gold 更详细但含 gold 核心事实 → 判 CORRECT(额外细节不扣分)。
- 情绪同价("proud" vs "fulfilled")→ 判 CORRECT;情绪反价("proud" vs "disappointed")→ 判 WRONG。
- golden 层无 endpoint(离线 CI)→ 该层跳过(env-gate),离线确定性层仍守 prompt/parse 断言。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 将 Mem0 `_JUDGE_TEMPLATE`(无 evidence 变体)的 7 条宽松规则(部分给分 / 同义 / 额外细节 / 日期 ±14 天·时长 ±50% / 语义重叠含情绪同价 / 同 referent / 重事实轻措辞)与收口条款("仅零命中或完全跑题才 WRONG")移植进 `judgeSystemPrompt`,**保留现有 `{"correct": true|false}` JSON 输出契约**,`parseJudgeVerdict` 不变。
- **FR-002**: 新宽松度 MUST 置于**默认 off** 的 `--judge-mem0-aligned` flag 之后;flag off 时 judge 行为与当前实现逐字不变。
- **FR-003**: 当 flag on 时,run fingerprint MUST 追加 `;judge=mem0-aligned`,使新口径 run 与老口径 run 的 fingerprint 不同(口径隔离,防静默跨口径配对/缓存复用)。
- **FR-004**: 系统 MUST 提供 golden 夹具(~25-30 条,`{question, gold, predicted, expected_correct, rule, note}`),覆盖三类:该判对的宽松案例、anti-放水陷阱(错名字/矛盾/错数字/零重叠/不知道/日期差>14天)、边界(恰好14天/零命中)。
- **FR-005**: 系统 MUST 有离线确定性测试层(进 `CGO=0` CI,零 LLM):断言新 prompt 文本含 7 条规则关键子句 + `parseJudgeVerdict` 正确性。
- **FR-006**: 系统 MUST 有近免费 golden 测试层(env-gate,不进离线 CI):用 relay judge 真跑夹具、断言逐条 label 匹配;**任一 anti-放水陷阱被判对即测试失败**。
- **FR-007**: 引擎目录(`memory embedding provider store internal`)MUST 零改;`git diff --name-only -- memory embedding provider store internal` 为空。
- **FR-008**: 系统 MUST 记录新基线声明协议(宪法 IV):judge 口径变更 → US2 声明新基线、eval 结果单独提交、明标"口径对齐非涨点";US1 mechanism 提交不声称任何分数。
- **FR-009**: US1 MUST 不做实际分数量化(无重判全量 / 无重跑全量);仅夹具判分。量化在 US2,需显式成本授权。
- **FR-010**: 默认 judge MUST 保持老严格口径,直到 US2 正式声明新基线后方可(单独决策)翻默认。

### Key Entities *(include if feature involves data)*

- **judge 口径(judge alignment mode)**:`strict`(老,默认) | `mem0-aligned`(新,flag on)。决定 `judgeSystemPrompt` 规则文本与 fingerprint 标签。
- **golden 夹具项**:一条判分测试用例,含 question / gold / predicted / expected_correct(bool)/ rule(所测规则)/ note。anti-放水陷阱项 expected_correct=false。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001(核心,防放水硬门)**: 新 judge 对夹具中**每一条 anti-放水陷阱判 WRONG**,零陷阱被判对。任一陷阱判对 = 增量不通过。
- **SC-002**: 新 judge 对夹具中**每一条"该判对的宽松案例"判 CORRECT**(对齐生效)。
- **SC-003**: US1 全程**零答题(answer)调用**,仅数十次夹具判分调用(近免费);无全量重判 / 重跑。
- **SC-004**: 引擎目录 `git diff` 为空(FR-007)。
- **SC-005**: flag off 时,对同一 (question, gold, predicted) 三元组,新实现判定结果与改动前逐字一致(零回归)。
- **SC-006**: 开/关 flag 的两次 run 的 fingerprint 不同,确保新口径分不会与老 judge 65.4% 基线被静默比较。
- **SC-007**: 宪法 IV 诚实约束落地:eval-log 新基线协议成文,eval 结果与 mechanism 分开提交,且文字明标"口径对齐非涨点"。

## Assumptions

- relay judge endpoint(gpt-5.6-luna 或等价)对 golden 层可用;不可用时该层按 env-gate 跳过,不阻塞离线 CI。
- 复用 003 冻结的 relay judge 模型口径;不引入新模型依赖。
- 旧答题 transcript 可能已失效(scratchpad、gitignored);US1 不依赖其存活,US2 量化按需重判或重跑。
- Mem0 judge 规则文本取自本机已克隆的 `../mem0/evaluation/benchmarks/locomo/prompts.py:218-245`(2026-07-21 采集)。
- 死规则(禁付费云 rerank)与本特性无关(不涉检索)。
