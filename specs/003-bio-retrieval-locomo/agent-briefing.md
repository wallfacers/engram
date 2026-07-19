# 执行简报：feature 003 批次 1（T001–T013，测量地基 MVP）

> 本文件是发给执行 Agent 的任务简报存档。设计已冻结并经跨工件一致性分析
> （含 I1 single-pass 口径裁决），执行方只实现、不重开设计。评审收口由规划方负责。

## 批次划分

| 批次 | 任务 | 性质 | 状态 |
|------|------|------|------|
| 1 | T001–T013（Setup + Foundational + US1 + US2 代码/脚本） | 纯离线，零 API 花费 | **本批** |
| — | T014（Strike 0 评测） | 维护者付费执行 | 批次 1 review 通过后 |
| 2 | T015–T021（US3 联想检索开发） | 纯离线 | 后续简报 |
| 3+ | US4/US5 开发、各枪判定、LME_S 终态 | 依次 | 后续简报 |

## 完成定义（每任务）

`go build ./... && go vet ./... && go test ./...` 全绿 + `CGO_ENABLED=0 go build ./...`
通过 + `TestRetrievalParity` 保持绿 + tasks.md 勾选 `[x]` + 本地 commit。

## 汇报与阻塞协议

- 完成 T013 或被阻塞 → 停下输出汇报（逐任务状态/新增文件/测试输出尾部/遗留问题）。
- 设计文档矛盾或无法按契约实现 → 不改 specs/ 设计文档，记录到
  `specs/003-bio-retrieval-locomo/impl-notes.md`，跳过该任务继续无依赖任务。

---

# 执行简报：批次 2.5（Amendment 001 测量协议修正，纯离线零花费）

> 依据：Strike 0 方差诊断（specs/003-bio-retrieval-locomo/eval-log.md）。中转站
> 后端随时间窗漂移，跨目录 --compare 判定失效。契约见 contracts/bench-cli.md
> §2b/§2c。批次 2（US3）完成后立即做本批；完成后 Strike 1 才可开跑。

## 任务

- [ ] A1 `cmd/locomo-bench/main.go` `--retrieval` 支持逗号分隔多臂 + `+assoc` 等
      后缀（armsFor 扩展）；臂后缀覆盖全局机制开关；各臂共库、同窗交错答题判分
- [ ] A2 多臂运行落盘同窗 `paired.json`（契约同 compare.json + `"paired_in_process": true`）；
      stats.json 分臂输出保持现命名 results-<arm>.jsonl
- [ ] A3 `--force-answer` flag：answer/multi-hop/open-domain 三个 prompt 去掉
      "I don't know" 出口、要求必给最佳猜测；与 --abstain-prompt 互斥（启动时报错）；
      **口径改动独立 commit**（宪法 IV）
- [ ] A4 测试：armsFor 解析、后缀覆盖、paired.json 产出、互斥报错；既有测试保持绿

## 完成定义 / 汇报协议

同批次 1（go build/vet/test 全绿 + CGO_ENABLED=0 + TestRetrievalParity 绿 +
逐任务 commit）。阻塞或契约矛盾 → 记 impl-notes.md，不改 specs/ 设计文档。

---

# 执行简报：批次 2.6（US3 code review 修复，纯离线零花费）

> 依据：批次 2（81a153f + 694ccea）三路评审，规划方已逐条核实。**在批次 2.5
> 完成并汇报之后执行**（F1 与 2.5-A1 同代码区）。按编号顺序修，逐项 commit
> （`003-us3fix: F1 ...`）。每项修复必须先补一个能暴露该 bug 的失败测试。

## CRITICAL（不修则 Strike 1 判定无效）

- [ ] F1 对照臂泄漏：`cmd/locomo-bench/main.go` retrieverOptionsFor 把
      `Associative: opt.assoc` 传给所有臂，fts/baseline 臂的 entityRanks 因此切到
      整句匹配路径。若 2.5-A1 的臂后缀机制已实现按臂独立 RetrieverOptions，则只需
      验证并补测试；否则修为：无 `+assoc` 后缀的臂一律拿零值 options。
      测试：开 `--assoc` 时 baseline 臂检索结果与完全不带 flag 的运行逐字节一致。
- [ ] F2 实体匹配无词边界：`memory/entities.go` EntityMatchCountsForQuery 与
      `memory/graph.go` EntityCues 用裸子串匹配（"sam" 命中 "same"）。修为
      **规范化词元边界匹配**：entity_raw 规范化后按词元序列在 query 词元序列中
      找连续子序列；单词元实体等价于旧 token 路径。测试："did they watch the
      same movie?" 不得命中实体 "Sam"；"Alice Smith" 仍须命中。

## HIGH

- [ ] F3 游走回声：`memory/graph.go` WalkEntityGraph 无 visited 集，hop 2 沿原边
      回流种子（回声分 = w²，压过真二跳）。修：维护 visited（种子+历代 frontier），
      target ∈ visited 时跳过。测试：单边 a–b depth=2，a 不得出现在 scores。
- [ ] F4 建边不幂等：`memory/pipeline/pipeline.go` storeFact 对已存在（去重命中）
      的 entry 仍执行 UpsertEdges +1 累计，重跑/续跑图分数不可复现。修：entry
      upsert 命中已存在时跳过建边。测试：同一 fact storeFact 两次 → co 边权重 =1。
- [ ] F5 同义边语义过宽：`memory/curation/worker.go` buildSynonymEdges 把两个
      >0.8 余弦 entry 的**全部实体叉积**成 syn 边（Alice–camping 类假边、权重高于
      真 co 边）。**设计裁决（规划方）**：syn 边仅限"别名对"——x∈entry1、y∈entry2、
      x≠y 且（共享 ≥1 个规范化词元 或 一方为另一方 ≥4 字符前缀）才建边，
      weight=cosine；同时加**高水位线**（每 pass 只扫上次之后新增的 entry 与全库
      配对，不再全库 O(N²) 重扫），EntitiesOf 每 entry 只查一次（批量预取）。
- [ ] F6 重复嵌入与全表重载：`memory/retriever.go` associativeRanks 重新调
      Embed(query) 并 LoadAllForModel 全量向量表，而同一次 Search 的 vectorRanks
      已做过两者。修：Search 内算一次、两信号共享（Strike 1 嵌入调用直接减半）。

## MEDIUM

- [ ] F7 每查询三次全表扫 memory_entities（MatchCountsForQuery / EntityCues /
      EntityDocFreq）。修：cue 提取与匹配计数合并为一次扫描；EntityDocFreq 加
      WHERE 限定到游走触到的实体集。
- [ ] F8 associativeRanks 全链路吞错误无日志，违反契约 engine-api §1（"返回空并
      记日志"）。修：每个 early-return 前 slog Warn（带 stage 字段）。
- [ ] F9 `--assoc-depth` 无校验、fingerprint 记录未 clamp 值（depth=5 记入 journal
      但实际走 2）。修：flag >2 时启动报错；fingerprint 记录生效值。
- [ ] F10 测试加固：TestAssociativeNoRegression 的 fixture 余弦退化（{0.1,0,0} 与
      {1,0,0} 同向恒等），降级矩阵子测只断言 err==nil。修：fixture 用非共线向量；
      降级子测断言结果内容（其余三路信号仍在）；新增正向集成测试——仅图可达
      （bm25/向量/实体三路都够不着）的 entry 在 --assoc 下进入 top-k。

## 备注

- RetrieverOptions 的 TemporalScore/SupersededPenalty 等字段现为死字段——**保留**，
  是 US4/US5 的契约占位（engine-api §1），勿删。
- 完成定义/汇报协议同批次 1；不改 specs/ 设计文档（F5 的设计裁决已由规划方
  写入本简报，照此实现即可）。

## 批次 2.6 增补（批次 2.5 复审发现，与 F1-F10 一并执行）

- [ ] F11 **CRITICAL** `cmd/locomo-bench/runner.go` answerPromptForOptions 用
      「删除含 "i don't know" 的整行」实现 force-answer，误删了两条核心指令：
      默认 prompt 的 "Make your best supported inference … combine multiple
      memories if needed"、open-domain 的 "COMBINE the memories with common
      sense…"。修：不做行过滤，为三个 prompt 各写显式 force-answer 变体——保留
      原核心指令、仅把 IDK 出口子句替换为必答最佳猜测。测试：forced 变体必须
      仍包含 "best supported inference"/"COMBINE" 关键指令。
- [ ] F12 **CRITICAL** `cmd/locomo-bench/main.go` buildCallPlan 忽略臂数：
      AnswerCalls/JudgeCalls = Questions×repeats，配对双臂真实调用是其 2 倍，
      --estimate 低估一半（违反 FR-014）。修：×len(arms)；同时按 Strike 0 实测
      校准常量：estimateAnswerOut 300→50，estimateAnswerIn 7000→4000（含检索
      上下文余量），estimateJudgeIn 1000→1600。
- [ ] F13 MEDIUM 未实装机制后缀静默空转：`+temporal`（RetrieverOptions 死字段，
      US4 未实现）、`+conflict`（无建库侧分支）、`+abstain` 与全局
      --abstain-prompt（T035 未实现）都被接受却无行为——treatment 臂悄悄等于
      baseline。修：armsFor/flag 校验对未实装机制直接报错
      "not implemented until US4/US5"，实装时再放开。
- [ ] F14 LOW paired.json 只配对 arms[0] vs arms[1]，≥3 臂时其余臂被静默忽略
      （加 log 说明）；journal 逐题记录缺 answer regime 指纹（force_answer 等
      口径应可追溯），在 result 或 run 元数据中补记。

---

# 执行简报：批次 3（T023–T028 US4 时间结构化，纯离线零花费）

> 设计依据：tasks.md T023-T028 + **research-ammo-2026-07.md 裁决 1（R1-R4 修订）**。
> 两文件先读完再动手。设计已冻结，只实现、不重开设计。

## 任务（按序，逐任务 commit，格式 `003-us4: T023 ...`）

- [ ] T023 `memory/temporal_test.go` 可失败测试：ParseTemporalIntent 表驱动
      （绝对日期/相对时间/事件锚定/次序比较/无时间意图五类；**R2：增
      current-vs-historical 状态位断言**；R4：相对表述无锚时回退+fuzzy 标记）
- [ ] T024 `memory/retriever_test.go` 可失败测试：T_score 软加权——**R3 定式
      `exp(−α·gap)`**（gap=事件区间与查询窗口距离，重叠=0）；时间路是加权/
      补充召回，语义候选不被删除（软性断言：无时间意图时结果与关闭时一致）
- [ ] T025 实现 `memory/temporal.go`：`ParseTemporalIntent` 纯规则（无 LLM），
      输出 {窗口 start/end, 意图类别, 状态位 current|historical, 锚点实体, fuzzy}；
      R4：相对表述锚定同上下文最近绝对日期，无锚回退 session date 标 fuzzy
- [ ] T026 抽取扩展：`memory/prompt/memory_extraction.go` 增 event_start/
      event_end 字段（migration v3 列已存在）；解析失败时字段留空不报错
- [ ] T027 检索接线：`memory/retriever.go` TemporalScore 开启时——
      (a) T_score 按 R3 定式加权进融合；(b) keywordRanks union 事件别名 FTS
      （memory_event_aliases 表已建）；(c) **R3：次序题（before/after 锚点）用
      SQL 方向谓词（event_end < anchor 等）做补充召回并集，不过滤主候选**；
      (d) R4：同 timestamp 事件不推导次序。全程尊重降级矩阵：无 event 数据时
      静默回退，记日志
- [ ] T028 bench 接线：`--temporal-score`/`--temporal-hard-filter` 透传
      RetrieverOptions（hard-filter 仅实验用 flag，判定运行不用）；**放开 F13
      对 `+temporal` 臂后缀的 not-implemented 拒绝**；指纹记录生效值
- [ ] T028b **（R1 新增，口径改动独立 commit）** temporal 答题计划：
      `cmd/locomo-bench/runner.go` 增 temporal 类别专用答题 prompt 分支
      （LoCoMo category 2），固定 CoT：「列出候选记忆的 [event: YYYY-MM-DD]
      标记 → 归一化 → 比较推理 → 输出绝对日期（自然格式，禁 ISO）」；须与
      --force-answer 组合有对应变体；abstain 变体留给 T035 不做

## 完成定义 / 约束 / 汇报

同批次 1：build/vet/test 全绿 + CGO_ENABLED=0 + `go test -race ./memory/...` +
TestRetrievalParity 绿（默认关时行为与 HEAD 一致）。不改 specs/ 设计文档；
矛盾记 impl-notes.md。不动 `.locomo-run/`（**Strike 1 评测正在其中运行**）。
完成或阻塞 → 逐任务汇报（状态/commit/文件/测试尾部/遗留）。

---

## 批次 3.5：US4 评审返工（H1-H8 + S1-S3，2026-07-19 评审裁决）

批次 3 评审（三角度并行 + 本人核实）发现 1 个 blocker panic 与多个判定污染
缺陷，**批次 3 不予核销**。按序修复，逐项 commit（`003-us4: H1 ...`）。

### MUST-FIX（阻塞 Strike 2 评测）

- **H1（blocker panic）** `memory/temporal.go` ParseTemporalIntent：
  `query[orderStart:date.start]` 在 order 词位于日期之后时（中文"之前/之后"
  永远如此，英文 "on May 1 … after" 亦然）slice 越界 panic，已三例复现
  （"2023年5月7日之前发生了什么？" 等）。且偏移在 `lower` 上匹配却切原
  `query`，Unicode 下可错位。修复：日期与 order 词在**同一坐标系**匹配；
  AnchorEntity 取两位置 min..max 之间文本并 clamp 边界；补 date-then-order
  布局的回归测试（中英文各至少 2 例）。
- **H2** orderPattern `之前|以后|之前的|之后的` 缺**裸「之后」与「以前」**：
  "X之后"（最常见中文 after 形式）落入 range 分支，衰减方向反转且不触发
  方向补充召回。补全 alternation 并加方向断言测试。
- **H3** current 意图生成 `[now−1y, now]` 窗 + tau=30d：一年前陈述的仍有效
  事实得分 ~3e-6，数值上等价硬过滤，违反 R3。裁决：currentPattern/
  historicalPattern 分支**只携带 State 状态位，不设 Start/End**（无窗则
  TemporalScore 中性 1；State 留给 US5 压制 superseded 用，即 R2 本意）；
  显式近期词（recent/最近）的 30 天窗保留。
- **H4** yearPattern 裸 `\b20\d{2}\b` 无时间语境门控："reach 2048 in the
  game" 生成 2048 年窗把全部有日期记忆打到 ~0。裁决：裸年份仅在伴随时间
  语境时生效（中文"…年"已由 chineseDatePattern 覆盖；英文要求
  `(in|during|since|until|before|after|by)\s+20\d{2}`），否则不成窗。
- **H5（口径契约违反）** `cmd/locomo-bench/runner.go` answerPromptFor 按
  category==2 **无条件**分发 temporalAnswerPrompt——bench-cli 契约 §6 要求
  "不带任何新 flag 时行为与 HEAD 完全一致"，此改动使所有历史口径漂移。
  修复：新增全局答题口径 flag `--temporal-answer-prompt`（默认 off，off 时
  category-2 回到 answerSystemPrompt/forceAnswerSystemPrompt），此项独立
  commit（宪法 IV）。force 变体矩阵保持完整。
- **H6** `memory/entrystore.go` upsert 的 ON CONFLICT DO UPDATE 缺
  `event_start/event_end`：同名事实重抽取后留下陈旧区间，而 applyTemporal
  优先用区间。补全 UPDATE 列并加 upsert 回归测试。
- **H7** findAbsoluteDate 只取第一个日期："between 2022 and 2023" 塌缩为
  2022 单年窗，span 后半段被指数衰减。裁决：解析全部日期匹配，≥2 个时取
  `[min(start), max(end)]` 并集窗，intent=range。
- **H8（F13 门禁）** 旧建库产物（pre-T026 store）无 event_start/alias 数据，
  `+temporal` 臂在其上静默空转（全中性 1.0）。修复：buildConversationRuntime
  复用持久化 store 且该运行含 temporal 机制时，检测
  `event_start IS NOT NULL` 计数与 alias 表行数——facts>0 但两者皆 0 →
  启动报错要求重建库，禁止静默降级。

### SHOULD-FIX（同批次完成，不阻塞）

- **S1** TimeWindow.AnchorEntity 解析后从未被消费：directionalEventNames 只认
  AnchorTime（仅查询含显式日期时存在），主流"事件锚点"次序题（"before the
  pottery class"）拿不到方向补充召回。实装：无 AnchorTime 时按 AnchorEntity
  在库内解析锚事件日期再做方向谓词；若本批次实装成本过高，在 impl-notes.md
  记录降级并在 fingerprint 中如实标注。
- **S2** applyTemporal 对整个融合并集逐条 GetByName（200-400 次点查/题，F7
  教训）且 keywordRanks 被丢弃后在 temporalKeywordRanks 内重算。批量预取
  （EntitiesByEntry 同款模式）+ 复用共享 name 列表。
- **S3** `event_start/event_end` 无索引，directionalEventNames 全表扫描。
  加迁移建索引。

### 已知限制（记 impl-notes.md，不修）

- rerank 启用时（EMBED_RERANK_MODEL 非空）temporal 乘子会被 cross-encoder
  覆盖，且 entityNeighbors 扩展可重新引入被 hard-filter 剔除的条目。当前
  评测 env 未启用 rerank，不影响 Strike 2；记录为交互限制。
- `--abstain-prompt` 在答题路径是 no-op（批次 3 之前遗留），US5/Strike 3
  批次修复，本批不动。

### 完成定义

批次 3 同款门禁（build/vet/test/CGO=0/race/TestRetrievalParity），另加：
- TestRetrievalParity 语义扩展：默认 flag 全关时**答题 prompt 选择**也与
  批次 3 之前一致（验证 H5）。
- H1 panic 回归用例必须先红后绿（date-then-order 中英文）。
- 不动 `.locomo-run/`（Strike 1 产物在其中）；不覆盖仓库根 `./locomo-bench`
  二进制；不改 specs/ 设计文档（本简报除外的矛盾记 impl-notes.md）。
完成或阻塞 → 逐任务汇报（状态/commit/文件/测试尾部/遗留）。

---

## 批次 4：Strike 1.5 cluster-sweep（C1-C6，2026-07-19 立项）

**前置**：批次 3.5 完成核销后才可开工（两者都动 memory/retriever.go，禁止并行）。

**背景**：Strike 1 判定 --assoc 为 above-noise 负效果（-6.7pp）已回退，印证
调研裁决 2——multi-hop（基线 44.2%，最大失分洼地）的病根是**单轮 top-k 覆盖
截断**（全证据平均需 10.81 块），不是缺联想信号。本批实现唯一直接攻击覆盖
的方案：枚举意图 cluster-sweep（同思路系统 multi-hop 43.2%→85.2%）。设计
依据见 research-ammo-2026-07.md 裁决 2。

### 任务（顺序执行，逐项 commit `003-s15: C1 ...`，测试先行）

- **C1** 枚举意图检测（`memory/temporal.go` 同款纯规则，新文件
  `memory/enumintent.go`）：正则识别枚举/计数/比较意图（what things/which/
  how many/how often/all the/every time/list/哪些/几次/多少次/每次），返回
  {IsEnumeration bool}。零 LLM 调用。可失败测试先行（中英文、否定用例——
  单事实 what/when 问题不得误报）。
- **C2** 实体簇全召回（`memory/retriever.go` 或新文件）：查询实体做种子
  （复用现有实体抽取），沿 co/syn 边取一跳邻域实体簇（复用现有图结构，
  depth 固定 1，**不是**已回退的 assoc 游走），取簇内实体关联的全部 entry
  并入候选。**硬上限 cap=120 条**，超出按 fused score 降序截断并
  slog.Warn 记录截断量（No silent caps）。
- **C3** session 分组聚合：sweep 候选按来源 session 分组、组内按事件时间
  排序后格式化进答题上下文（分组标头 `[session N, YYYY-MM-DD]`），供答题
  模型逐 session 扫描聚合。非 sweep 路径格式完全不变。
- **C4** retriever 集成：新 flag 门控 `--cluster-sweep`（默认 off）。枚举
  意图命中时以 sweep 候选集**替换** top-k 截断（含 C2 cap）；意图未命中时
  路径与现状逐字节一致。TestRetrievalParity 扩展覆盖。
- **C5** bench 接线（`cmd/locomo-bench/`）：flag + `+sweep` 臂后缀加入
  supportedArmMechanisms（F13：未实装即启动报错）+ retrievalFingerprint 记录
  + 确认零新增 LLM 调用（--estimate 不变）。配对臂隔离同 F1 标准。
- **C6** 预算护栏：sweep 命中题的 answer 上下文 token 估算若超过基线均值
  （5145）的 1.5×，在该题 results 记录 `sweep_over_budget=true` 并在报告
  尾部汇总占比——判定时用于 SC-007 预算门审查，不做运行时硬阻断。

### 完成定义

批次 3.5 同款门禁（build/vet/test/CGO=0/race/TestRetrievalParity 扩展），
另加：C1 意图误报率测试（非枚举题不受影响是判定有效性的前提）。不动
`.locomo-run/`；不覆盖 `./locomo-bench` 二进制；不改 specs/ 设计文档。
完成或阻塞 → 逐任务汇报。

### 判定（我来跑，Strike 1.5）

配对双臂 `--retrieval hybrid,hybrid+sweep --no-idk-retry --force-answer`
× 5 repeats，复用 s1-store（sweep 不依赖 temporal 字段，旧库可用）。保留
标准：multi-hop above-noise 正增益、其他类别无显著回退、预算门合规。

## 批次 5：answer-plan prompt 隔离（P1-P3，harness 缺口，2026-07-20 立项）

**前置**：无。纯 `cmd/locomo-bench/` 改动，不动 `memory/`，与批次 4 已提交
代码无文件冲突，**可与其他线并行**。

**背景**：Strike 2 两臂都带 `temporal_answer_prompt=true`（`--retrieval
hybrid,hybrid+temporal --temporal-answer-prompt`），调研裁决 1 里号称最大
杠杆的"时间答题-计划 prompt"（TRACE 消融 −17.6pp）被烘进 baseline，**从未
单独量过**。要干净隔离它，需同进程配对、**唯一变量是 answer prompt**。现有
`--temporal-answer-prompt` 是全局 flag（main.go:124），两臂经 `arm := global`
一并继承（`optionsForArm` main.go:502），做不到隔离。本批把它接成 per-arm
机制，复用既有 arm 后缀 / paired.json / regime 基础设施，不另起炉灶。

**关键语义（务必据此实现与解读）**：温度答题 prompt **只在 `category == 2`
（temporal）生效**（`answerPromptForOptionsWithTemporal` runner.go:197 的
`if temporalAnswer && category == 2`）；非温度类问题两臂 prompt 逐字节一致。
故隔离效应**局限于 temporal 类目**，判定只看该类目，**不得外推到全 benchmark**。

### 任务（顺序执行，逐项 commit `003-ap: P1 ...`，测试先行）

- **P1** per-arm 机制 `tplan`：`supportedArmMechanisms`（main.go:461）加
  `"tplan"`。`optionsForArm`（main.go:502）把 `temporalAnswerPrompt` 改成
  arm 决定——override 分支（main.go:517-524 区）
  `arm.temporalAnswerPrompt = global.temporalAnswerPrompt || spec.mechanisms["tplan"]`；
  非 override 分支（main.go:507-515 区）保持继承 `global`（与既有语义一致，
  不强制清零）。**失败测试先行**，须覆盖三态：
  (a) `hybrid,hybrid+tplan` **不带**全局 flag → arm A `temporalAnswerPrompt=false`、
      arm B `=true`（这就是隔离 A/B）；
  (b) 向后兼容：`hybrid,hybrid+temporal` + 全局 `--temporal-answer-prompt` →
      两臂皆 true（Strike 2 精确可复现，不得改变）；
  (c) `hybrid+tplan+temporal` 组合合法（tplan 与其他机制正交，检索仍走 temporal）。
- **P2** paired/regime 固化：确认 `answerRegimeFingerprint`（main.go:749）已
  按 per-arm 记录 `temporal_answer_prompt`（main.go:751），JSONL 逐题
  `answer_regime` 用 `armOpt`（main.go:818）能区分两臂——加测试固化。
  `checkRunDirRegime`（run-dir 级 pin，main.go:730 用全局 opt）**不得**因两臂
  regime 不同而误拒；per-arm 差异体现在 JSONL，不在 run-dir 级。验证
  `validatePromptModes` 对 `hybrid+tplan` 臂通过（tplan 与 force-answer 不互斥，
  与 abstain 的互斥关系照旧）。
- **P3** estimate/文档：确认 `--estimate` 不变（零新增 LLM 调用，prompt 仅换
  文本，token 量近似）。impl-notes 记入上述"仅作用于 category 2、判定不得外推"
  的解读口径。

### 完成定义

批次 4 同款门禁（build/vet/test/CGO=0/race/TestRetrievalParity——tplan 未命中
温度类时路径逐字节不变），另加 P1(b) 向后兼容测试（Strike 2 flag 组合可复现）。
不动 `memory/`、不动 `.locomo-run/`、不覆盖 `./locomo-bench` 二进制、不改
specs/ 设计文档。完成或阻塞 → 逐任务汇报。

### 判定（我来跑，answer-plan 隔离）

配对双臂 `--retrieval hybrid,hybrid+tplan --no-idk-retry --force-answer`
（**不带**全局 `--temporal-answer-prompt`，否则 arm A 也会被污染成 true）×
5 repeats，复用 s2-store。**只看 temporal 类目配对差异**（prompt 只作用于
cat 2）——这即 answer-plan prompt 在 sol 后端上对温度题的净贡献。成立门槛：
temporal 类目 **≥+3pp 且配对 McNemar 显著**；低于此则调研的"大杠杆"在本后端
不成立，如实记录。

## 批次 4.5：cluster-sweep 评审修复（X1-X4，2026-07-20 立项）

**前置**：批次 4（C1-C6）已提交。本批修其评审缺陷，**必须先落地才可跑 Strike
1.5 评测**——否则评的是 no-op（见 X1）。动 `memory/retriever.go`、
`memory/enumintent.go`、`memory/graph.go`，与批次 5（纯 bench）无冲突可并行。

**背景**：cluster-sweep 代码评审 VERDICT=FIX，5 条中 4 条需修（F4 观测型护栏
符合 C6 规格，不改）。核验记录见下，逐条测试先行。

### 任务（顺序执行，逐项 commit `003-s15fix: X1 ...`，测试先行）

- **X1（阻塞）top-k 截断吃掉 sweep 召回**：`retriever.go:189` 无条件
  `if len(fused) > k { fused = fused[:k] }`，把 sweep 的最多 `clusterSweepCap`
  (120) 条又砍回 `k`(默认 30)——`clusterSweepUsed=true` 但只有 30 条进上下文，
  **全簇召回当场失效，功能变 no-op**。修：sweep 生效时把有效截断上限抬到
  `clusterSweepCap`。落点——引入 `kEff := k; if clusterSweepUsed { kEff = clusterSweepCap }`，
  第 189 行改用 `kEff`。**注意 reranker 分支**（`retriever.go:186-187`
  `r.rerank(ctx, query, fused, k)`）也传了 `k`，若 rerank 内部按 k 截断会二次
  砍回——同步改用 `kEff`。**失败测试先行**：枚举意图命中、簇大小 >k 时，
  `Search` 返回条数 = `min(簇大小, clusterSweepCap)` 而非 `k`（无 reranker 与
  有 reranker 两种路径都要断言）。
- **X2（精度）枚举意图误判**：`enumintent.go:15` 裸 `\blist\b` 命中名词
  "list price"、`多少(?:...)?` 后缀可选致裸 `多少` 命中"多少钱"，把单事实题
  误判为枚举 → 被 sweep 替换成宽泛簇上下文而答错。修：`list` 要求列举语境
  （如 `\blist\s+(?:of|all|the|out|every)\b` 或前置动词），`多少` 计数后缀改为
  **必需**（去掉 `?`）或显式排除 `钱|价|钱数`。**否定测试先行**："What is the
  list price…"、"多少钱" 必须 `IsEnumeration=false`；既有正例（"哪些/几次/how
  many/list of"）不得回归。
- **X3（鲁棒）UNION 兜底替换**：`retriever.go:181-184` 当前 `fused = swept` 纯
  替换——真答案若不在查询实体簇内会被整个丢弃（X2 收紧后误判虽降，仍有实体
  解析错的残余风险）。改为**有界 UNION**：保留原 `fused` 的高排名项（如前
  `k/2` 条）作为语义保底，与 sweep 候选按 key 去重合并，合并集整体受
  `clusterSweepCap` 约束后再走 X1 的 `kEff` 截断。测试：真答案排原 fused #1
  但不在簇内时，UNION 后仍在结果中。
- **X4（潜伏）SQLite 变量上限**：`graph.go:335` `IN (?,?,…)` 每实体一占位符，
  稠密 hub 一跳邻域实体数可爆 modernc 变量上限 → 查询 error → sweep 静默自禁。
  修：进 `IN` 前对 entitySet 设上限（如 200，与变量上限留余量），超出按
  `entityScores` 降序截断并 `slog.Warn` 记截断量（No silent caps）；或分批查询
  合并。测试：实体数 > 上限时不 error、正常返回截断后结果。

### 完成定义

批次 4 同款门禁（build/vet/test/CGO=0/race/TestRetrievalParity——非枚举/未命中
路径逐字节不变），另加 X1（截断条数）、X2（误判否定用例）、X3（UNION 保底）、
X4（超限不崩）的新回归测试。不动 `.locomo-run/`、不覆盖二进制、不改 specs/
设计文档。完成或阻塞 → 逐任务汇报。

### 判定（我来跑，Strike 1.5，X1-X4 落地后）

配对双臂 `--retrieval hybrid,hybrid+sweep --no-idk-retry --force-answer` ×
5 repeats，复用 s1-store。**成立门槛（已收紧，防 ±1pp 自我安慰）**：multi-hop
/枚举类目 **≥+8pp 且配对 McNemar 显著**，其他类别无显著回退，预算门合规。
低于门槛按"诚实回退"撤 `--cluster-sweep`。

## 批次 6：枚举召回微修 + 类目过滤（Y1-Y2，2026-07-20 立项）

**前置**：批次 4.5 已提交（X1-X4 复审：X1/X3/X4 通过，X2 矫枉过正需 Y1 修）。
两任务文件不同（`memory/enumintent.go` vs `cmd/locomo-bench/main.go`），可并行。

**背景**：(1) Strike 2 判定 `--temporal-score` within-noise 已回退；temporal
答题 prompt 的贡献两臂共有未隔离，交批次 5 `+tplan` 隔离测——但那个 prompt
只作用于 category 2（321 题），跑全量 1540 题纯烧钱，需类目过滤把成本压到
~1/5（Y2）。(2) 批次 4.5 的 X2 修枚举误判时矫枉过正，把真枚举挡掉了（Y1）。

### 任务（顺序执行，逐项 commit `003-y: Y1 ...`，测试先行）

- **Y1（枚举召回微修）**：批次 4.5 的 X2 过窄，实测假阴性（RE2 无 lookahead
  约束下重新平衡）：
  - 英文 `list`：允许接续扩到代词/物主 —— 现 `list\s+(?:of|all|the|out|every)`
    改为 `list\s+(?:of|all|the|out|every|your|their|his|her|its|our|my|them|us|down)`。
    找回 "Can you list your hobbies?"、"list them"，仍挡 "list price"（price
    不在集内）。
  - 中文 `多少`：后缀恢复**可选** `多少(?:个|次|种|项|件|人|地方)?`——找回
    "多少国家/多少本书"（国家/本 本就不在固定表内的真计数问题）。恢复后
    "多少钱" 的假阳性由已落地的 X3 UNION 兜底（精确命中在 k/2 floor 里不丢），
    对枚举-召回型功能而言假阴性比被兜底的假阳性更该避免。
  - **测试先行**：正例 "Can you list your hobbies?"、"list them"、"多少国家"
    → IsEnumeration=true；既有正例（how many/all the/哪些/几次/多少次）不回归；
    "List all the places…"/"what activities" 仍 true。
- **Y2（类目过滤 flag）**：加 `--only-category N`（默认 0=全部；N=只保留该
  category 的问题）。落点 `selectQuestions`（main.go）——在现有 maxQuestions/
  adversarial 选择逻辑之上按 `qa.Category == N` 过滤（N>0 时；adversarial 题
  是否纳入按现有 adversarial flag 逻辑，不受 only-category 影响或显式说明）。
  `buildCallPlan`/estimate 必须随之缩减（否则 --estimate 报的是全量、误导预算
  判断）。**测试先行**：`--only-category 2` 时 selectQuestions 只返回 cat 2 题，
  buildCallPlan 的 AnswerCalls 相应缩减。

### 完成定义

批次 4.5 同款门禁（build/vet/test/CGO=0/race/TestRetrievalParity），另加
Y1 召回正例、Y2 过滤与 call-plan 缩减测试。不动 `.locomo-run/`、不覆盖二进制、
不改 specs/ 设计文档。完成或阻塞 → 逐任务汇报。

### 判定（我来跑）

- **answer-plan 隔离（省钱版）**：`--retrieval hybrid,hybrid+tplan --no-idk-retry
  --force-answer --only-category 2 --repeats 5`，复用 s2-store（prompt 差异
  不依赖建库，旧库可用）。只看 temporal 类目配对差异。门槛：≥+3pp 且配对
  显著。预计 ~1/5 全量成本。
- Y1 落地后，cluster-sweep（Strike 1.5）按批次 4.5 判定条款全量跑。

## 批次 7：Y1 修正 + 枚举过滤 + evidence 日志（Z1-Z3，2026-07-20 立项）

**背景（方向已调整）**：两次仿生 strike 判负，路线改为"低成本验证假设 +
evidence 可观测 + pilot 门控再决定烧不烧全量"。cluster-sweep **不直接跑
category-1 全量**（离线统计：cat-1 命中 51/282，其中 33 错，要整类 +8pp 需净救
~23 道≈修复 70%，概率低）——先跑 51 道命中题的 pilot（名义 ~¥5，实付 ~¥2-3），
过 pilot 门槛才跑全量。本批交付 pilot 所需的过滤与诊断。

### 任务（顺序执行，逐项 commit `003-z: Z1 ...`，测试先行）

- **Z1（Y1 修正）**：批次 6 的 Y1 把"多少钱"重新误判成 enumeration 不合理
  （X3 UNION 只保原 top-15、仍会灌入至多 120 条噪声并换 prompt）。修
  `memory/enumintent.go`：**保留**新增的英文 `list your/their/...`（那部分合理），
  但中文 `多少` 必须**排除金额类**。RE2 无 lookahead → 用消费式否定字符类：
  `多少[^钱价金费]`（"多少钱/价格/金额/费用" 的首字 钱/价/金/费 被排除；
  "多少国家/多少本/多少次/多少人/多少？" 仍命中）。**测试先行**：
  "多少钱""多少价格""多少费用""多少金额" → IsEnumeration=**false**；
  "多少国家""多少本书""多少次""你收集了多少？" → true；英文 list your/them 与
  既有正例不回归。
- **Z2（枚举过滤 flag）**：加 `--only-enumeration`（默认 off）。开启时
  `selectQuestions` 只保留 `memory.ParseEnumerationIntent(question).IsEnumeration
  == true` 的题（与 `--only-category 1` 叠加 → cat-1 命中子集，约 51 道）。
  `buildCallPlan`/estimate 随之缩减。**测试先行**：`--only-category 1
  --only-enumeration` 只返回 cat-1 且枚举命中的题；call-plan 缩减。
- **Z3（evidence 诊断日志）**：每题 results 记录新增诊断字段（sweep 命中时必记，
  非 sweep 路径可留空）——(a) LoCoMo gold evidence（数据集 QA 的 evidence 字段，
  映射到 session/dialog id；先确认 locomo10.json 的 evidence schema）、
  (b) 检索到的 memory names、(c) 检索到的 source session ids、(d) sweep 前/后
  候选数、(e) evidence session recall = |gold evidence session ∩ 检索 session| /
  |gold evidence session|、(f) answer context token 数（已有则复用）。目的：
  分数不涨时能区分"实体簇没覆盖证据"vs"覆盖了但 120 条上下文干扰答题"。
  **测试**：构造一题断言 recall 计算正确、字段落盘。

### 完成定义

批次 6 同款门禁 + Z1 否定用例 / Z2 过滤缩减 / Z3 recall 计算测试。不动
`.locomo-run/`、不覆盖根目录二进制、不改 specs/ 设计文档。逐任务汇报。

### 判定（我来跑，sweep pilot，Z1-Z3 落地后）

`--retrieval hybrid,hybrid+sweep --only-category 1 --only-enumeration
--repeats 5 --force-answer --no-idk-retry`，复用 s1-store，新 run-dir。
**pilot 门槛**：命中子集 ≥+8pp 且 McNemar 显著、sweep_over_budget 占比可接受、
baseline 正确题未被大量搞错。过 pilot 才跑 cat-1 全量（282 道）；不过直接回退
`--cluster-sweep`，不烧全量。
