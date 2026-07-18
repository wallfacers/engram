# Quickstart: 记忆引擎抽离(验证与上手)

面向抽离完成后的验证者/使用者。展示如何构建、跑测试、做保真对拍、跑评测。

## 前提

- Go 1.22+
- 无需 C 工具链(纯 Go,`modernc.org/sqlite`)
- 保真对拍与单测**无需外网、无需活端点**(向量打桩)
- 评测(可选)需:LoCoMo 数据集 + 本地或私有 embedding/LLM 端点(经环境变量)
- 评测答案、判分和抽取使用 `LOCOMO_API_KEY`；embedding 使用 `EMBED_API_KEY`（可为空）

## 1. 构建(离线、纯净环境)

```bash
cd engram
go build ./...          # 引擎 + store + embedding + provider + cmd/locomo-bench
```

**期望**:构建成功,无缺失依赖,无对 workhorse 宿主包的引用(SC-001/SC-004)。

## 2. 单测平移全绿

```bash
go test ./...
```

**期望**:所有随迁的既有单测通过(entrystore/retriever/embedder/migrate/snapshot/
usagelog/export + curation + pipeline + store 的 fts5/migrations/probe + queryplan)。
通过率 100%、无测试丢失(SC-002)。

## 3. 确定性检索对拍(核心保真门禁)

```bash
go test ./memory -run TestRetrievalParity
```

**机制**:用 `testdata/parity/` 下固定语料 + 固定 query 集 + 固定/打桩向量,让 engram
`Retriever` 产出每个 query 的排序 entry ID 序列,与冻存的 workhorse 基线 golden 快照逐条比对。

**期望**:逐条一致率 100%(SC-003)。任一 query 排序不符即失败。此用例进 CI,每次改动重跑
(FR-008)。

**基线来源**:抽离前在 workhorse 侧用同一组固定输入跑 `Retriever`,导出排序序列存为 golden。

## 4. 三路信号降级验证

```bash
go test ./memory -run TestSignalDegradation
```

**期望**:语义/关键词/实体任一信号缺失或失败时,其余独立降级仍返回结果,行为与抽离前一致
(FR-009),不整体报错。

## 5. 评测工具(可选:回归 + 论文设施)

先构建工具并查看参数:

```bash
go build ./cmd/locomo-bench
go run ./cmd/locomo-bench -h
```

小子集端到端跑通需要 LoCoMo 数据集和本地或私有端点。当前工具使用
`--conversations` 与 `--questions` 控制小子集，不提供 `--limit` 参数:

```bash
export LOCOMO_API_KEY=...                       # 必填:答案、判分、抽取
export LOCOMO_BASE_URL=http://127.0.0.1:4000/anthropic
export LOCOMO_MODEL=...
export EXTRACT_MODEL=...                        # 可选，默认 LOCOMO_MODEL
export EMBED_BASE_URL=http://127.0.0.1:11434/v1
export EMBED_MODEL=...
export EMBED_API_KEY=...                        # 可选
export EMBED_RERANK_MODEL=...                   # 可选
go run ./cmd/locomo-bench \
  --data <locomo.json> \
  --run-dir ./testdata/locomo-run \
  --conversations 1 \
  --questions 2 \
  --retrieval both
```

**期望**:端到端完成并产出可比口径结果(SC-006)。全量数据集运行作**一次性 sanity check**
(SC-007),不作逐分合并门禁(FR-010)。

本轮执行环境没有 LoCoMo 数据集，也没有配置上述 API key/端点，因此 T025/T026 仅完成入口和资源说明，
未运行端到端或全量评测；不得将构建/对拍结果当作 LoCoMo 指标。

## 验收对照

| 步骤 | 对应 SC | 门禁性质 |
|------|---------|----------|
| 1 构建 | SC-001, SC-004 | 硬门禁 |
| 2 单测 | SC-002 | 硬门禁 |
| 3 检索对拍 | SC-003 | 硬门禁(CI) |
| 4 降级 | (FR-009) | 硬门禁 |
| 5 小子集 bench | SC-006 | 可运行性门禁 |
| 5 全量 bench | SC-007 | 一次性抽查,非逐分门禁 |
