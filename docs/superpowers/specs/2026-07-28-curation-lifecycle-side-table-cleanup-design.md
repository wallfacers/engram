# Curation 生命周期接入与 Side-table 完整清理设计

**日期**：2026-07-28  
**状态**：已确认方案，待实现  
**范围**：MCP/CLI curation 接入；记忆删除/合并时的派生数据完整清理

## 1. 背景与根因

当前 `memory/curation` 已具备候选评分、近重复聚类、LLM judge、leader lease
和后台 worker，但适配器没有装配它：

- `mcpserver.Registry` 为 namespace 创建 `pipeline.Pipeline` 时未设置 `OnWrite`，
  `memory_write` 也不通知 worker；
- CLI 是一次性进程，只装配 pipeline/embedder/retriever，没有可执行的 curation
  入口。

这不是原实现遗漏：spec 002 曾明确把“后台 curation 自动运行”排除在 MCP MVP
之外。本次是在用户明确授权下扩展产品契约。为避免升级后产生未预期的 LLM 调用、
合并或删除，自动 curation 必须保持默认关闭并显式开启。

存储孤儿数据的根因是 `deleteDerivedTx` 只覆盖最初的
`memory_embeddings` 和 `memory_entities`。后续迁移新增了
`memory_event_aliases`、`memory_fact_queries`，embedding 又新增了
`#alias`/`#query` shadow row，但删除辅助函数没有随 schema 演进。所有关联均为
逻辑关联而非数据库外键，因此 SQLite 不会代为级联。

## 2. 目标与非目标

### 目标

1. MCP 在显式开启后，为每个打开的 namespace 启动一个异步 curation worker。
2. `memory_write` 和 `memory_ingest` 成功写入后都向同 namespace worker 发出
   去抖通知。
3. namespace 被 LRU 淘汰或 Registry 关闭时，先停止并等待 worker，再关闭
   embedder 和 SQLite，杜绝后台访问已关闭数据库。
4. CLI 新增显式、同步的 `engram curate` 命令；普通 `add`/`ingest` 不增加延迟。
5. curation pass 有明确的两分钟默认上限，避免 provider/SSE 无限等待。
6. Delete/Merge 在同一事务内清理所有可归属的派生数据、shadow vector 和失效引用。
7. 默认关闭时，MCP/CLI 的现有工具契约、写入延迟和检索结果保持不变。

### 非目标

- 不新增 CLI daemon、持久任务队列或独立后台子进程。
- 不修改 curation 的候选评分、judge prompt、合并/淘汰算法。
- 不给现有 side table 追加外键 migration。
- 不自动生成 `memory_fact_queries`。
- 不在本次引入用户可调的全部 curation 水位参数；先采用来源项目已经验证的默认值。

## 3. 用户可见契约

### 3.1 MCP

新增配置：

| 入口 | 值 | 默认 |
|---|---|---|
| `--curation-enabled` | bool | `false` |
| `ENGRAM_CURATION_ENABLED` | bool | `false` |

flag 覆盖环境变量。显式开启但没有完整 LLM 配置时启动失败，并给出可操作错误；不能
出现“开关显示已开、worker 实际 inert”的假成功。

开启后的写入语义：

```text
memory_write / memory_ingest
  -> 事务性写入成功
  -> curator.Notify()（非阻塞、buffer=1 去抖）
  -> MCP 请求返回
  -> namespace worker 后台检查水位并执行 pass
```

启动日志增加 `curation=true|false`，不记录模型密钥。

### 3.2 CLI

新增一次性命令：

```text
engram --data-dir <dir> [--namespace <ns>] curate
```

命令本身就是显式授权，因此不再要求第二个 enable flag。它要求 LLM 配置，使用当前
namespace，最多同步运行两分钟。普通 `add` 和 `ingest` 不自动运行 curation，
避免把一次模型调用或全库派生索引扫描偷偷加入常用命令延迟。

`curate` 保持 curation 的 fail-safe 语义：不合法 judge 决策不会改库；命令输出只
声明“一趟已经结束”，不虚报发生了 merge/evict。能力缺失（无 LLM）返回现有
capability 类错误。

## 4. Curation 装配与生命周期

`memory/curation` 提供一份适配器可复用的默认配置：

- `EntryCountHigh = 80`
- `MinInterval = 30m`
- `LeaseTTL = 60s`
- `ManifestBudgetChars = 2000`
- `MaxCandidatesPerPass = 20`
- `ContentSnippetChars = 1200`
- weights：hit `1.0`、recency `1.0`、age `0.5`、volatility `0.5`
- budgets：`memory.DefaultBudgets()`
- `PassTimeout = 2m`

这些值来自原 workhorse-agent 的生产默认值。`PassTimeout` 是本次新增的安全边界；
它覆盖本地扫描、judge 调用和 apply 的整趟生命周期。

每个 MCP `NamespaceHandle` 持有：

- `*curation.Worker`
- namespace 级 `context.CancelFunc`

装配顺序：

1. 打开 namespace SQLite；
2. 创建 EntryStore/VectorStore/Embedder/Retriever；
3. 若显式开启，创建 Worker 并启动 namespace context；
4. 创建 Pipeline，将 `OnWrite` 设为 `Worker.Notify`；
5. 将 handle 加入 Registry。

关闭顺序：

1. cancel worker context；
2. `Worker.Wait()` 等待正在执行的 pass/heartbeat 退出；
3. `Embedder.Close()` 排空 embedding；
4. `Store.Close()`。

`Worker.Start` 变为幂等启动并受 wait group 管理。未开启时不创建 worker，
Pipeline 的 `OnWrite` 保持 nil，现有路径无额外 goroutine。

`memory_write` 是绕过 Pipeline 的直接写入路径，因此成功 `Upsert` 和 enqueue
embedding 后必须显式调用 `handle.curator.Notify()`；`memory_ingest` 由 Pipeline
统一通知，避免重复发送。

CLI 不启动后台 loop。`engram curate` 创建 worker 后直接同步调用一趟
`RunPass`，命令 context 受两分钟上限约束，结束后按现有 handle 顺序关闭资源。

## 5. Side-table 清理设计

删除与合并需要区分两种语义：

1. **条目被删除**：清理派生数据，并清除其他条目指向它的失效引用；
2. **合并目标仍存活但内容改变**：只使目标的派生索引失效，不能清除其他条目指向
   这个存活目标的 `superseded_by`。

因此把现有 `deleteDerivedTx` 拆为职责明确的事务内 helper。

### 5.1 清理一个条目的派生数据

按 entry name 删除：

- `memory_embeddings.entry_name IN (name, name#alias, name#query)`；
- `memory_entities.entry_name = name`；
- `memory_event_aliases.entry_name = name`，由现有 trigger 同步清理 alias FTS；
- `memory_fact_queries.entry_name = name`。

`memory_entries_fts` 继续由基表 DELETE/UPDATE trigger 维护，不手工操作。

### 5.2 条目真正删除时的附加清理

- `UPDATE memory_entries SET superseded_by = NULL WHERE superseded_by = deleted_name`；
- 删除实体索引后，清理任一端已经不再出现在 `memory_entities.entity_norm`
  中的 `memory_entity_edges`。

实体 edge 是跨条目累计的共享数据，不能按单条记忆直接扣减权重；只有当端点已无
任何存活 entry 引用时才整条删除。

### 5.3 Delete

在现有单事务中：

1. 删除 `memory_entries` 基表行；
2. 清理该 name 的全部派生数据；
3. 清除反向 `superseded_by`；
4. prune 无引用 entity edge；
5. commit。

任一步失败全部回滚。

### 5.4 Merge

在现有单事务中：

1. upsert 合并目标；
2. 对每个不等于目标 name 的源条目，删除基表行、全部派生数据和反向引用；
3. 对仍存活的合并目标仅清理全部派生数据，使后续 embedding/实体重建；
4. 所有源处理完后统一 prune 无引用 entity edge；
5. commit。

无关 entry、其 side rows 和仍有效的 `superseded_by` 必须保持不变。

后台 judge 与 embedding 使用 schema v6 的 `memory_entries.revision` 作为并发令牌。
revision 由数据库单调递增，不能用可能同微秒重复或由调用方提供的 `updated_at`
替代。Delete/Merge 验证全部相关 entry；Supersede 在一个原子 UPDATE 中同时验证
loser/winner，任一变化或 loser 被 pin 都跳过旧决策。

## 6. 错误与并发语义

- MCP curation 继续 fail-safe：模型/解析/单个 apply 错误记录 WARN，不使原写请求失败。
- CLI 在缺少 LLM 时直接拒绝；进入 pass 后沿用 worker 的保守 no-op 语义。
- pass timeout 通过 context 传给 provider；超时后不得继续 apply。
- leader lease 仍保证同一 namespace 数据库跨进程最多一个 curator。
- namespace handle 关闭必须发生在 refs 降为 0 后；新增的 `Wait` 保证 LRU 不关闭
  worker 正在使用的 DB。
- side-table 清理必须复用调用方事务，不引入清理完成一半的可见状态。

## 7. 测试策略

### TDD：存储完整性

先新增会失败的真实 SQLite 测试：

1. Delete 前写入正文/alias/query 三类 vector、entities、aliases、fact queries、
   superseded reverse reference 和 entity edges；
2. Delete 后断言目标相关行全部为 0、alias FTS 同步为 0、reverse reference 已清空、
   无引用 edge 已删除；
3. 同时断言无关 entry 及其所有 side rows 保持不变。

Merge 测试覆盖：

- 被消费 source 的全部 side rows 和 reverse reference 被清理；
- 存活 target 的旧派生 rows 被失效；
- target 基表仍存在；
- 指向存活 target 的 superseded reference 不被误清；
- 无关数据保持不变。

### TDD：worker 生命周期与 timeout

- `Start` 后 cancel + `Wait` 能确定性结束；
- 阻塞 caller 收到 pass deadline；
- 重复 `Start` 不产生两个 loop；
- inert worker 保持安全 no-op。

### TDD：MCP

- config 默认 false，env/flag 显式 true，非法 bool 报错；
- 开启但无 LLM 时 Registry 构造失败；
- 开启后 `memory_write` 触发后台 judge；
- `memory_ingest` 每批只触发一次通知；
- LRU 淘汰会先取消 worker，再关闭 namespace DB；
- 关闭状态下没有 curation goroutine 泄漏；
- 默认关闭的工具列表和 CRUD/parity 测试保持原样。

### TDD：CLI

- `curate` 被命令路由识别；
- 无 LLM 返回 capability error；
- mock judge 下同步完成并输出保守结果；
- 普通 `add`/`ingest` 不触发 curation；
- namespace 隔离不变。

### 完整验证

```bash
CGO_ENABLED=0 go test -count=1 ./memory ./memory/curation ./memory/pipeline
CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram ./cmd/engram-mcp
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go build ./...
go vet ./...
```

这次 storage/curation 行为变化还需执行现有确定性 retrieval parity 与 signal
degradation 测试。清理逻辑只作用于显式 Delete/Merge，默认关闭的 curation
装配不改变正常检索算法；无需为开关接线重新支付 LoCoMo 模型评测，但必须明确记录
这一归因判断。
