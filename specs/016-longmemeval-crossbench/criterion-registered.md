# 判据登记（测量前固化）

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
