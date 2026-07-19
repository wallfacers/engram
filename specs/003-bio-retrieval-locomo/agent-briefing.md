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
