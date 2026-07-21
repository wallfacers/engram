# Research: LoCoMo Judge 口径对齐

Phase 0 决策记录。Technical Context 无 NEEDS CLARIFICATION;以下为关键设计决策的定稿。

## R1 — 只改规则文本,保留 JSON 输出契约

- **Decision**: 只重写 `judgeSystemPrompt` 的规则正文,输出契约保持现有 `{"correct": true|false}`;`parseJudgeVerdict`、`buildJudgePrompt` 一字不动。
- **Rationale**: Mem0 口径的实质在**判定规则**(部分给分、日期容差…),而非 JSON 外壳(Mem0 用 `{"label":CORRECT/WRONG,"reasoning"}`)。沿用 engram 已测的解析,回归面最小、无需改调用方。
- **Alternatives**: 照搬 Mem0 的 `{"label",...}` 输出——被否:要改 `parseJudgeVerdict` 及其测试,收益为零(口径不由外壳决定)。

## R2 — 默认 off flag + fingerprint 隔离(宪法 IV 落地形态)

- **Decision**: `--judge-mem0-aligned`(默认 false);on 时 run fingerprint 追加 `;judge=mem0-aligned`。
- **Rationale**: judge 口径是 metric 定义变更(宪法 IV)。默认 off 保证旧口径逐字零回归(SC-005),既有 65.4% 基线可复现;fingerprint 分叉使新/旧口径的分**不可能被静默配对或复用缓存**(SC-006)。翻默认延后到 US2 正式声明新基线。
- **Alternatives**: 直接替换默认 judge——被否:一次性打断所有历史基线可比性,违背宪法 IV 的"改口径需声明新基线"。

## R3 — 移植哪些规则(照搬 Mem0 7 条 + 收口)

- **Decision**: 移植 Mem0 `_JUDGE_TEMPLATE` 无 evidence 变体的 7 条:①部分给分(gold 列表≥1 命中即对,零命中才 WRONG)②同义改写 ③额外细节不扣分 ④日期 ±14 天/时长 ±50%/相对日期 ⑤语义重叠(情绪同价)⑥同 referent ⑦重事实轻措辞;收口"仅零命中或完全跑题才 WRONG"。用户已选"逐条照搬(含部分给分)"。
- **Rationale**: 目标是与竞品**用同一把尺**;删任一条即引入新的口径偏差,失去可比性。放水风险由 R4 的 anti-放水夹具兜底。
- **Alternatives**: 保守子集(不含部分给分)/ 按类别门控部分给分——头脑风暴中被否,因非完全对齐。

## R4 — 防放水:anti-放水 golden 夹具作硬判据

- **Decision**: ~25-30 条 golden 夹具,含"该判对的宽松案例"+"anti-放水陷阱(必判 WRONG)"+"边界"。近免费 golden 测试层真跑,**任一陷阱被判对 = 测试红 = 增量不通过**(SC-001)。
- **Rationale**: judge 宽松度活在 LLM 对 prompt 的解读里,纯离线断言测不出"是否放水";用带 ground-truth 标注、含对抗陷阱的小夹具真跑,是零成本量级下唯一能证伪放水的手段。陷阱是护栏本体。
- **Alternatives**: 在真实 transcript 上做 flip 审计——被否作 US1 手段:需 transcript 存活 + 判分 token + 人工审计,归 US2 量化步。

## R5 — 两层测试与 CI 边界

- **Decision**: (1) 离线确定性层进 `CGO=0` CI(零 LLM):断言 mem0-aligned 模式 prompt 含 7 条规则关键子句、`parseJudgeVerdict` 表驱动正确;(2) golden 层 env-gate(如 `LOCOMO_JUDGE_GOLDEN=1` + judge endpoint 环境变量),不进离线 CI,手动近免费跑。
- **Rationale**: 符合"引擎/harness 测试离线跑"的既有纪律(如 `TestCoverage...UsesNoAnswerOrJudgeLLM`);endpoint 依赖的检查显式 opt-in,降级不阻塞 CI(宪法 V)。
- **Alternatives**: golden 层进 CI——被否:CI 无 endpoint、且会引网络成本进门禁。

## R6 — Mem0 规则文本溯源

- **Decision**: 规则表述以本机克隆 `../mem0/evaluation/benchmarks/locomo/prompts.py:218-245`(2026-07-21 采集)为准,改写为 engram judge 的 `{"correct":bool}` 语境;不逐字复制版权文本,取其判定语义。
- **Rationale**: 语义对齐即可达成同尺;避免整段拷贝第三方 prompt 文本。
- **Alternatives**: 逐字复制——不必要,且引第三方文本入库。
