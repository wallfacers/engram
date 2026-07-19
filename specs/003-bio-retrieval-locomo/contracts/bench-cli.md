# Contract: locomo-bench CLI 扩展（feature 003）

> 原则：既有 flag 语义不变；新 flag 默认值 = 现行为。评测口径改动（prompt/记分）
> 与引擎算法改动分开提交（宪法 IV）。

## 1. 多跑与统计（US1）

```
--repeats N            # 重复次数，默认 1（现行为）；每次重复独立答题/判分，
                       # 检索与记忆库复用（噪声源=生成随机性，与检索确定性一致）
--run-dir DIR          # 既有；多跑时产出 DIR/run-1..N/results.jsonl + DIR/stats.json
--compare DIR_A DIR_B  # 配对 diff 模式：读两组 run-dir，按 question_id 对齐，
                       # 输出逐题翻转清单 + McNemar p 值 + CI 重叠判定
```

**stats.json 契约**：

```json
{
  "repeats": 5,
  "categories": {
    "multi-hop": {"mean": 0.66, "ci95": [0.61, 0.71], "n_questions": 96},
    "...": {}
  },
  "overall": {"mean": 0.747, "ci95": [0.72, 0.77]},
  "overall_comparable": {"mean": 0.726, "ci95": [0.70, 0.75]}   // 含对抗题口径
}
```

**--compare 输出契约**（stdout + compare.json）：逐题 `{question_id, category,
a_majority, b_majority, flip}`；汇总 `{flips_a_to_b, flips_b_to_a, mcnemar_p,
ci_overlap, verdict: "above-noise" | "within-noise"}`。

## 2. 机制开关（FR-011，默认全关）

```
--no-idk-retry         # 禁用两级 IDK 重试；判定口径必开（自校准基线起全程 single-pass）
--assoc                # Strike 1 联想信号
--assoc-depth 2
--temporal-score       # Strike 2 时间信号
--temporal-hard-filter
--abstain-prompt       # Strike 3 拒答 prompt 契约（仅替换答题 prompt；与 --no-idk-retry
                       #  独立，判定运行中二者组合使用）
--conflict-resolution  # Strike 3 冲突四分类（作用于建库/curation 阶段）
--superseded-penalty 0.3
```

## 2b. 同进程配对双臂（Amendment 001，Strike 0 方差诊断后新增）

> 动机：中转站对答题模型的后端质量随时间窗漂移（Strike 0 实测 run 间 ±10pp、
> IDK 率 5%~28%），跨目录 `--compare`（不同时间窗）的 verdict 被漂移淹没。
> 修正：判定比较必须在**同一次调用内**并跑 baseline 臂与 treatment 臂，
> 共享记忆库与时间窗，per-question 配对让漂移作为共同因子在 McNemar 中抵消。

```
--retrieval hybrid,hybrid+assoc   # 逗号分隔多臂；"+flag" 后缀表示该臂启用的机制开关
                                  # （assoc / temporal / conflict / abstain），
                                  # 裸开关 flag（--assoc 等）仍为全局默认，臂后缀覆盖之
```

- 各臂共用同一建库产物（机制开关只影响检索/答题侧时）；含建库侧差异的机制
  （--conflict-resolution）例外：treatment 臂单独建库，但答题/判分仍同窗交错。
- 多臂运行时 `--run-dir` 下按臂分文件（沿用现行 results-<arm>.jsonl 命名），
  stats.json 分臂输出，并直接落盘同窗 `paired.json`（契约同 compare.json，
  另加 `"paired_in_process": true` 字段）。
- 判定门：以同窗 paired.json 的 mcnemar_p 为准；跨目录 `--compare` 降级为参考。

## 2c. 反拒答口径（Amendment 001）

```
--force-answer         # answerable 口径：answer prompt 移除 "I don't know" 出口，
                       # 必给最佳猜测；与 --abstain-prompt 互斥（Strike 3 用后者）。
                       # 口径改动独立提交（宪法 IV）；自 Strike 1 起判定运行必开。
```

## 3. 数据集与口径

```
--dataset-format locomo|longmemeval   # 默认 locomo（现行为）
--adversarial                          # 既有；LME 的 abstention 题型自动按此口径记分
```

LongMemEval_S 题型→报告桶映射（D5）：single-session-user/assistant、multi-session、
temporal-reasoning、knowledge-update、preference、abstention 各自单列 + OVERALL。

## 4. 费用账（FR-014）

```
--estimate             # 只算账不跑：题数×历史均值 token×价目表 → 预估费用，退出码 0
LOCOMO_PRICE_TABLE     # env，JSON：{"gpt-5.6-sol": {"in": 1.25, "out": 10.0}, ...}
                       # 单位：USD / 1M token；缺失模型按 0 计并在报告标注 "unpriced"
```

**cost.json 契约**（每次正式跑落盘 + 报告尾部打印）：

```json
{
  "estimated_usd": 12.40,
  "actual_usd": 11.87,
  "by_role": {
    "extract":  {"calls": 350, "in_tokens": 0, "out_tokens": 0, "usd": 0},
    "answer":   {"calls": 0, "in_tokens": 0, "out_tokens": 0, "usd": 0},
    "judge":    {"calls": 0, "in_tokens": 0, "out_tokens": 0, "usd": 0},
    "embed":    {"calls": 0, "in_tokens": 0, "out_tokens": 0, "usd": 0}
  },
  "answer_context_tokens_mean": 6800   // SC-007 预算门禁监控值
}
```

## 5. 预算门禁报告（FR-013/SC-007）

每次跑的报告必须打印 `answer_context_tokens_mean` 与校准基线的比值；>1.5× 时
以醒目 WARNING 标注「涨分可能来自预算膨胀，判定无效」。

## 6. 兼容性

- 不带任何新 flag 时，行为与当前 HEAD 完全一致（含两级 IDK 重试仍在）；
  `--no-idk-retry` 与 `--abstain-prompt` 均为独立口径开关、独立提交、flag 门控。
- `run-N/results.jsonl` 逐题记录 schema 见 data-model §6；question_id 稳定可配对。
