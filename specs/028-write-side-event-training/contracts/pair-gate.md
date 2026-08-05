# Contract: 配对门禁口径（028）

**Purpose**: 冻结每阶段 GO/NO-GO 的配对口径，保证 US1（教师）、US2（训练后）与 027 基线可比。**008 铁律：端到端转化是唯一 GO 门。**

**Status**: Draft · **Version**: 0.1.0

## 配对不变项（与 027 完全一致）

| 项 | 值 |
|---|---|
| 数据集 | LoCoMo cat 1–4，`locomo.json` |
| 子集 | 84 题（`phase0-ids.txt`：temporal 59 + multi-hop 25） |
| 检索 | `--retrieval hybrid --top-k 30 --chunk-quota 12`，同 store（009-bge-chunks-store） |
| answerer | `Qwen/Qwen3.6-35B-A3B-FP8`（本地 vllm） |
| judge | `deepseek-v4-flash` + `--judge-mem0-aligned --force-answer --no-idk-retry` |
| repeats | 3（answerer temp=1.0 噪声） |
| 聚合 | per-question majority（≥2/3 对） |
| 统计 | 配对 McNemar（exact binomial） |

## 唯一可变项

| 阶段 | 抽取 LLM | 事件投影 | 指令 |
|---|---|---|---|
| 027 基线 | 7B（Qwen2.5-7B） | 无时间锚定强化 | `--build-event-project`（027 原版） |
| US1 | DeepSeek-v4-pro（教师） | 时间锚定强化 prompt | `EVENT_LLM_*` 换教师 + `--event-anchor-prompt`（若加） |
| US2 | 训练后抽取器（本地 vllm sidecar） | 训练学入锚定 | `EVENT_LLM_*` 换训练模型 |

其余全部同 027。

## GO/NO-GO 门（SC 映射）

| 阶段 | 门 | SC |
|---|---|---|
| US1 GO | 时间锚定率 5% → ≥50 绝对点 **且** event−chunk ≥ −10pp | SC-001 |
| US2 GO | 时间锚定率 ≥70% + 合法率 ≥95% + 幻觉 ≤5% + **event−chunk ≥ 0** | SC-002/003 |
| US3 GO | 默认路径零回归 + 开启配置单独口径登记 | SC-004 |
| 任何阶段 NO-GO | 记录负结论到 [experiment-verdicts](../../../docs/evaluation/experiment-verdicts.md)，不进入默认路径 | FR-006 |

## 产物（每次配对 MUST 产出）

- `pair-<stage>/run-{1,2,3}/results-hybrid.jsonl`（逐题）
- `pair-<stage>/stats.json`（分类别 mean + ci95）
- `pair-<stage>/cost.json`（token 记账）
- 本机 `pair_analysis.py` 输出（majority 配对表 + McNemar）

## 与 027 的兼容

复用 027 已建的 `cmd/locomo-bench` 配对跑法（`--build-event-project` / `--representation event` / `--only-questions`）与 `~/.claude/engram-027/pair_analysis.py`；本契约只冻结口径不变项，不新增引擎契约。
