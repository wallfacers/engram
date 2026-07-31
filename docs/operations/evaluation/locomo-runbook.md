---
title: LoCoMo 评测运行手册
summary: 本文提供当前 LoCoMo 端到端评测的可复现 recipe、验证步骤和故障边界；不将单次运行结果作为产品能力结论。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [locomo-runbook]
tags: [operations, evaluation, locomo, runbook]
---

# LoCoMo 评测运行手册

本手册用于复现已登记的 LoCoMo recipe。分数以[当前评测结果](../../evaluation/results.md)为准，实验取舍以[实验裁决](../../evaluation/experiment-verdicts.md)为准。

## 前置条件

将 answer/extract、embedding 与 judge 的配置分离到非追踪环境文件。所有凭据、远端地址和令牌只能通过运行环境传入，不能写入仓库、日志或产物。运行前确认数据版本、固化 store、answerer、embedder、judge 和并发限制。

## Canonical recipe

```bash
source ~/.config/engram/locomo-vllm.env
source ~/.config/engram/judge.env
export EMBED_BASE_URL=http://127.0.0.1:8010/v1
export EMBED_MODEL=BAAI/bge-large-en-v1.5
export EMBED_API_KEY=local-eval

locomo-bench \
  --data testdata/locomo/locomo.json \
  --store-dir <frozen-store> \
  --chunks --top-k 30 --chunk-quota 12 \
  --retrieval hybrid --force-answer --judge-mem0-aligned \
  --concurrency 40 --run-dir <run-dir>
```

`--chunks`、`--chunk-quota 12`、`--force-answer` 和 `--judge-mem0-aligned` 是该 recipe 的必要标志。变更任何一项都必须作为新 recipe 登记，不得与现有基线混合。

远端 vLLM 的 embedding endpoint 监听 8010，且 `EMBED_MODEL` 必须与其
`/v1/models` 返回的 served model ID 完全一致；使用短名会让 semantic signal
静默降级，不能作为可比较评测。

## 运行后验证

首先检查 `<run-dir>/regime.json`：它必须记录 `force_answer=true`、`judge=mem0-aligned` 与实际 judge 模型。再检查失败调用、context token 分布、store embedding 维度和结果文件完整性。`cost.json` 中的零价格不等于没有调用 judge，必须结合 regime 与日志判断。

## 常见故障

- 分数显著偏低时，先核对 aligned judge、chunk quota、正确 store 和 embedding 维度。
- 远端模型冷启动后的首个 arm 只能作为 warm-up；同配置复跑才是对照锚点。
- judge 凭据失效可使题目被记为错误；长跑前先执行小型 warm-up。
- 离线 embedding 服务需要显式离线环境变量，避免启动时访问外网。

远端机器的生命周期与成本纪律见[远端 GPU 评测运维](remote-gpu-runbook.md)。
