# Quickstart: 043 confidence-gated deepening

**Date**: 2026-08-15

## 0. 前置

- worktree:`.claude/worktrees/043-confidence-gated-deepen`(分支 `worktree-043-confidence-gated-deepen`)
- 本地构建门:`CGO_ENABLED=0 go build ./...` && `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`
- 模型侧实验全部在 AutoDL box 一次开机内跑(042 残余 + Step A 臂 + 本 feature 各段),跑完必关

## 1. 纯函数层(本地,零模型)

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestConfidenceDeepen' -v
# 覆盖:GapItem schema 校验、gap→query 确定性映射、追加 union 去重、阈值门、AUC 计算、
#       默认旗标关 golden(主路径逐字节一致)
```

## 2. pilot(box 第 1 段)

```bash
# env: LOCOMO_BASE_URL/MODEL/API_KEY(vllm 侧), JUDGE_* 照 042
# 硬前置(87.9% 锚 = thinking on,2026-08-15 核实):
#   LOCOMO_NO_THINKING=0   # 对照臂主通道开 thinking(代码默认 off,不设即双变量差)
#   answer vllm --max-model-len 32768   # thinking-on SSE 卡死修复
#   embed  --max-num-seqs 1             # 并发确定性(context parity 依赖)
go run ./cmd/locomo-bench --data <locomo.json> --run-dir <dir> \
  --deepen-pilot signal \
  --retrieval hybrid --chunks --chunk-quota 12 --unified-answer-contract \
  --concurrency 32
# 产物:pilot-report.json + seal;AUC>=0.65 且通道 flip_rate 在噪声带内 ⇒ GO
# NO-GO ⇒ feature 关闭,写 verdict,剩余段不跑
```

## 3. 机制配对批(box 第 2 段,GO 后)

```bash
go run ./cmd/locomo-bench --data <locomo.json> --run-dir <dir> --store-dir <store> \
  --retrieval hybrid,hybrid+unified+deepen --repeats 3 \
  --chunks --chunk-quota 12 --confidence-deepen \
  --concurrency 32
# 阈值/特征只读 pilot seal(--deepen-threshold 显式传非定稿值会被拒绝)
# 跑完:box 侧 clean 重判脚本(042/LME 先例)→ clean-*.json
# 判定:clean 3-rep majority >= 90.0% 且 avg_retrieved_items <= 60 且配对 above-noise
```

## 4. LME 迁移门(box 第 3 段)

```bash
# 同配方换 --data <lme>,零参数改动(阈值/特征/k 全部沿用)
# 判定:机制臂 clean 3-rep 不显著低于 90.2% 锚
```

## 5. 收尾

- result-matrix 配对行 + verdict 文档(含逐题翻转清单、deepen 触发率、failure_kind 分布)
- eval-config commit 与算法 commit 分开(宪法 IV)
- manifest seal 一致性自查(冻结前填满全部字段)
- box:备份小文件 → 关机(必做)
