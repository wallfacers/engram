# Quickstart Validation: Curation 生命周期与记忆索引完整性

本指南验证用户可见契约，不包含完整测试实现。所有命令从仓库根目录执行。

## Verification Record

| Date | Stage | Command | Result |
|---|---|---|---|
| 2026-07-28 | pre-implementation baseline | `CGO_ENABLED=0 go test -count=1 ./...` | PASS，14 个 test packages 通过，1 个 package 无测试 |
| 2026-07-28 | storage integrity | `CGO_ENABLED=0 go test -count=1 ./memory` | PASS |
| 2026-07-28 | worker lifecycle | `CGO_ENABLED=0 go test -count=1 ./memory/curation ./memory/pipeline` | PASS |
| 2026-07-28 | MCP adapters | `CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram-mcp` | PASS |
| 2026-07-28 | CLI adapter | `CGO_ENABLED=0 go test -count=1 ./cmd/engram` | PASS |
| 2026-07-28 | full regression | `CGO_ENABLED=0 go test -count=1 ./...` | PASS，14 个 test packages 通过，1 个 package 无测试 |
| 2026-07-28 | build | `CGO_ENABLED=0 go build ./...` | PASS |
| 2026-07-28 | static analysis | `CGO_ENABLED=0 go vet ./...` | PASS |
| 2026-07-28 | deterministic retrieval gates | `CGO_ENABLED=0 go test -count=1 ./memory -run 'Parity\|Degrad\|Retriev'` and `./mcpserver -run 'Parity\|Isolation\|Offline'` | PASS |
| 2026-07-28 | offline benchmark package | `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` | PASS |
| 2026-07-28 | review concurrency stress | new revision/late-embed/heartbeat/LRU/CLI boundary tests at 10–20 repeats | PASS |
| 2026-07-28 | 100 full namespace lifecycles | `TestEnabledRegistryCompletes100OpenStartEvictCloseLifecycles` | PASS |
| 2026-07-28 | monotonic revision remediation | v6 migration, same-timestamp delete/merge/vector and both-endpoint supersede tests at 50 repeats | PASS |
| 2026-07-28 | post-revision full regression | `CGO_ENABLED=0 go test -count=1 ./...` + build + vet + deterministic retrieval/MCP/LoCoMo package gates | PASS，14 个 test packages 通过，1 个 package 无测试 |
| 2026-07-28 | independent post-revision review | Critical/Important-only re-review | PASS，Critical 0，Important 0 |
| 2026-07-28 | canonical LoCoMo paid merge gate | 1540 questions × 3，Qwen + BAAI/bge-large + DeepSeek Flash，canonical flags | PASS，多数票 86.10% vs 85.71%，McNemar p=0.585，within-noise；估算总费用约 ¥4.45 < ¥16 |

基线检查同时确认 sibling `longmemeval-lossless-chunks` worktree 为 clean，且没有触及
`memory/entrystore.go`、`memory/curation/worker.go`、`mcpserver/` 或 `cmd/engram/`。

## 1. Prerequisites

- Go 1.25+
- `CGO_ENABLED=0`
- 可选：一个本地 OpenAI-compatible 或 Anthropic-compatible LLM sidecar，用于手工
  验证 curation；单元/集成测试不需要网络。

为避免写入真实用户数据，手工运行时使用显式临时目录：

```bash
ENGRAM_VALIDATION_DIR="$(mktemp -d)"
export ENGRAM_VALIDATION_DIR
```

## 2. Contract and focused tests

```bash
CGO_ENABLED=0 go test -count=1 ./memory ./memory/curation ./memory/pipeline
CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram ./cmd/engram-mcp
```

期望：

- Delete/Merge 的 base、alias、query、entity、edge 与 supersession 测试全绿。
- Worker cancel + Wait、重复 Start、pass timeout 测试全绿。
- MCP 默认关闭、显式开启、write/ingest 通知和 LRU/Close 顺序测试全绿。
- CLI `curate` 能力、同步完成、超时和普通命令零触发测试全绿。

## 3. Verify default-off compatibility

```bash
env -u ENGRAM_CURATION_ENABLED \
  CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram
```

期望：

- 现有 MCP 工具清单不变。
- 没有 LLM 时 CRUD/search 继续离线可用。
- 普通 CLI `add` 与 `ingest` 没有 curation 调用。

## 4. Validate CLI one-shot curation manually

先写入测试记忆：

```bash
go run ./cmd/engram --data-dir "$ENGRAM_VALIDATION_DIR" \
  add --name duplicate-a --content "The user likes jasmine tea."
go run ./cmd/engram --data-dir "$ENGRAM_VALIDATION_DIR" \
  add --name duplicate-b --content "The user prefers jasmine tea."
```

配置本地 LLM sidecar（示例值按本机替换）：

```bash
export ENGRAM_LLM_PROVIDER=openai
export ENGRAM_LLM_BASE_URL=http://127.0.0.1:11434/v1
export ENGRAM_LLM_MODEL=local-model
```

执行一趟：

```bash
go run ./cmd/engram --data-dir "$ENGRAM_VALIDATION_DIR" curate
```

期望 stdout：

```markdown
# curated

- namespace: default
- status: completed
```

该命令同步等待；正常耗时取决于本地扫描和 sidecar，一旦超过两分钟必须失败并停止迟到
apply。输出只证明 pass 结束，使用 `list` 检查模型是否选择了实际动作。

缺少 LLM 配置时：

```bash
env -u ENGRAM_LLM_PROVIDER -u ENGRAM_LLM_BASE_URL -u ENGRAM_LLM_MODEL \
  go run ./cmd/engram --data-dir "$ENGRAM_VALIDATION_DIR" curate
```

期望非零 capability 状态及 `curate requires an LLM`。

## 5. Validate MCP opt-in configuration

默认关闭的配置解析：

```bash
CGO_ENABLED=0 go test -count=1 ./mcpserver -run 'Curation|Config'
```

手工启动本地 MCP：

```bash
ENGRAM_CURATION_ENABLED=true \
ENGRAM_LLM_PROVIDER=openai \
ENGRAM_LLM_BASE_URL=http://127.0.0.1:11434/v1 \
ENGRAM_LLM_MODEL=local-model \
go run ./cmd/engram-mcp --data-dir "$ENGRAM_VALIDATION_DIR"
```

期望启动日志包含 `curation=true`，但不含密钥。`memory_write`/`memory_ingest` 的响应
不等待后台 pass；后台错误只记 WARN。未配置 LLM 却设置
`ENGRAM_CURATION_ENABLED=true` 时必须启动失败。

## 6. Full offline regression gates

```bash
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go build ./...
go vet ./...
```

并确认：

```bash
CGO_ENABLED=0 go test -count=1 ./memory -run 'Parity|Degrad|Retriev'
CGO_ENABLED=0 go test -count=1 ./mcpserver -run 'Parity|Isolation|Offline'
```

默认关闭且检索算法未变时，以上确定性 parity 是无分数回退的直接前置证据，但不替代
宪法 IV。本功能已在显式 ¥16 成本授权下完成 canonical LoCoMo 1540×3：多数票
86.10% vs 85.71% reference，McNemar p=0.585（within-noise），没有显著回退。
详细 recipe、逐次分数、调用量、费用和产物路径见 `plan.md` 的 Evaluation Gate Status。

Go race detector 强制要求 CGO，`CGO_ENABLED=0 go test -race` 会直接报
`go: -race requires cgo`，与本项目无 CGO 硬门冲突。因此本 feature 使用 100 次
重复 Start/Acquire/write、200 次 LRU 压力和确定性 cancel/Wait 测试覆盖并发生命周期，
不把无法在目标构建模式执行的 race detector 伪装成已通过门禁。

## 7. Cleanup

验证目录不含真实数据时可自行删除：

```bash
rm -rf -- "$ENGRAM_VALIDATION_DIR"
```
