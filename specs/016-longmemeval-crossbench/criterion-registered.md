# 判据登记（测量前固化）

> ## ⚠️ 修正记录：G-尺子门判据于 2026-07-26 在**看过实测结果之后**重新登记
>
> 这是本特性最需要提防的一类操作，因此把它放在最显眼处，而不是藏在附录里。
>
> | 项 | 内容 |
> |---|---|
> | 改的是什么 | **只有 G-尺子门**的判据（原 FR-009 / SC-003 的 ≥0.95） |
> | **没改什么** | **最终判决判据**（条件增益 / 两侧当量，[criterion.txt](./criterion.txt)）**一个字未动**，SHA256 仍为 `2142f722…09ba`，已复核 |
> | 改动时点 | 在 smoke 集实测出 OVERALL = 0.901 **之后** |
> | 谁决定的 | 维护者裁定；执行者已先按原判据停在 P1 并归档失败（commit `af753c5`） |
> | 改动理由 | v1 阈值 0.95 的依据是「oracle 零干扰项 ⇒ 覆盖率理应接近满分」，该先验被实测否定：同一套系统、同一把严格尺子、同一配方在 **LoCoMo 上只有 0.808**。v1 要求 LongMemEval 达到系统在参照 benchmark 上从未达到过的水平，并把「检索预算够不够」混进了一个本该只检验「读取与计量对不对」的门 |
> | 是放宽还是收紧 | **收紧**。v1 容忍 canonical 配方下 0.95 的不完美；v2 要求两项与预算无关的上限指标都必须是**精确的 1.000** |
> | 新判据 | [criterion-gate-v2.txt](./criterion-gate-v2.txt)，SHA256 `b18cb90f96f7e55699e3f886dd929ea8f57eed3ce4c2bafbc5a50ddfffb7a7a3` |
>
> **本次修正不构成对「测量前固化」原则的豁免**。最终判决（T036）仍严格受
> `criterion.txt` 约束，且**不得**以任何理由重新登记 —— 若它也失败，那就是失败。


**登记时间**: 2026-07-26 · **登记于**: 任何产生正确率数字的任务之前（tasks.md T003）

本文件是 SC-008 的载体。判据原文单独存放于 **[criterion.txt](./criterion.txt)**，
在**任何测量发生之前**落盘并提交；T036 下判决时必须校验其 SHA256 未变、再逐字对照。
**提交后 `criterion.txt` 不得修改。**

若后续认为判据本身需要改动，唯一合法路径是：作废本次 016 判决、公开记录改动理由
与时点、以新判据重新登记并重跑。**不允许在看过数字之后调整阈值。**

## 校验

```bash
sha256sum specs/016-longmemeval-crossbench/criterion.txt
```

**登记值**:

```
2142f722233c97265be6f0238d7ba0e50091611ef82079c1c168d62f1e7609ba
```

T036 判决时必须先跑上面这条命令并确认与登记值一致；不一致 ⇒ 判据已被改动 ⇒
本次判决作废。

> **为什么判据单独成文件**：初稿把判据与说明写在同一个 Markdown 里，用
> `sed -n '/CRITERION-BEGIN/,/CRITERION-END/p'` 界定哈希范围。但说明段里的示例命令
> 自身就含有 `CRITERION-BEGIN` 字面量，`sed` 扫到它会再开一个范围，导致登记时与
> 校验时算出**不同的哈希** —— 一个自引用的校验等于没有校验。改为独立文件后
> 哈希范围就是整个文件，无歧义。

## 判据摘要（便于阅读，**以 criterion.txt 为准**）

| 结论 | 条件 |
|---|---|
| 复现 | 条件增益 ∈ [20, 50] pp **且** 检索侧当量 < 答题侧当量 |
| 证伪 | 条件增益 > 60 pp **或** 检索侧当量 > 答题侧当量 |
| 无法判定 | 其余（含 (50, 60] 这段有意留下的空档） |

任一桶 n < 20 ⇒ 该桶标记不可判；边界按闭区间字面判定，不得四舍五入到任一侧。

## 基线记录（tasks.md T001–T002，同批登记）

| 项 | 实测值 |
|---|---|
| `CGO_ENABLED=0 go build ./...` | exit 0 |
| `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/` | ok，0.733s |
| LoCoMo 零调用探测 `--estimate` | `dataset=locomo repeats=1 questions=1540 extract_calls=288`（T040 的比对锚） |
| `longmemeval_oracle.json` | 500 题，question_id 唯一 500 |
| `longmemeval_s_cleaned.json` | 500 题，question_id 唯一 500 |
| 两个数据集 gitignore | `.gitignore:37` 命中，均未入库 |

工作树中 `docs/` 下 7 个文件（README、capability-and-product-north-star、
competitive-benchmarks、locomo-score-levers、memory-strategy、
memos-inhouse-locomo-repro、paper-outline-eval-reliability）有其他工作的未提交改动，
**本特性不触碰**。
