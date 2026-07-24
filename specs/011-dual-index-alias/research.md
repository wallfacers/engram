# Research: 写入侧表示 —— dual-index alias 向量

**Phase 0** | 全部技术决策已解;无遗留 NEEDS CLARIFICATION。

## D1 — 影子向量的 name 约定与粒度

- **Decision**: 影子 name = `<源 fact name>#alias`(单一后缀);一个有 alias 的 fact 只产**一条**影子向量,其嵌入文本 = 该 fact 全部 aliases 合并、去掉 Content 已含词后的短文本。
- **Rationale**: `#` 不在 entryName 字符集(slug 为 kebab + ULID 后缀,不含 `#`),故 `#alias` 后缀与真 entry name 无碰撞、可无歧义 strip 回源。每 fact 一条(而非每 alias 一条)避免影子向量爆炸(Doc2Query++「coverage not quantity」;doc2query Appendix B 追加过多稀释原文)——aliases 合并成一句更接近「概念集合」的稠密表示。
- **Alternatives**: (a) 每 alias 一条影子向量 → 影子数膨胀、同 fact 多票需去重更复杂、稀释;(b) 新增 `memory_embeddings.kind` 列区分影子 → schema 变更,违背零改约束。均否决。

## D2 — 归并注入点与截断次序(关键)

- **Decision**: 注入点 = `vectorRankContext`(`retriever.go:821`)。改为:对 `LoadAllForModel` 全 candidates 算 cosine → **在截断前**把 `#alias` 影子的 cosine 折叠回源 fact、取 `max(text_cosine, alias_cosine)` → 再排序取 topK → `ranksFromOrder`。影子 name resolve 回源后**不进 ranks**;同源 fact 的 text/alias 双命中只留一条(去重)。
- **Rationale**: 若沿用现有 `TopKCosine` 先截断再归并,源 fact 的**低 text cosine 会在截断时被丢**,其 alias 的高 cosine 增益就拿不到——这正是 dual-index 要救的场景(text 弱、alias 强)。故必须**全集算 cosine → 归并 → 再截断**。max-pool 与 Doc2Query++ Dual-Index Fusion 的 `S_q(d)=max_j sim(q,u_j)` 一致。
- **Alternatives**: 截断后归并(丢增益,否决);α 加权融合 text/alias(引入可调参,违 tuning-free,否决——用无参数 max)。

## D3 — 影子向量的写入路径

- **Decision**: 复用 `embedOne`(`embedder.go:68`):识别 name 带 `#alias` 后缀 → strip 得源 fact name → 查 `memory_event_aliases` 取 aliases → 合并去冗成嵌入文本 → `client.Embed` → `vectors.Put(影子 name, model, vec)`。非影子 name 走原路径逐字节不变。
- **Rationale**: 最小改动、单一 embed 通道;影子在 `memory_entries` 无 row 是刻意的(它只有向量),`embedOne` 影子分支不调 `GetByName`。写入侧每有 alias fact 多一次 embed(离线、与 text 向量同 embedder)。
- **Alternatives**: 新增独立 `EmbedAliasShadow` 方法 → 重复 embed 样板;在 pipeline 里同步 embed → 阻塞写入(违 write-behind D3 惯例)。否决。

## D4 — 影子枚举与生命周期

- **Decision**: 「应有影子 names」= `memory_event_aliases` 的 distinct `entry_name` 各映射 `#alias`。写入触发:`pipeline.storeFact` 在 `PutAliases` 之后,对有 alias 的 fact `Enqueue(factName+"#alias")`。批量补齐:引擎提供影子枚举供 `Backfill`/US2 re-embed 用。孤儿影子(源 fact 被 supersede/删):检索归并时源 `GetByName` 失败即丢弃该命中(FR-007),不产悬空结果;主动清理为可选、评测 store 不删故不阻塞本 feature。
- **Rationale**: aliases 变更即重嵌入影子(与 text 向量同「内容变→重嵌」语义);枚举源单一(aliases 表)、确定。
- **Alternatives**: 用 trigger/触发器自动产影子 → 隐式、难测;不枚举仅靠 storeFact → 固化店(已抽取完)补不了影子。否决。
- **M2 核查(承 analyze)**: 影子是「有向量、`memory_entries` 无 row」的半 entry,**只应被 `vectorRankContext` semantic 归并消费**。实现时 MUST 核查其它 `memory_embeddings` 消费者不把影子当真 entry:`Backfill.NamesMissingModel`(不因影子缺 entry row 反复 enqueue 失败)、export/snapshot(不导出影子为条目)、curation(不评审影子)。凡遍历向量后 `GetByName` 的路径,遇影子 name 须 resolve/跳过。

## D7 — baseline/treatment 物理隔离(承 analyze H1,维护者裁定方案 A)

- **Decision**: 引擎 `vectorRankContext` 归并是**固有行为**(见 `#alias` 影子即 max-pool 归并),**不受 adapter flag 控制**(引擎不知道 `--alias-shadow`)。`--alias-shadow` 是 `off|baseline|treatment` enum;baseline/treatment 都先将 canonical 009 店复制到各自 `<run-dir>/alias-store`,再在副本上执行 US1 `Backfill`。baseline 随后剥离并断言 `#alias` 行为 0;treatment 断言 `#alias` 行 `>0`。**canonical 原店绝不作为运行店打开、绝不写影子。**
- **Rationale**: US1 `Backfill` 已无条件补齐应有影子;若 baseline 直接打开 canonical,即使只想复用 text embedding,也会污染原店并破坏唯一变量。两臂采用相同“复制+Backfill”路径、只在 baseline 副本末尾剥离,才能保证 canonical 纯净且唯一变量确为影子行。
- **Alternatives**: 同一物理店 + flag 控归并 → 违引擎/适配分离、污染 baseline,否决。

## D5 — 融合:max-pool + RRF,无 α

- **Decision**: semantic 通道内 text/alias cosine 取 max(无参数);信号间复用现有 `fuseRRF`(k=60)。semantic 每 fact 仍只贡献一个排名。
- **Rationale**: 守 tuning-free 可移植(engram 全局 RRF 哲学),不引入对 LoCoMo 拟合的旋钮;max-pool 是 Doc2Query++ α=1 分支的无参特例,契合「不双重计票」。
- **Alternatives**: Doc2Query++ 可调 α(0.3–0.6 峰值)→ 涨分但引入拟合参数,留作 future 且仅在证明有据时。

## D6 — 退化保真边界

- **Decision**: 无 alias 的 fact、所有 chunk → 无影子向量、semantic 逐字节不变;有 alias fact 的 **text 向量不变**(影子是额外 row,不改原向量);embedding client nil → 无任何向量、semantic 缺席、aliases 走原 keyword 通道。
- **Rationale**: 干净隔离变量(只有「有 alias fact 从 alias 角度的可发现性」改变),满足 SC-001 parity + 宪法 III/V。
- **Alternatives**: 单向量 append(改原 text 向量→稀释 bge-large,文献证掉分)——已在 brainstorm 否决。
