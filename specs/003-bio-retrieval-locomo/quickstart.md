# Quickstart: 四枪运行手册（feature 003）

> 角色分工：实现与方法=本方；**评测运行与预算=维护者**。每次评测：先 `--estimate`
> 算账 → 维护者确认 → 正式跑 → `cost.json` 记账 → 结果记入 `eval-log.md`。

## 0. 环境

```bash
source .locomo-run/env.sh          # LOCOMO_API_KEY（本地文件，勿提交）
export LOCOMO_BASE_URL=...         # 中转站
export LOCOMO_PROVIDER=openai
export LOCOMO_MODEL=gpt-5.6-sol    # 答题/判分
export EXTRACT_MODEL=gpt-5.4-mini  # 抽取（校准期 A/B 后冻结）
export EMBED_BASE_URL=http://127.0.0.1:11434/v1
export EMBED_MODEL=qwen3-embedding:0.6b
export LOCOMO_PRICE_TABLE='{"gpt-5.6-sol":{"in":1.25,"out":10.0},"gpt-5.4-mini":{"in":0.15,"out":0.6}}'
go build ./cmd/locomo-bench
```

## Strike 0 — 校准基线（先算账）

```bash
# 1) 算账（不花钱）
./locomo-bench --data testdata/locomo/locomo10.json --repeats 5 --estimate

# 2) 抽取 A/B（两库各建一次，维护者确认预估后执行）
EXTRACT_MODEL=gpt-5.4-mini ./locomo-bench --data ... --run-dir .locomo-run/s0-extract-mini --repeats 5
EXTRACT_MODEL=gpt-5.6-sol  ./locomo-bench --data ... --run-dir .locomo-run/s0-extract-sol  --repeats 5

# 3) 配对判定 → 选定抽取模型并全程冻结；产出即校准基线
./locomo-bench --compare .locomo-run/s0-extract-mini .locomo-run/s0-extract-sol
```

记录：`stats.json` 均值±CI 即**校准基线**；与旧基线（74.7@luna）的差 = 模型贡献。

## Strike 1 — 联想检索

```bash
./locomo-bench --data ... --run-dir .locomo-run/s1-assoc --repeats 5 --assoc
./locomo-bench --compare .locomo-run/s0-<frozen> .locomo-run/s1-assoc
```

判定：multi-hop 提升 above-noise **且** 其他类别无 above-noise 回退 **且**
`answer_context_tokens_mean` ≤ 基线 1.5× → 保留（后续枪叠加 `--assoc`）；否则回退，
启用备选 PPR 再测一轮，仍不达标则记负结果。

## Strike 2 — 时间结构化

```bash
./locomo-bench --data ... --run-dir .locomo-run/s2-temporal --repeats 5 --assoc --temporal-score
./locomo-bench --compare .locomo-run/s1-assoc .locomo-run/s2-temporal
```

判定同上（目标类别=temporal）。⚠️ 诚实门：temporal 小样本已 10/11，within-noise
即砍，负结果照记。

## Strike 3 — 拒答 + 冲突消解（含对抗口径）

```bash
# 建库阶段带冲突消解重建一次库；答题换 abstain prompt（口径改动，独立提交）
./locomo-bench --data ... --run-dir .locomo-run/s3-full --repeats 5 \
  --assoc --temporal-score --conflict-resolution --abstain-prompt --adversarial
./locomo-bench --compare .locomo-run/s2-<best> .locomo-run/s3-full
```

判定：对抗题编造率显著降 **且** 可答题误拒率上升 within-noise → 保留。

## LongMemEval_S（终态验证，分批控预算）

```bash
./locomo-bench --dataset-format longmemeval --data testdata/longmemeval/longmemeval_s.json \
  --estimate --repeats 3          # 先算账！~500 题建库开销显著，维护者分批
./locomo-bench --dataset-format longmemeval --data ... --run-dir .locomo-run/lme-final \
  --repeats 3 <获胜 flag 组合>
```

## 记录纪律（每枪必做）

1. 跑前 `--estimate` 输出记入 `specs/003-bio-retrieval-locomo/eval-log.md`；
2. 跑后 `cost.json`（预估 vs 实际）、`stats.json`、`compare.json` verdict 同记；
3. 保留/回退结论 + flag 组合写明，负结果不删；
4. 算法 commit 与口径 commit 分开（宪法 IV）。

## 离线快速验证（不花钱，每次改动后）

```bash
go test ./memory -run 'TestRetrievalParity|TestSignalDegradation|TestAssociative|TestSupersede'
CGO_ENABLED=0 go build ./... && go vet ./...
```
