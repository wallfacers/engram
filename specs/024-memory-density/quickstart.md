# Quickstart: 024 记忆密度杠杆的实现与评测

**Branch**: `024-memory-density` | **Date**: 2026-07-31

本文件给出从引擎机制到双基准配对验证的最小执行路径。所有命令以 repo 根为 cwd，`CGO_ENABLED=0` 硬门。

## 1. 固定工作区并检查碰撞

```bash
git worktree list
# 确认 022/023 分支无并行改动；active feature 已是 024：
cat .specify/feature.json   # → specs/024-memory-density
```

## 2. 离线 TDD — 引擎机制（先写失败测试）

```bash
# 冗余抑制（Increment 0）：storeFact 前判定，默认关
# 失败测试先行：
#   - 写入两条近似事实 → 第二条投影被抑制、evidence 完整
#   - 关闭时行为与现状一致
#   - 无 embedding 端点 → 走离线 Jaccard 路径
#   - 冲突事实不被抑制
# 邻居扩展（Increment 1）：候选冻结后取共享-evidence 兄弟 fact（depth-1 有界）
#   - 命中其一 → 兄弟出现在扩展候选
#   - 无邻居 → 零变化；上限生效

CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./memory/... ./cmd/locomo-bench/...
```

## 3. 单基准方向确认 — LoCoMo 同预算四臂（F0 Gate）

在 AutoDL 上（vllm :8000 answer/extract + :8010 embedding，judge deepseek，`022.v1` 冻结协议，**固定 answer-context token cap**）：

```bash
# 机制与 eval 配置分 commit 提交（宪法 IV 归因）：
#   commit A: engine 机制代码（write_dedup / neighbor_extend 实现）
#   commit B: eval 协议绑定 + 四臂 manifest
# 四臂：关/开 write_dedup × 关/开 neighbor_extend，repeats≥3
/root/locomo-bench --eval-protocol /root/022-runs/b1-high.json \
  --run-dir /root/022-runs/024-dedup-only   --compiler-arm extractive --write-dedup \
  --data testdata/locomo/locomo.json --chunks --top-k 30 --chunk-quota 12 \
  --retrieval hybrid --force-answer --judge-mem0-aligned --concurrency 25 --repeats 3 --max-tokens 8000
# …（neighbor-only / both / none 三臂同理，protocol 各自冻结）
```

**F0 门**：任一机制相对基线不显著回归；有方向收益 → 进 Increment 3；无收益/负 → 记录负结果、保持默认关、收口（spec SC-003 允许负结果）。

## 4. 双基准四臂验证 — LongMemEval-S（Increment 3）

```bash
# 同 recipe，数据换 longmemeval_s_cleaned.json；报告双基准 overall + 分类别 + McNemar + token 记账
```

## 5. 报告与归档

- 结果写入 `docs/evaluation/`：双基准四臂明细、候选冗余下降度量（SC-001）、交互效应（SC-003）。
- 负收益机制如实归档；成功机制才讨论默认路径。
- 收尾提交后 `git diff --name-only -- mcpserver` 必须为空（不动 adapter）。

## 关键约束提醒

- **默认关**：两机制 protocol 缺省 `false`，关闭时与现状逐字节一致。
- **无付费云模型**：冗余判定主路径是离线 Jaccard（复用 `memory/curation`），embedding 仅本地 sidecar 可选。
- **不破坏 append-only**：只抑制投影创建，evidence 一行不动。
