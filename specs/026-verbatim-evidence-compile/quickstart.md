# Quickstart: 查询期 verbatim 证据编译

**Branch**: `026-verbatim-evidence-compile` | **Date**: 2026-08-01

## 目标

5 分钟内跑通:compiler arms 的离线 byte-replay(无 LLM/embedding)+ 配对消融的进入条件。

## 0. 前置

```bash
# 引擎测试全绿(022 承接资产)
CGO_ENABLED=0 go test -count=1 ./memory/evidencecompiler/...
# 全仓
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/
```

## 1. 离线 arms byte-replay(无模型依赖)

```bash
# 026 新增的 arms(extractive / verbatim-first)测试
CGO_ENABLED=0 go test -count=1 -run "CompileArm|VerbatimFirst|Extractive" ./cmd/locomo-bench/
```

断言:
- 同一 query + 同一候选池 → 每 arm 输出逐字节一致(deterministic)。
- verbatim-first:原文装得下 → bundle 含原始 span;装不下 → EXTRACT/MERGE 且逐句绑 source。
- fail-closed:无来源 ADD 拒绝、无效 citation 丢弃、退回 extractive。

## 2. 配对消融(正式,需 AutoDL 或本地 LLM 端点)

```bash
# 需 LOCOMO_*/EMBED_*/JUDGE_* 端点;protocol 复用 022 frozen manifest
go run ./cmd/locomo-bench --data <locomo.json> --run-dir ./.locomo-run \
  --eval-protocol <022-b1-manifest.json> --compile-arm <legacy_count|exact_token|extractive|verbatim_first>
```

配对纪律(025 教训):
- 两臂**同一 store**,候选逐字节一致(只差 arm)。
- repeats ≥ 3,对比 overall + 分类别配对统计,重点 multi-hop / temporal。
- 报告 candidate oracle(gold 是否在池)区分 compiler miss 与 candidate miss。

## 3. 退出条件

- 任一 arm 负收益 → 记录 verdict,默认关,不进默认路径。
- verbatim-first 若同预算下相对 chunk_900 有可测 multi-hop/temporal 增益 → 评估进默认路径(需双基准共同过门)。
