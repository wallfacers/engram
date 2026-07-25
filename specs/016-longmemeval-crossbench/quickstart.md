# Quickstart：LongMemEval 子集先行

**日期**: 2026-07-26

## 执行顺序（不可跳步）

```
P0 adapter 增量 + 离线测试（无远程依赖）
   │
   ▼
P1 G-尺子门 ──不过──► 停止，归档判决，本特性作废
   │
  ≥0.95
   ▼
P2 ORACLE 全 500（需排队）──► G-向量门
   │
   ▼
P3 S 臂 100 题（需排队）──► G-向量门 ──► 答题 ×3
   │
   ▼
P4 分账 + 判决 + 归档
```

**P1 未通过之前，MUST NOT 进入 P2。** 尺子不可信则后续数字全部无意义。

---

## 前置

数据已就位（commit `1fc86f8` 起 gitignore）：

```
testdata/longmemeval/longmemeval_oracle.json       # 500 题, 零干扰项
testdata/longmemeval/longmemeval_s_cleaned.json    # 500 题, 证据仅占 haystack 4.1%
```

远程评测机当前由其他工作占用。**P0/P1 全程本地**，不受排队影响。

---

## P0 · adapter 增量（零模型调用）

测试先行。每步之后：

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/
CGO_ENABLED=0 go test -count=1 ./...            # 合并前全量
git diff --name-only -- memory embedding provider store internal   # 必须为空
```

---

## P1 · G-尺子门（零 LLM 调用的部分 + 一次小规模抽取）

### 1. 切出 30 题 smoke 子集

一次性脚本从 oracle 取前 30 题（顺序取即可，此门验的是机制不是统计量），
输出 `oracle_smoke30.json`。

### 2. 建库

约 658 条消息的抽取。embedding 走本地服务（2026-07-25 已验证本地栈可复现归档向量，
逐条余弦 p50 = 0.9999）；抽取走小额付费口。**成本按脚本实测 usage 记录，不预先估算。**

```bash
setsid bash -c 'EMBED_BASE_URL=… EMBED_MODEL=… LOCOMO_API_KEY=… \
  ./locomo-bench --data <oracle_smoke30.json> --dataset-format longmemeval \
  --store-dir <run>/smoke-store --run-dir <run>/smoke-cov \
  --chunks --chunk-quota 12 --top-k 30 --coverage-only --retrieval hybrid \
  >p1.log 2>&1; echo $? >p1.exit' </dev/null >/dev/null 2>&1 & disown
[ -f p1.exit ] && echo "exit=$(cat p1.exit)" || tail -1 p1.log
```

### 3. 判据

| 判据 | 通过 | 判死 |
|---|---|---|
| 精确证据覆盖率 | **≥ 0.95** | < 0.95 |
| 答题模型调用数 | = 0 | ≠ 0 |
| 判分模型调用数 | = 0 | ≠ 0 |

覆盖率不达标 ⇒ 读取或计量有误（oracle 按构造零干扰项，覆盖率理应接近满分）
⇒ **停止**，归档判决。

---

## P2 · ORACLE 臂（全 500，需排队）

```bash
# 建库 + 答题 + 判分, canonical 配方
setsid bash -c '… ./locomo-bench --data testdata/longmemeval/longmemeval_oracle.json \
  --dataset-format longmemeval --store-dir <run>/oracle-store --run-dir <run>/oracle \
  --chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned \
  --retrieval hybrid >p2.log 2>&1; echo $? >p2.exit' </dev/null >/dev/null 2>&1 & disown
```

**建库后立即跑 G-向量门**（独立脚本）：

```bash
python3 <check_vectors.py> --store-dir <run>/oracle-store --model <EMBED_MODEL>
# total_missing 必须为 0；否则重复建库直至补齐（Backfill 受有界队列限制，一趟补不完）
```

> **为什么这是硬门禁**：`memory.Embedder.Backfill` 队列满即丢弃
> （`memory/embedder.go:236-255` 注释明写）。2026-07-25 的 A 实验中，一趟 build 只
> 回填 2569/4892 行，33% 语料对语义信号不可见且**检索器按设计静默降级不报错**，
> 一度把结果压低 6.4pp，差点被当成模型结论报出。

`--store-dir` 两臂**必须分目录**，否则互相覆盖（每题一库，ORACLE 500 个、S 100 个）。

---

## P3 · S 臂（分层抽样 100 题，需排队）

### 1. 分层抽样

一次性脚本按配额抽样，输出子集文件与 id 清单：

| 题型 | 配额 |
|---|---:|
| multi-session | 27 |
| temporal-reasoning | 27 |
| knowledge-update | 15 |
| single-session-user | 14 |
| single-session-assistant | 11 |
| single-session-preference | 6 |

固定种子；`longmemeval_s_subset100.json` 与 `subset100_question_ids.json` 落盘，
子集文件本身即可复现性凭证。

### 2. 建库 → G-向量门 → 答题 ×3

同 P2，但 `--data` 指向子集文件，重复 3 次答题以抑制方差。

---

## P4 · 分账与判决

1. 按逐题精确证据覆盖率分桶：全覆盖 / 部分覆盖 / 零覆盖
2. 各桶题数、正确率、Wilson 区间；**任一桶 n < 20 标记「不可判」**
3. 条件增益 = 全覆盖正确率 − 零覆盖正确率
4. 检索侧当量 = 零覆盖题数 × 条件增益；答题侧当量 = 全覆盖仍答错的题数
5. 对照**测量前登记**的判据：

| 结论 | 条件 |
|---|---|
| **复现** | 条件增益 ∈ [20, 50] pp **且** 检索侧当量 < 答题侧当量 |
| **证伪** | 条件增益 > 60 pp **或** 检索侧当量 > 答题侧当量 |
| **无法判定** | 其余 |

**不得四舍五入到任一侧，不得事后调整判据。**

### 归档

- 判决写入**本特性专属台账**（新文件），**不写**
  `docs/locomo-score-levers.md` —— 该文件正被其他工作并发修改
- 回填 `docs/paper-outline-eval-reliability.md` 的 RQ6 状态
  （提交前先 `git status` 确认无并发冲突，有冲突则停下升级）
- 脚本与原始产物推 HF 私仓；数据集与 store 不入库

---

## 长跑命令纪律（WSL2，硬规则）

所有建库/评测 MUST `setsid` detach + 文件轮询，**禁止**前台 `sleep` 轮询：

```bash
setsid bash -c '<cmd> >run.log 2>&1; echo $? >run.exit' </dev/null >/dev/null 2>&1 & disown
[ -f run.exit ] && echo "exit=$(cat run.exit)" || tail -1 run.log
```

日志一律进会话 scratchpad，不进仓库。
