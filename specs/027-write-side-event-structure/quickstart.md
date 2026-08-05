# Quickstart: 写入侧事件时序结构记忆（027）

验证指南（非实现文档）。分阶段：阶段 0 零成本诊断 → 阶段 1 先导 → 阶段 2 全量配对。
实现细节见 `tasks.md`。

## 前置条件

- 本地 Go 1.25（CGO_ENABLED=0 硬门）
- 已冻结 store 资产（022/027 复用）与 LoCoMo 基线（85.19% B1 已收口，见
  `docs/evaluation/results.md`）
- 本地 LLM sidecar（vllm 7B 于 8001 或 35B 于 8000）用于 event 抽取；embedding 端点
  （bge-large 于 8010）——仅阶段 1/2 需要
- 数据/评测纪律：AutoDL run-dir 必须在数据盘 `/root/autodl-tmp/`；机器空闲必停

## 阶段 0:gold 在不在池诊断（零成本，先确诊）

**目标**:确认 temporal + multi-hop 错题里，答案片段在候选池但打包丢上下文（B 有救）
还是根本不在池（B 救不了，直接 STOP）。

```bash
# 1. 从已冻结 store 导出 temporal + multi-hop 错题清单（含 gold 答案）
# 2. 对抽样题（每 conversation ~5 条），把全对话喂给 answerer 或直接人审：
#    gold 文本是否存在于对话？是否被现有检索捞进候选？
# 3. 分类计数：gold 在池 / 不在池 / 在池但打包缺上下文
```

**判定**:「在池但打包缺上下文」占比高（预期多数）→ 进阶段 1；否则 STOP 记录负结论。

## 阶段 1:event store 先导配对（~35 分钟/次）

复用 023 的 `--only-questions` formal 子集模式。temporal + multi-hop 子集题白名单 +
event store vs chunk store 配对。

```bash
cd /root/autodl-tmp/engram   # 数据盘工作副本
RUN=/root/autodl-tmp/027-runs
IDS=$RUN/temporal-multihop-ids.txt   # 按类别过滤的题白名单（conv-N-q-M 格式）
export EMBED_BASE_URL=http://localhost:8010 EMBED_MODEL=bge-large-en-v1.5
# event 抽取的 LLM 端点（sidecar）经 env 注入
export EVENT_LLM_BASE_URL=http://localhost:8001 EVENT_LLM_MODEL=Qwen2.5-7B-Instruct

# 基线臂（chunk store，已冻结）
go run ./cmd/locomo-bench --data /root/autodl-tmp/locomo.json \
  --run-dir $RUN/baseline-chunk --retrieval both \
  --repeats 3 --only-questions $IDS
# 实验臂（event store）
go run ./cmd/locomo-bench --data /root/autodl-tmp/locomo.json \
  --run-dir $RUN/arm-event --retrieval both \
  --representation event --repeats 3 --only-questions $IDS
```

**判定**:两臂 majority 配对（McNemar），temporal + multi-hop 提升且 overall 不回归 → 阶段 2；
无转化/负收益 → STOP（008 铁律）。

## 阶段 2:全量配对（LoCoMo 1540 + LongMemEval-S 500）

阶段 1 GO 后，全量跑（一次 ~3–4h）：

```bash
# LoCoMo 全量，同 answerer/judge/预算，只变表示
go run ./cmd/locomo-bench --data /root/autodl-tmp/locomo.json \
  --run-dir $RUN/full-event --retrieval both \
  --representation event --repeats 3
# 分类别报告 + 配对统计 + token 记账；LongMemEval-S 同配方
```

**判定**:Go 门 = temporal+multi-hop 类别 ≥2.0pp 且 McNemar p<0.05 量级 + overall
non-regression + validity 全绿；负则记录 verdict、保持默认关。

## 复现/审计

- 每 run 记录 config-hash（抽取模型 + prompt 版本 + 时间锚定策略）——投影重建的依据
- 判定统计（抽取数/失败率/幻觉率/grounded 率）随 run 产物归档（`fail-closed.md`）
- 结果/verdict 落 `docs/evaluation/`（`verdicts-go-to-tracked-docs` 纪律）
