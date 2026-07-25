# Quickstart：离线固结 · 跨 session 桥接合成

**日期**: 2026-07-25

## 执行顺序（不可跳步）

```
P1 证伪门 ──判死──► 停止，归档判决，特性作废
   │
  通过
   ▼
P2 桥接合成（TDD）──► 全量评测回归门 ──► P3 后台作业
```

**P1 未通过之前，MUST NOT 写任何引擎代码。** 这是本特性的核心纪律，也是 spec
US1 优先级为 P1 的原因。

---

## P1 · 证伪门（零引擎代码）

### 准备数据

本地 scratchpad 已清空，从 HF 私有集拉回：

```bash
# 需要 ~/.cache/huggingface/token（已配置）
hf download wallfacers/engram-locomo-artifacts --repo-type dataset \
  --include "009-eval-runs/*" "014b-oldtplan-confirm/*" \
  --local-dir "$SCRATCHPAD/hf"
# store（009-bge-chunks-store）同样从该私有集取回
```

数据只落在会话 scratchpad，**不进入仓库**（`.locomo-run/`、`*.db`、
`testdata/locomo/` 均已 gitignore）。

### 门 0（100% 免费，零模型调用）

复刻候选枚举逻辑（跨 session + 共享实体 + IDF 剪枝 + top-K=2000），计算：

| 判据 | 计算方式 | 通过 | 判死 |
|---|---|---|---|
| A 候选召回 | 分母=多跳失败题中「至少存在一个跨 session gold 证据对」的题数；分子=其中「至少一个此类对出现在候选集」的题数 | ≥ 60% | < 40% |
| B 候选规模 | 10 conv 候选对总数 | ≤ 2 万 | > 5 万 |

gold 证据对的求法：`locomoQA.Evidence`（`["D1:1","D2:1"]`）→ 经 `--chunks` 的
entry↔turn 映射 → gold entry 集合 → 取 `source_session_id` 互异的两两组合。

「多跳失败题」= 009-full-A-base 三次重复中多数判错的 category-1 题。

**灰区规则（写死，不可事后放宽）**：A 落在 40–60% 时，**只允许调整一次**
K/IDF 阈值重测；重测仍在灰区按判死处理。

### 门 1（近免费，只需 embedding，不调回答模型）

把每道门 0 命中题的最高分 gold 对**模板拼接**成 oracle 桥接 entry（**不用 LLM**），
插入 store 副本，重跑 `--coverage-only`：

```bash
setsid bash -c 'go run ./cmd/locomo-bench --data <locomo.json> \
  --store-dir <oracle-store-副本> --chunks --chunk-quota 12 --top-k 30 \
  --coverage-only --retrieval hybrid \
  >gate1.log 2>&1; echo $? >gate1.exit' </dev/null >/dev/null 2>&1 & disown
```

| 判据 | 计算方式 | 通过 | 判死 |
|---|---|---|---|
| C 可发现性 | 分母=插入的 oracle 桥接总数；分子=其中在**对应题**检索结果里进 top-30 的条数 | ≥ 50% | < 50% |
| D 覆盖增益 | coverage@30 相对基线的 Δ，沿用 `coverage.go` 的 `evidenceRecallAt` 口径 | **> 0** 严格大于 | ≤ 0 |

**D 必须用杀死 011/012 的同一把尺**，否则本次结论与历史结论不可比。

### 判决归档

无论通过还是判死，都写入 `docs/locomo-score-levers.md`（四项判据的实测值 + 结论），
并把脚本与原始产物推到 HF 私有集。判死时特性到此为止。

---

## P2 · 桥接合成（TDD）

### 测试先行（宪法「测试先行」）

先写这些**失败**测试，再写实现。全部离线、零模型（模型用 stub 闭包）：

| 测试 | 断言 |
|---|---|
| 候选枚举确定性 | 固定 entry+entity 集，两次调用产出完全相同的序列；同分按 (A,B) 字典序 |
| 跨 session 过滤 | 同 session 的对不出现在候选中 |
| 二阶禁止 | 已在 `memory_bridges` 的 entry 不进候选 |
| 桶上限 | 超过 `MaxBucketSize` 的实体桶被跳过 |
| NONE 拒绝闸 | stub 返回 NONE → 落库数为 0，无残留 |
| 悬空引用闸 | stub 返回不存在的源 → 拒绝落库 + 告警，整趟继续 |
| 冗余闸 | stub 返回与源内容等价的文本 → 拒绝落库 |
| ADD-only | pass 前后逐条比对源 entry 的内容与总数完全不变 |
| 幂等 | 连续两趟 → `memory_bridges` 行数不变、无重复 entry |
| inert 降级 | `call == nil` → RunPass 零副作用（无新 entry、无新血缘、无错误） |
| 无 embedder 降级 | `embedder == nil` → 产物仍落库，无 panic |
| 多实例 | 两个 worker 指向同一 DB，只有一个真正执行 |
| migration | v6 应用后表与索引存在；down 后干净移除 |

### 实现顺序

1. `store/migrations.go` 追加 v6（+ migration 测试）
2. `memory/consolidation/candidates.go` — 纯枚举（零模型，最易测）
3. `memory/consolidation/verdict.go` — `ParseVerdict` / `ValidateVerdict`
4. `memory/prompt/` — 固结提示词（含拒绝权与源回述要求）
5. `memory/consolidation/worker.go` — `RunPass` 编排 + 落库
6. `Notify`/`Start` 后台循环（P3）

### 每步验证

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./memory/consolidation/ ./store/
CGO_ENABLED=0 go test -count=1 ./...        # 合并前全量
```

### 回归门（宪法 IV，硬门禁）

本特性动写入路径 ⇒ **必须**跑全量 LoCoMo 对比：

- multi-hop 正确率相对当前基线**提升**
- 整体正确率**不低于**当前基线
- 用既有 canonical 配方与 paired McNemar 判显著性
- **eval 配置改动与算法改动分开提交**（可归因）

---

## P3 · 后台作业

在 P2 通过后接入 `Notify`/`Start`，补多实例与可恢复测试。此阶段对评测结论无影响，
是产品化与未来 SaaS 的形态准备。

---

## 长跑命令纪律（WSL2，硬规则）

所有 locomo-bench 运行 MUST `setsid` detach + 文件轮询，**禁止**前台 `sleep` 轮询：

```bash
setsid bash -c '<cmd> >run.log 2>&1; echo $? >run.exit' </dev/null >/dev/null 2>&1 & disown
[ -f run.exit ] && echo "exit=$(cat run.exit)" || tail -1 run.log
```

日志一律进会话 scratchpad，不进仓库。
