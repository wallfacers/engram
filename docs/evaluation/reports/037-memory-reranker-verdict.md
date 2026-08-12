# 037 Verdict: 记忆专用重排模型训练（Memory-Specific Reranker Training）

**日期**: 2026-08-12 · **口径**: LoCoMo 1540 全量 × 1 rep（US1/US2 各自 run 内配对）· **模型**: deepseek-v4-pro（answer/extract/rewrite）+ v4-flash（judge），api.deepseek.com/anthropic · **检索**: hybrid（bge-small-en-v1.5 embedding + keyword + entity），top-k 30，无 --chunks · **rerank**: Qwen3-Reranker-0.6B base（US1）vs bce-infonce merged（US2），transformers 服务（`tools/server.py`，score equation 冻结）· **store**: 每 run 独立 extraction（v4-pro）

## 结论

**NO-GO（008 铁律，run 内配对为准）：记忆专用重排训练（0.6B LoRA, bce-infonce 9450×3ep）未转化为端到端增益。** run 内配对 US2 merged rerank 增量 **−1.1pp**（hybrid 59.5% → rerank 58.4%），US1 base rerank 增量 **−0.4pp** —— rerank（现成或训练后）在 LoCoMo 上第 3 次证伪 e2e 转化（008 bge-v2-m3 → US1 Qwen3-base → US2 trained-merged）。SC-005 opt-in sidecar 决策不触发；**cross-encoder 永不进本地默认栈**（死亡规则）。

**唯一正向信号：temporal +1.6pp**（70.7→72.3，+5 净翻转；US1 base 为 0.0pp）——temporal-hard negatives 训练确实让 rerank 在时间域生效。但被 single-hop（−1.6pp）/ open-domain（−7.3pp）拖累，总体仍负，且为单次观测。

## 配对结果（008 铁律，run 内 arm-to-arm：同 store/answerer/judge/检索）

### US1（base rerank，reports/us1-paired.md）

| 臂 | OVERALL | single-hop | multi-hop | temporal | open-domain |
|---|---|---|---|---|---|
| hybrid | 68.1% | 67.2% | 64.2% | 76.3% | 60.4% |
| hybrid+rerank | 67.7% | 67.7% | 59.9% | 76.3% | 61.5% |
| Δ | **−0.4pp** | +0.5pp | −4.3pp | 0.0pp | +1.0pp |

flips a2b 62 / b2a 69（净 rerank 害 7 题）；multi-hop 被害（−12 题）。

### US2（trained merged rerank，reports/us2-paired.md）

| 臂 | OVERALL | single-hop | multi-hop | temporal | open-domain |
|---|---|---|---|---|---|
| hybrid | 59.5% | 56.7% | 58.9% | 70.7% | 47.9% |
| hybrid+rerank | 58.4% | 55.1% | 58.5% | 72.3% | 40.6% |
| Δ | **−1.1pp** | −1.6pp | −0.4pp | **+1.6pp** ⭐ | −7.3pp |

flips a2b 69 / b2a 86（净 rerank 害 17 题）；temporal +5 净翻转（15/10）。

## 机制归因

1. **score collapse（训练配方）**：BCE 目标把 merged 的 yes_no logit 整体压入负区（serving 分数 0.14–0.46 vs base 0.08–0.72）——低分样本间排序被噪声主导，解释了 open-domain 的 −7.3pp 与 single-hop 的 −1.6pp。训练区分度未转化为绝对分数区分度。
2. **temporal-hard 训练生效但局部**：temporal +5 净翻转证明 temporal-hard negatives 有学习信号，但 R7 审计已警示 hard 池时间信号率仅 18.7%——训练数据的时间可判别性本身受限（[r7-temporal-audit.md](../../specs/037-memory-reranker-training/reports/r7-temporal-audit.md)）。
3. **训练数据与 serving 模板差异（遗留 caveat）**：训练 build_texts 无 `\n\n` 分隔，serving server.py 有 `\n\n`（US1 冻结）——系统性偏差对 base/merged 一致，不影响配对方向，但记录在案。

## 方法论发现（重要，影响后续所有 008 配对）

**单次 run 的系统噪声尺度 ≈ 8.6pp。** US1/US2 协议逐项一致（同 binary/数据/retrieval flags/regime/trace fallback/answer context tokens 654 vs 657），但 hybrid 基线 68.1% vs 59.5%，逐题 439 diff（US1 独对 286 vs US2 独对 153，不对称）。根因：两次 run 独立 extraction（v4-pro 随机，store 2944 vs 3077 entries）+ answer（temp=1.0）随机。

- **跨 run 单臂直接对比不可靠**（US2 rerank 58.4 vs US1 67.7 不能归因于模型）。
- **run 内配对（同 store）自洽有效**，是 008 判定的可靠依据。
- **检测 ~1pp 级杠杆必须 repeats ≥3 + `--store-dir` 复用**（消除 extraction 随机性）；单次 run 结论一律标注噪声下限。

## 成本账

| 项 | 费用 |
|---|---|
| 训练（AutoDL 4090，bce-infonce 9450×3ep ~20 分钟，含环境搭建） | ~¥数元（4090 时费率） |
| US1 评测（answer 4047 + extract 256 + judge 3080） | ~$6.6 |
| US2 评测（answer 4744 + extract 272 + judge 3080，含 retry） | ~$13 |
| **合计** | **~$20 + 少量 GPU 训练费** |

推理端自托管（transformers/vllm），零付费云 rerank（死亡规则遵守）。

## 诚实边界

- **US2 单 rep + 基线漂移 8.6pp**：temporal +1.6pp 未用 repeats/store-dir 复用确认，可能含噪声。
- **bce（stage1-only）消融未训练**：T017 只训了 bce-infonce；NO-GO 后 bce 消融无增量信息，不补训。
- **泛化否决门（T021）不适用**：GO 门已不通过，无泛化宣称需否决；heldout（conv-48/49/50）+ LME 500 未跑。
- **training-serving 模板差异**（上述机制归因 3）是 US1 冻结的历史遗留，未在本次修正。
- 报告正本：`specs/037-memory-reranker-training/reports/us1-paired.md` / `us2-paired.md` / `r7-temporal-audit.md`。

## 出货影响

- **无出货**：037 训练产物不出货，cross-encoder 永不进本地默认栈（死亡规则，宪法 I/V）。
- **方法论资产保留**：`tools/`（build_training_data / train_reranker / test_* / rerank_server / server / serve_trained）+ `contracts/rerank-serving.md`（transformers serving 冻结）+ `data/train-r1.jsonl`（确定性 9450 样本）可作为未来检索侧杠杆的复用基线。
- **temporal 若再探**：需针对性配方（修 score collapse：label smoothing/温度缩放；增强 temporal-hard 数据使 hard 池信号率 >50%）+ store-dir 复用 + repeats≥3。
