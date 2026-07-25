# 外部 codex agent 启动提示词（015 离线固结）

**前置条件（硬）**: 以下所有提示词 **仅在 Phase 1 证伪门（B0）判决通过后才可启动**。
B0 判死 ⇒ 本特性作废，一个 agent 都不要启动。B0 由维护者会话直接执行，不外包。

**批次与并行度**:

| 批次 | 提示词 | 并行度 | 前置 |
|---|---|---|---|
| B1 | §1 | 1 | B0 通过 |
| B2 | §2.1 / §2.2 / §2.3 | **3** | B1 完成 |
| B3 | §3.1 / §3.2 | **2** | B2 对应测试完成 |
| B4 | §4 | 1 | B3 完成 |
| B5 | §5 | 1 | B4 完成 |

---

## 0. 所有 agent 共用的前言（每段提示词都已内嵌，此处仅说明）

每个 agent 都是独立上下文，不知道设计讨论。因此每段提示词都必须自带：
必读文件、只准碰的文件白名单、硬禁止项、验证命令、完成标准。

**最重要的一条**：每个 agent **只准修改自己白名单里的文件**。这是 B2/B3 三路
并行不冲突的唯一保证。

---

## 1. B1 — v6 migration（1 个 agent，阻塞项）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。这是一个纯 Go（CGO_ENABLED=0
硬门禁）的本地优先记忆引擎，存储用 modernc.org/sqlite。

【必读】按顺序读完再动手：
- specs/015-consolidation-bridging/data-model.md（§1 是你要实现的 DDL 原文）
- specs/015-consolidation-bridging/tasks.md（你负责 T010、T011、T012）
- store/migrations.go（理解 migrationsByVersion 的既有结构）

【任务】T010 → T011 → T012，严格按此顺序（测试先行）：

T010: 在 store/migrations_test.go 追加 v6 migration 测试，先写，且必须先失败。断言：
  - 迁移后 memory_bridges 表存在，四个列齐全（entry_name/source_a/source_b/pair_key/created_at）
  - idx_memory_bridges_pair 唯一索引存在，且插入重复 pair_key 会被拒绝
  - idx_memory_bridges_source_a / idx_memory_bridges_source_b 存在
  - down 迁移后表与三个索引都被干净移除
  参照该文件中既有 migration 测试的写法与断言风格。

T011: 在 store/migrations.go 追加 v6ConsolidationBridges 与 v6ConsolidationBridgesDown
  两个常量，并在 migrationsByVersion 末尾追加 {Version: 6, Up: ..., Down: ...}。
  DDL 必须与 data-model.md §1 逐字一致。

T012: 验证 T010 转绿。

【只准修改这两个文件】
- store/migrations.go（只准追加，见下方禁止项）
- store/migrations_test.go（只准追加）

【硬禁止】
- 绝对不许修改 v1..v5 任何一行。已发布的 migration 是不可变的，只能新增版本。
- 不许修改上面白名单之外的任何文件。
- 不许引入任何新的第三方依赖。本任务零新依赖。
- 不许用 CGO。

【验证，必须全绿才算完成】
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./store/

【完成标准】
两条命令零错误；git diff --name-only 只显示 store/migrations.go 与
store/migrations_test.go 这两个文件。把 git diff --name-only 的输出贴出来自证。
```

---

## 2. B2 — 三路并行（3 个 agent 同时启动）

### 2.1 Agent A — 候选枚举测试（T013）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。纯 Go，CGO_ENABLED=0 硬门禁。

【必读】
- specs/015-consolidation-bridging/data-model.md（§2.1 CandidatePair 不变式、§4 枚举查询）
- specs/015-consolidation-bridging/contracts/consolidation-api.md（§1 类型、§3 EnumerateCandidates 签名）
- specs/015-consolidation-bridging/tasks.md（你负责 T013）
- memory/curation/scorer_test.go（借鉴既有测试的建库/建表风格）

【任务】T013：新建 memory/consolidation/candidates_test.go，写候选枚举的测试。
这些测试现在必然编译失败（实现还不存在），这是预期的——测试先行。
必须覆盖：
1. 确定性：同一输入两次调用产出完全相同的序列；Score 相同时按 (A,B) 字典序升序
2. 跨 session 过滤：同一 source_session_id 的两条 entry 不得出现在候选中
3. 共享实体必需：没有共享实体的两条 entry 不得出现在候选中
4. IDF 打分：共享稀有实体（低 df）的对，得分必须高于共享常见实体（高 df）的对
5. top-K 截断：候选数超过 Config.TopKPerPass 时只保留得分最高的前 K 个
6. MaxBucketSize：某个 entity_norm 关联的 entry 数超过上限时，整个桶被跳过
7. 二阶禁止：已存在于 memory_bridges 表中的 entry 不得进入候选
8. PairKey() 与枚举顺序无关：(a,b) 与 (b,a) 产出同一个 key

全部离线、零模型调用。用真实的内存 SQLite（modernc.org/sqlite）建表灌数据。

【只准新建/修改这一个文件】
- memory/consolidation/candidates_test.go

【硬禁止】
- 不许创建或修改 memory/consolidation/ 下的任何其他文件（另有 agent 在并行写它们）
- 不许修改 memory/、store/、embedding/、provider/ 下任何既有文件
- 不许引入新的第三方依赖
- 不许为了让测试通过而放宽断言

【验证】
CGO_ENABLED=0 go vet ./memory/consolidation/ 2>&1 | head
（此时因缺实现而报错是正常的。请确认报错只是"undefined: EnumerateCandidates"这类
缺符号错误，而不是你自己代码的语法错误。）

【完成标准】
测试文件写完，8 项覆盖齐全；git diff --name-only 只显示这一个文件。贴出来自证。
```

### 2.2 Agent B — 裁决解析与校验测试（T014）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。纯 Go，CGO_ENABLED=0 硬门禁。

【必读】
- specs/015-consolidation-bridging/data-model.md（§2.2 BridgeVerdict 与全部校验规则）
- specs/015-consolidation-bridging/contracts/consolidation-api.md（§1 类型、§3 ParseVerdict/ValidateVerdict 签名）
- specs/015-consolidation-bridging/tasks.md（你负责 T014）
- memory/curation/judge_test.go（借鉴既有解析测试的写法）

【任务】T014：新建 memory/consolidation/verdict_test.go。测试现在必然编译失败
（实现还不存在），这是预期的——测试先行。必须覆盖：

ParseVerdict（注意：它不返回 error）：
1. 正常输出 → Bridged=true，Content/SourceA/SourceB 正确解析
2. 显式 NONE 记号 → Bridged=false，且不返回错误
3. 完全不可解析的垃圾输出 → Bridged=false，且不返回错误
   （关键：模型说"没有"是正常路径，不是故障。绝不能把它当错误处理。）

ValidateVerdict（返回 error，nil 表示可落库）：
4. Bridged=false → 拒绝
5. Content 为空或纯空白 → 拒绝
6. SourceA 或 SourceB 在库中不存在（悬空引用）→ 拒绝
7. SourceA/SourceB 与传入的 CandidatePair 不一致 → 拒绝
8. Content 去掉空白后与任一源 entry 的内容等价 → 拒绝（冗余，非新增信息）
9. 全部合法 → 返回 nil

【只准新建/修改这一个文件】
- memory/consolidation/verdict_test.go

【硬禁止】
- 不许创建或修改 memory/consolidation/ 下的任何其他文件（另有 agent 在并行写它们）
- 不许修改 memory/、store/、embedding/、provider/ 下任何既有文件
- 不许引入新的第三方依赖

【验证】
CGO_ENABLED=0 go vet ./memory/consolidation/ 2>&1 | head
（因缺实现报"undefined"是正常的；确认不是你自己的语法错误。）

【完成标准】
9 项覆盖齐全；git diff --name-only 只显示这一个文件。贴出来自证。
```

### 2.3 Agent C — 固结提示词（T015）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。纯 Go，CGO_ENABLED=0 硬门禁。

【必读】
- specs/015-consolidation-bridging/contracts/consolidation-api.md（§5 提示词契约要求）
- specs/015-consolidation-bridging/data-model.md（§2.2 BridgeVerdict —— 你的输出格式必须能被它解析）
- specs/015-consolidation-bridging/tasks.md（你负责 T015）
- memory/prompt/ 下既有的抽取/策展提示词（必须沿用同样的组织方式与命名风格）

【背景】这个提示词用于"离线固结"：给模型两条来自不同对话 session 的记忆，问它
两者之间是否存在一条可推出的、非冗余的桥接事实。例如给
  A: "他提到打算搬去柏林"
  B: "他的新工作在一家德国公司"
应产出类似 "他因为德国公司的新工作而搬去柏林" 的桥接事实。

【任务】T015：新建 memory/prompt/consolidation.go，定义固结提示词常量。契约要求：
1. 系统提示必须显式授予模型拒绝权，并规定拒绝时的确切输出记号（如单独一行 NONE）。
   这是精度阀门——绝大多数候选对之间并不存在真实连接，模型必须能廉价地说"没有"，
   而不是被迫编造。这一条最重要。
2. 输出格式必须要求模型回述两个源的标识，供程序侧校验（防止模型引用不存在的源）。
3. 必须明确要求"非冗余"：桥接内容不得只是复述任一条源事实。
4. 必须要求桥接事实简短、单句、只陈述可从两源推出的内容，不得引入外部知识或猜测。
5. 输出格式必须是确定性可解析的（供 ParseVerdict 解析），不要求 JSON 也可，但必须
   规整、无歧义。

【只准新建/修改这一个文件】
- memory/prompt/consolidation.go

【硬禁止】
- 不许修改 memory/prompt/ 下任何既有文件
- 不许创建或修改 memory/consolidation/ 下任何文件（另有 agent 在并行写）
- 不许引入新的第三方依赖

【验证】
CGO_ENABLED=0 go build ./memory/prompt/
CGO_ENABLED=0 go vet ./memory/prompt/

【完成标准】
两条命令零错误；git diff --name-only 只显示这一个文件。把提示词全文与
git diff --name-only 输出一起贴出来自证。
```

---

## 3. B3 — 两路并行（2 个 agent）

### 3.1 Agent A — 候选枚举实现（T016）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。纯 Go，CGO_ENABLED=0 硬门禁。

【必读】
- memory/consolidation/candidates_test.go（已由前一批写好，这是你的验收标准）
- specs/015-consolidation-bridging/data-model.md（§2.1 不变式、§4 枚举 SQL 与分桶算法）
- specs/015-consolidation-bridging/contracts/consolidation-api.md（§1 Config/CandidatePair、§3 签名）
- memory/curation/worker.go（借鉴"后台 worker 持 *sql.DB 句柄"的既有模式）

【任务】T016：新建 memory/consolidation/candidates.go，实现 CandidatePair、
PairKey()、EnumerateCandidates，以及 Config 中它用到的字段，让 candidates_test.go
全部转绿。

算法严格按 data-model.md §4：
- 一条 JOIN 查询取全量 (entity_norm, entry_name, source_session_id)，
  用 LEFT JOIN memory_bridges 排除已是桥接产物的 entry
- 内存中按 entity_norm 分桶；桶大小超过 Config.MaxBucketSize 则整桶跳过
- 桶内两两组合，过滤掉 source_session_id 相同的对
- IDF = log(N / df)，df 即桶大小，N 为 entry 总数；同一对的得分是其全部共享实体的 IDF 之和
- 全局排序：Score 降序，同分按 (A, B) 字典序升序
- 取前 Config.TopKPerPass 个

【只准新建/修改这一个文件】
- memory/consolidation/candidates.go

【硬禁止】
- 不许修改 candidates_test.go。测试是验收标准，不是可以改的东西。
  如果你认为某条测试断言写错了，停下来说明理由，不要自行修改。
- 不许创建或修改 memory/consolidation/ 下其他文件（另有 agent 在并行写 verdict.go）
- 不许修改 memory/、store/ 下任何既有文件。特别地：不许在 memory 包新增公开方法，
  本任务的所有 DB 访问都在 consolidation 包内用 *sql.DB 直接完成。
- 不许引入新的第三方依赖

【验证，必须全绿】
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 -run 'Candidate|Enumerate|PairKey' ./memory/consolidation/

【完成标准】
测试全绿；git diff --name-only 只显示 candidates.go 这一个新文件。贴出来自证。
```

### 3.2 Agent B — 裁决解析与校验实现（T017）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。纯 Go，CGO_ENABLED=0 硬门禁。

【必读】
- memory/consolidation/verdict_test.go（已由前一批写好，这是你的验收标准）
- memory/prompt/consolidation.go（已由前一批写好，你的解析必须匹配它规定的输出格式）
- specs/015-consolidation-bridging/data-model.md（§2.2 全部校验规则）
- specs/015-consolidation-bridging/contracts/consolidation-api.md（§1 BridgeVerdict、§3 签名）

【任务】T017：新建 memory/consolidation/verdict.go，实现 BridgeVerdict、
ParseVerdict、ValidateVerdict，让 verdict_test.go 全部转绿。

关键语义（不要搞错）：
- ParseVerdict 不返回 error。模型输出 NONE 或输出无法解析，都返回 Bridged=false。
  "模型说没有"是正常路径，不是故障。
- ValidateVerdict 返回 error，nil 表示可以落库。它施加 data-model.md §2.2 的
  全部五条拒绝规则。

【只准新建/修改这一个文件】
- memory/consolidation/verdict.go

【硬禁止】
- 不许修改 verdict_test.go。如果你认为某条断言写错了，停下来说明理由，不要自行修改。
- 不许创建或修改 memory/consolidation/ 下其他文件（另有 agent 在并行写 candidates.go）
- 不许修改 memory/prompt/consolidation.go —— 你要适配它，不是改它
- 不许修改 memory/、store/ 下任何既有文件
- 不许引入新的第三方依赖

【验证，必须全绿】
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 -run 'Verdict|Parse|Validate' ./memory/consolidation/

【完成标准】
测试全绿；git diff --name-only 只显示 verdict.go 这一个新文件。贴出来自证。
```

---

## 4. B4 — worker 编排（1 个 agent，串行）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。纯 Go，CGO_ENABLED=0 硬门禁。

【必读】
- specs/015-consolidation-bridging/contracts/consolidation-api.md（§1/§2 完整 API 契约）
- specs/015-consolidation-bridging/data-model.md（§3 状态流转图，这是编排的骨架）
- specs/015-consolidation-bridging/research.md（R2 lease 语义、R3 落库序列、R4 实体并集）
- memory/consolidation/candidates.go 与 verdict.go（已完成，你要编排它们）
- memory/curation/worker.go（你要照它的模子写：RunPass/heartbeat/inert）
- memory/pipeline/pipeline.go 第 160-190 行（落库序列的原样出处）

【任务】T018 → T019 → T020 → T021，严格按此顺序（测试先行）。

T018: 新建 memory/consolidation/worker_test.go，先写测试且必须先失败。模型侧用
stub 闭包（ModelCaller 是函数类型，直接传假函数）。必须覆盖：
  - NONE 拒绝闸：stub 返回 NONE → 落库数为 0，memory_bridges 无残留
  - 悬空引用闸：stub 返回不存在的源 → 拒绝落库并告警，整趟继续不中断
  - ADD-only：pass 前后逐条比对所有源 entry 的内容与总条数，必须完全不变
  - 幂等：连续跑两趟 RunPass，memory_bridges 行数不变、无重复 entry
  - inert：call == nil 时 RunPass 零副作用（无新 entry、无新血缘、无 error、不 panic）
  - 无 embedder 降级：embedder == nil 时产物仍落库，不 panic
  - 单对失败不中断整趟：让 stub 对某一对返回 error，其余对仍被正常处理

T019: 实现 memory/consolidation/worker.go 的 Config、ModelCaller、PassStats、
  NewWorker、RunPass。编排顺序严格按 data-model.md §3 状态流转图。
  落库序列严格照 research.md R3（照抄 pipeline.go 的既有路径）：
      entries.Upsert → entries.PutEntities → entries.UpsertEdges → embedder.Enqueue
  产物实体取两条源记忆实体的并集（research.md R4）。
  最后 INSERT OR IGNORE INTO memory_bridges。
  所有失败一律 fail-safe：记 WARN 日志 + 跳过该对 + 继续整趟。RunPass 永不 panic、
  永不向外传播 error。

T020: 用 curation.NewLease(db) 接入领导租约与 heartbeat。注意：这把 lease 是
  id=1 的单例行锁，固结与策展因此必然互斥、不能同时跑 —— 这是预期语义不是 bug，
  因为存储层是 SetMaxOpenConns(1) 单写连接，并发只会互相阻塞。不要试图"修复"它。

T021: 验证。

【只准新建/修改这两个文件】
- memory/consolidation/worker.go
- memory/consolidation/worker_test.go

【硬禁止】
- 不许修改 candidates.go / verdict.go / prompt/consolidation.go（它们已完成并通过测试）
- 不许修改 memory/、store/、embedding/、provider/ 下任何既有文件
- 不许新建独立的 lease 表，必须复用 curation 的
- 不许引入新的第三方依赖

【验证，必须全绿】
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./memory/consolidation/ ./memory/prompt/ ./store/

【完成标准】
全绿；git diff --name-only 只显示上述两个文件。贴出来自证。
```

---

## 5. B5 — 后台作业（1 个 agent，串行）

```text
你在 Go 项目 /home/wallfacers/project/engram 上工作。纯 Go，CGO_ENABLED=0 硬门禁。

【必读】
- specs/015-consolidation-bridging/contracts/consolidation-api.md（§2 Notify/Start 契约）
- specs/015-consolidation-bridging/tasks.md（你负责 T022–T024）
- memory/consolidation/worker.go（已完成，你在它上面加后台循环）
- memory/curation/worker.go 的 run / shouldRun / heartbeat / Notify（照这个模子写）

【任务】T022 → T023 → T024，测试先行。

T022: 在 memory/consolidation/worker_test.go 追加测试，先写且必须先失败：
  - 两个 worker 指向同一个 DB，只有一个真正执行（另一个安静让出，不报错）
  - 中断后重跑只补未完成部分（幂等已保证，此处验证实际行为）
  - TopKPerPass 上限生效：候选超限时单趟只处理前 K 个
  - EntryCountLow 水位线：非桥接记忆数低于该值时不执行，且不产生告警噪声
  - 任何失败被捕获，既有检索与写入能力完全不受影响

T023: 实现 Notify（非阻塞去抖，buffered(1) 的 trigger channel，已有待处理唤醒时
  直接吸收）、Start（后台循环直到 ctx 取消）、水位线判定、单趟上限。
  照 curation.Worker 的 run/shouldRun/heartbeat 写法。
  inert worker（call == nil）上 Notify 与 Start 都必须是安全空操作。

T024: 验证。

【只准修改这一个文件】
- memory/consolidation/worker.go 及其测试文件 memory/consolidation/worker_test.go

【硬禁止】
- 不许修改 candidates.go / verdict.go / prompt/consolidation.go
- 不许修改 memory/、store/ 下任何既有文件
- 不许引入新的第三方依赖

【验证，必须全绿】
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...

【完成标准】
全量测试全绿；git diff --name-only 只显示上述文件。贴出来自证。
```

---

## 6. 维护者收口（不外包）

B5 完成后由维护者会话执行，**不交给外部 agent**：

- **T025** 全量测试 `CGO_ENABLED=0 go test -count=1 ./...`（含 parity 与 namespace
  isolation 硬门）—— 独立复跑，不采信 agent 的报告
- **T026** 引擎表面零改动核验：`git diff --name-only` 确认引擎既有文件中只有
  `store/migrations.go` 被改动且仅为追加；`memory/retriever.go`、`mcpserver/`、
  `cmd/locomo-bench/` 零改动
- **T027** 全量 LoCoMo 回归门（宪法 IV，需独占 box）
- **T028** 结论归档，eval 配置改动与算法改动分开提交

**评审要点**（按 CLAUDE.md「验证，不要轻信报告」）：
1. 独立复跑测试，不采信 agent 贴的结果
2. 检查测试是否真的在断言，而不是自比为真的同义反复
3. 确认没有 agent 越界改了白名单外的文件
4. 确认没人为了让测试通过而放宽了断言
