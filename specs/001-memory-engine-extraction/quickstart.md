# Quickstart: 记忆引擎抽离(验证与上手)

面向抽离完成后的验证者/使用者。展示如何构建、跑测试、做保真对拍、跑评测。

## 前提

- Go 1.22+
- 无需 C 工具链(纯 Go,`modernc.org/sqlite`)
- 保真对拍与单测**无需外网、无需活端点**(向量打桩)
- 评测(可选)需:LoCoMo 数据集 + 本地 embedding/LLM 端点(经环境变量)

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

小子集端到端跑通(需本地端点):

```bash
export EMBED_BASE_URL=http://localhost:11434   # 本地 embedding sidecar
export EMBED_MODEL=...                          # 见 cmd/locomo-bench 说明
export BASE_URL=... ; export _API_KEY=...       # 本地/私有 LLM 端点
go run ./cmd/locomo-bench --dataset <locomo.json> --limit <小子集> ...
```

**期望**:端到端完成并产出可比口径结果(SC-006)。全量数据集运行作**一次性 sanity check**
(SC-007),不作逐分合并门禁(FR-010)。

## 验收对照

| 步骤 | 对应 SC | 门禁性质 |
|------|---------|----------|
| 1 构建 | SC-001, SC-004 | 硬门禁 |
| 2 单测 | SC-002 | 硬门禁 |
| 3 检索对拍 | SC-003 | 硬门禁(CI) |
| 4 降级 | (FR-009) | 硬门禁 |
| 5 小子集 bench | SC-006 | 可运行性门禁 |
| 5 全量 bench | SC-007 | 一次性抽查,非逐分门禁 |
