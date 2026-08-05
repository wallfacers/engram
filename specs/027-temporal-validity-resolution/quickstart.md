# Quickstart: 查询时时间有效性解析

**Branch**: `027-temporal-validity-resolution` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

## 构建与单测（引擎零改动验证）

```bash
# 构建 + 全量测试（硬门禁）
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/...

# 验证引擎零改动（027 增量只在 cmd/locomo-bench/）
git diff --name-only -- memory embedding provider store internal   # 必须为空
```

## 配对消融 runbook

在 022 冻结协议下，chunk_900 对照 vs `--temporal-resolution` 臂，同 store、候选逐字节一致。

### 前置

- 022 accepted baseline 已收口：LoCoMo B1 = 85.19% majority / 84.7% stats（协议
  `sha256:263b52b6…`）。
- 需要 answerer/judge/embedding 端点（AutoDL 评测箱或本地 sidecar）。

### 环境变量

```bash
export LOCOMO_API_KEY=...      # 仅当 answerer 走 API
export LOCOMO_BASE_URL=...
export LOCOMO_MODEL=...
export JUDGE_PROVIDER=... JUDGE_BASE_URL=... JUDGE_MODEL=... JUDGE_API_KEY=...
export EMBED_BASE_URL=... EMBED_MODEL=... EMBED_API_KEY=...
```

### 配对命令（同 store，三臂只差机制 flag）

```bash
# 对照臂（control，chunk_900，无 temporal_resolution）
go run ./cmd/locomo-bench \
  --data <locomo.json> --run-dir <store> \
  --eval-protocol b1 \
  --retrieval both

# 处理臂（temporal_resolution on）
go run ./cmd/locomo-bench \
  --data <locomo.json> --run-dir <store> \
  --eval-protocol b1 \
  --retrieval both \
  --temporal-resolution
```

- **run-dir 必须位于 `/root/autodl-tmp/`**（AutoDL 系统盘仅 ~30G，勿放 `/root`；见
  docs/remote-eval-box.md）。
- 两臂必须同一 `--data` + 同一 `--run-dir`（同 store），候选经 `compileFormalSources` 逐字节
  一致，只差 `mechanism_flags{temporal_resolution}`。
- repeats≥3，对比 overall 与分类别配对统计（重点 temporal / knowledge-update / 演化类
  multi-hop）。

### 输出检查

- 协议 hash：对照 vs 处理臂 hash 必须不同（机制可归因）。
- validity 全绿、全 rate=1（026 修复后的正式协议口径）。
- 处理臂 per-question 解析 audit（mode / group_count / versions_considered / superseded_excluded
  / window_excluded / resolution_oracle）。

## 归因（US3）

temporal/knowledge-update 错题逐题分类 candidate miss / resolution miss / answerer miss；
只有 resolution-miss 占比显著时，解析机制的增益才归因到该机制（spec US3 + SC-007）。

## 负结果处理

任何解析模式相对基线负收益 → 机制保持默认关（FR-010/FR-011），verdict 记录到
`docs/evaluation/experiment-verdicts.md`。APEX-MEM 的 +14~25pp 是跨栈证据（GPT5 answerer），
engram 固定栈增益必须独立配对验证，不外推。
