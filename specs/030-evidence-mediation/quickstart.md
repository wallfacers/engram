# Quickstart: 读侧证据装配结构（Evidence Mediation）

验证场景按 US 分级，全部可独立运行。契约详情见 [contracts/](contracts/)，实体见 [data-model.md](data-model.md)。

## 前置

```bash
# 引擎零改动硬门（FR-001）
CGO_ENABLED=0 go build ./...
git diff --name-only -- memory embedding provider store internal   # 必须为空

# 全包测试绿
CGO_ENABLED=0 go test -count=1 ./...
```

## US1 证据装配地基（离线，零模型）

**验证点**: token 精确记账 / chunk 原文优先 / 类别条件结构 / 默认关 parity。

```bash
# 1. 离线单测（stub tokenizer/provider，无网络）——装配记账/保底/类别/parity
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestAssembly|TestChunkFraction|TestCategoryStructure|TestAssemblyParity'

# 2. 全量装配诊断（纯 Go，零模型成本）——chunk_fraction/token 记账审计
go run ./cmd/locomo-bench --store-dir <store> --run-dir ./.locomo-030-us1 \
  --chunks --evidence-assembly --assembly-diagnose
python specs/030-evidence-mediation/tools/assembly_diagnose.py ./.locomo-030-us1
```

**预期**:
- `assembly_diagnose.py` 输出 `chunk_fraction` 中位 ≥ 阈值（相对 029 的 ~1% 硬修复）+ `tokens_estimated` 分布（应全部 `false`，vllmTokenCounter 在线）。
- 默认（不加 `--evidence-assembly`）与现有路径 parity（`git diff` 无行为变化，SC-004）。

## US2 引用链证据中介（配对，008 铁律）

**验证点**: sidecar 生成 trace（opt-in）→ fail-closed 门 → e2e majority ≥ 基线。

```bash
# 1. 离线单测（stub provider）——非法 ID 丢弃 / 解析失败重试 / 闭包 / parity
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestTraceGate|TestTraceFailClosed|TestTraceParity'

# 2. 配对：基线 vs 中介 arm，84 题 × 3 reps（复用 029 whitelist）
#    基线
go run ./cmd/locomo-bench --store-dir <store> --run-dir ./.locomo-030-us2/base \
  --only-questions specs/029-agentic-memory-navigation/diagnosis/phase0-ids.txt --repeats 3
#    中介 arm（sidecar env: ENGRAM_TRACE_MODEL 等，走 029 式 harness HTTP caller）
go run ./cmd/locomo-bench --store-dir <store> --run-dir ./.locomo-030-us2/trace \
  --only-questions specs/029-agentic-memory-navigation/diagnosis/phase0-ids.txt --repeats 3 \
  --trace-mediation
go run ./cmd/locomo-bench --compare ./.locomo-030-us2/base ./.locomo-030-us2/trace
python specs/030-evidence-mediation/tools/trace_analyze.py ./.locomo-030-us2
```

**预期**: `--compare` 输出 majority + McNemar；`trace_analyze.py` 报告门状态分布（valid/invalid_citation/parse_failed/fallback）。**GO 门 = majority ≥ 基线且类别不回归（L0-3）**；否则 NO-GO 记录负结论，US3 不执行。

## US3 条件压缩（配对，P2）

**验证点**: 预算内不压缩（默认 parity）/ 超预算 opt-in 压缩 ≤ cap / 压缩 arm 不显著回退。

```bash
# 1. 离线单测——默认关 parity / 超预算触发 / replaced_unit_ids 记录
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestConsolidate|TestConsolidateParity'

# 2. 配对：保留 vs 压缩 arm（同 84 题）
go run ./cmd/locomo-bench --store-dir <store> --run-dir ./.locomo-030-us3/keep \
  --only-questions specs/029-agentic-memory-navigation/diagnosis/phase0-ids.txt --repeats 3 \
  --evidence-assembly
go run ./cmd/locomo-bench --store-dir <store> --run-dir ./.locomo-030-us3/cons \
  --only-questions specs/029-agentic-memory-navigation/diagnosis/phase0-ids.txt --repeats 3 \
  --evidence-assembly --consolidate
go run ./cmd/locomo-bench --compare ./.locomo-030-us3/keep ./.locomo-030-us3/cons
python specs/030-evidence-mediation/tools/consolidation_analyze.py ./.locomo-030-us3
```

**预期**: `consolidation_analyze.py` 报告预算交叉（超预算题占比 / 压缩后 ≤ cap）；压缩 arm e2e 不显著回退；超预算子集上重演 Retain or Consolidate 的交叉或诚实报告负结论。

## 评测纪律

- 评测配置变更（flag/env）与算法改动分开 commit（宪法 IV 归因）。
- 每个 arm 同 store/子集/answerer/judge/预算（008 配对纪律）。
- verdict 落 `docs/evaluation/` tracked + `specs/030/diagnosis/`（verdicts-go-to-tracked-docs）。
- 引擎零改动硬门：每次 commit 前 `git diff --name-only -- memory embedding provider store internal` 为空。
