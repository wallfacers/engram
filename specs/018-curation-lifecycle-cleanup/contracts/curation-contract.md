# Contract: Curation 配置、命令与生命周期

## 1. MCP 启动配置

| Flag | Environment | Default | Meaning |
|---|---|---:|---|
| `--curation-enabled[=true\|false]` | `ENGRAM_CURATION_ENABLED` | `false` | 为该 MCP 实例打开所有 namespace 的后台 curation |

### Precedence and validation

1. 显式 flag 覆盖环境变量。
2. 未提供两者时为 `false`。
3. 非法布尔值是启动配置错误，进程以现有 usage/config 失败路径结束。
4. `true` 要求完整可构造的 LLM caller；缺失时启动失败，不能降级为 inert。
5. API key 继续仅来自环境变量，不允许新增 secret flag。

### Observable startup state

启动 INFO 必须包含：

```text
curation=true|false
```

不得输出 provider token、API key 或完整 secret-bearing URL。

## 2. MCP 写入时序

### `memory_write`

```text
validate
→ acquire namespace
→ upsert entry
→ enqueue embedding
→ Notify curator (only when enabled; non-blocking)
→ return existing response
```

### `memory_ingest`

```text
validate
→ acquire namespace
→ extraction pipeline
→ zero or more fact writes
→ one pipeline OnWrite notification if written > 0
→ return existing response
```

通知不新增 MCP 工具，不改变任何 request/response schema。curation pass 失败不得把已经
成功的 write/ingest 改成失败。

## 3. MCP 生命周期

每个 curation-enabled namespace：

1. store/EntryStore/VectorStore/Embedder/Retriever 创建成功；
2. curator 以引擎默认配置创建；
3. extraction pipeline 的 `OnWrite` 指向同一 curator；
4. curator 用 namespace 生命周期 context 启动；
5. handle 才对 registry 调用者可见。

关闭顺序：

```text
cancel curator context
→ wait for worker loop/pass
→ drain and close embedder
→ close namespace store
```

LRU 淘汰与 Registry.Close 使用同一顺序。一次 handle 生命周期内不得启动第二个 loop。

## 4. CLI 命令

### Syntax

```text
engram --data-dir <dir> [--namespace <name>] curate
```

`curate` 不接受命令级参数。全局 LLM flag/environment 与 `ingest` 相同。

### Success output

成功完成、合法 no-op 或 fail-safe 无动作均输出确定性 Markdown：

```markdown
# curated

- namespace: <normalized namespace>
- status: completed
```

`completed` 只表示一趟已结束，不表示一定发生 merge、supersede 或 evict。

### Failure contract

| Condition | Exit | stderr contract |
|---|---:|---|
| command arguments invalid | `2` | 指出 `curate` 不接受参数并给出正确 invocation |
| LLM capability unavailable | `4` | `curate requires an LLM` + 配置下一步 |
| pass context cancelled or deadline exceeded | `1` | 明确 `curation cancelled` 或 `curation timed out` |
| store/engine cannot open | existing engine exit | 沿用现有可操作诊断 |

模型拒绝、无效 decision、单个被安全拒绝的 action 或非超时 provider error 继续遵循
worker fail-safe：不改库、WARN、命令以“pass completed”结束。

### Timing semantics

- CLI 是同步一次性：调用方等待整个 pass，正常耗时主要由本地扫描和一次 judge 调用
  决定；不承诺固定完成时长。
- 默认硬上限是 2 分钟，允许最多 5 秒清理/进程退出余量。
- `add` 与 `ingest` 不启动、通知或等待 curator。

## 5. Engine lifecycle contract

`memory/curation` 对适配器提供：

- 一份唯一、可复制的默认 `Config`，含两分钟 pass timeout；
- `Worker.Start(ctx)`：inert 安全、同一实例幂等、最多一个 loop；
- `Worker.Wait()`：inert/未启动安全，等待已启动 loop 完全退出；
- `Worker.Notify()`：inert 安全、非阻塞、pending capacity 为 1；
- `Worker.RunPass(ctx)`：一趟 fail-safe pass，遵守调用者取消和配置 deadline。
- `Worker.RunPassContext(ctx)`：供同步适配器使用；只返回 pass 实际观察到的
  cancel/deadline，已提交后到达的取消不误报失败。

适配器不得直接重写候选评分、judge、lease 或 apply 算法。

pass 返回与 `Wait` 的完成边界包含该 pass 的 heartbeat。LRU 在全局 registry 锁内只
摘除 victim；cancel/Wait/embedder drain/store close 在锁外完成。同 namespace 在旧
handle closing 期间等待，无关 namespace 可继续 Acquire/release。

## 6. Entry deletion/merge contract

### Delete(name)

成功返回前，必须原子完成：

- 删除 base entry；
- 删除 `name`、`name#alias`、`name#query` 的 vector；若 shadow key 同时是另一条
  存活 base entry 的真实 name，则按“不误删”优先保留该共享 key；
- 删除 name 的 entity、event alias、fact-query rows；
- 由现有 trigger 删除 alias FTS mirror rows；
- 清空其他 entry 中等于 name 的 `superseded_by`；
- 删除任一端点已无 entity row 引用的 entity edge。

不存在的 name 继续返回现有 not-found，不能清理任何 side data。

### Merge(names, into)

- upsert `into`；
- 对 `name != into.Name` 的 source 执行真实删除清理；
- 对存活 `into.Name` 只失效其派生 rows/vector；
- 保留指向存活 target 的有效 `superseded_by`；
- 删除每个 source/target 的 entity rows 前，只 prune 触及其 entity 且将失去端点的
  edges；无关历史孤儿不在本操作中 sweep；
- 任一步失败时回滚整个 merge。

schema v6 新增 `memory_entries.revision INTEGER NOT NULL DEFAULT 1`；不新增外键或历史
孤儿 sweep。

### Background concurrency guard

- curation Delete 必须匹配 judge 前的 `id + revision` 且当前仍非 pinned；
- curation Merge 必须在事务内匹配全部 source 和 target revision；
- curation Supersede 必须在同一原子 UPDATE 中匹配 loser 与 winner 的 `id + revision`，
  且 loser 当前仍非 pinned；
- 任一 revision 变化时对应 destructive action 整体跳过；
- write-behind embedding 只在 owner revision 仍匹配时原子 upsert vector。
- `updated_at` 只表达时间；调用方复用相同时间戳时 revision 仍必须严格增加。
