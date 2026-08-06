# Quickstart: Agentic 多步记忆导航

**Date**: 2026-08-06 · **Spec**: [spec.md](spec.md) · 契约: [navigation-tools.md](contracts/navigation-tools.md) · [navigation-trajectory.md](contracts/navigation-trajectory.md)

## 前置条件

复用 027/028 评测栈（本机 / AutoDL）：

- **answerer**: vllm sidecar `Qwen/Qwen3.6-35B-A3B-FP8 @ :8000`（Blackwell env，`serve-ans80g.sh`）
- **embed**: `BAAI/bge-large-en-v1.5 @ :8010`（`serve-emb80.sh`）
- **judge**: DeepSeek（`source ~/.config/engram/judge.env`，env-only）
- **store**: `009-bge-chunks-store`（chunk store）+ `locomo.json` + `phase0-ids.txt`（84 题子集）
- **bin**: `locomo-bench`（CGO=0 构建）→ 编译后上传远端

```bash
CGO_ENABLED=0 go build ./cmd/locomo-bench
```

## US1 零成本诊断（救回空间）

纯本地分析（不需要 answerer/judge；hybrid 检索需本地 embedding sidecar）。真实混合检索在 harness 内完成（Python 无法跨语言调引擎），诊断分两步：

```bash
# ① harness 产出逐题检索诊断（真实检索：in_pool / 单次 top-30 gold rank / wide-pool rank / 模拟动作）
locomo-bench --data locomo.json --store-dir 009-bge-chunks-store --chunks \
  --retrieval hybrid --only-questions phase0-ids.txt --top-k 30 \
  --nav-diagnose --run-dir .locomo-nav-diagnose

# ② 离线分类 + 归因 + 报告
python specs/029-agentic-memory-navigation/tools/nav_diagnose.py \
  --hits-jsonl .locomo-nav-diagnose/nav-diagnose.jsonl \
  --questions /root/autodl-tmp/027-runs/phase0-ids.txt \
  --out diagnosis-report.json
```

env（仅 embedding）：`EMBED_BASE_URL=http://localhost:8010/v1` · `EMBED_MODEL=BAAI/bge-large-en-v1.5` · `LOCOMO_API_KEY=dummy`

**产出**: 三分类计数 `{in_pool, topk_hit, rescueable, not_in_pool}` + rescueable 归因分布（rewrite 换查询 / follow_entity 跟线索 / deep 换粒度）+ 抽样。

**判定**: `rescueable 占比 ≥ 20%` → 进 US2；否则 STOP 记录负结论。

## US2 最小先导（多步导航配对）

单次基线（chunk 现有路径）vs 多步导航（新增 `--nav`），同 store/子集/answerer/judge：

```bash
# 基线（单次检索，现有路径）
locomo-bench --data locomo.json --store-dir 009-bge-chunks-store --chunks \
  --only-questions phase0-ids.txt --retrieval hybrid --repeats 3 \
  --top-k 30 --chunk-quota 12 --force-answer --no-idk-retry --judge-mem0-aligned \
  --run-dir pair-base

# 多步导航（新增 --nav，复用同一 store/配置）
locomo-bench --data locomo.json --store-dir 009-bge-chunks-store --chunks \
  --only-questions phase0-ids.txt --retrieval hybrid --repeats 3 \
  --top-k 30 --chunk-quota 12 --force-answer --no-idk-retry --judge-mem0-aligned \
  --nav --nav-max-steps 4 --nav-k 8 --run-dir pair-nav

# 配对分析（离线）
python specs/029-agentic-memory-navigation/tools/nav_analyze.py \
  --base-dir pair-base --nav-dir pair-nav --arm hybrid --repeats 3 \
  --nav-trajectories pair-nav/nav-trajectories.jsonl \
  --out us2-pair-report.json
```

env（两臂一致）：`LOCOMO_MODEL=Qwen/Qwen3.6-35B-A3B-FP8` · `LOCOMO_BASE_URL=http://localhost:8000/v1` · `EMBED_BASE_URL=http://localhost:8010/v1` · `EMBED_MODEL=BAAI/bge-large-en-v1.5` · `JUDGE_*`（source judge.env）· `LOCOMO_API_KEY=dummy`

**产出**: 两臂 majority + McNemar + 类别明细 + 轨迹 token 记账（步数分布/fallback 率/超 cap 计数）。

**判定（008 铁律）**: 多步 majority ≥ 单次基线 → GO；否则 STOP 记录（负结果可接受）。

## 期望结果（基于证据先验）

- **US1**: 受 023 residual（gold 中位 rank 71-90）与 199/200 gold 在池的先验，预期 `rescueable` 有可观占比（深层可救）；若 <20% 则本线直接 STOP。
- **US2**: MemCog/NapMem 消融指示「推理介入检索」在低 context 预算下收益最明显——engram 预算内先导是其在本地口径的首次实测；GO 门严格（008 铁律），不达标如实记录。

## 门禁与提交

- 评测配置变更（步数上限/token cap/repeats）与算法改动**分开 commit**（宪法 IV 归因）。
- 每阶段 verdict 落 `docs/evaluation/`（tracked docs），不只进本地记忆（028 教训）。
- 引擎零改动验证：`git diff --name-only -- memory embedding provider store internal` 必须为空（FR-003）。
