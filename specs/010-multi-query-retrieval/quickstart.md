# Quickstart: 多查询检索(multi-query retrieval)

## US1 —— 引擎 `SearchMulti`(free,单测即验)

先写失败测试,再实现(TDD)。

```bash
# 1) 契约门:parity + 融合正确性 + 退化(先失败 → 实现 → 全绿)
CGO_ENABLED=0 go test ./memory -run 'TestSearchMulti' -count=1 -v

# 2) 全引擎回归(退化保真不破坏既有 parity golden)
CGO_ENABLED=0 go test ./memory -count=1
CGO_ENABLED=0 go build ./...
```

**验 SC**:
- SC-001 `TestSearchMulti_SingleQueryParity` 绿(`SearchMulti(ctx,[]string{q},k)` 逐字节等于 `Search`)。
- SC-002 `TestSearchMulti_FusionLiftsCoHit` + `_CoHitOutranksSingleHit` 绿(确定性,无 LLM)。
- SC-003 `CGO_ENABLED=0` 构建+测试通过,无云 reranker 依赖。

**US1 交付 = 单独 commit**(engine),`git diff --name-only` 仅 `memory/`。

## US2 —— adapter 分解 + 三道门

### 门① 纯 Go 契约(free)
US1 的测试即门①;adapter 分解退化单测(mock provider):
```bash
CGO_ENABLED=0 go test ./cmd/locomo-bench -run 'TestDecompose' -count=1
```

### 门② 离线召回诊断(near-free,retrieval-only)
在固化 store 上跑分解 + `SearchMulti`,比 single 臂看目标 multi-hop 题 gold 是否升进 top-30、coverage@30 delta。**仅诊断,不作 verdict**。
```bash
# 复用 009 固化 bge-large chunks store(HF 009-bge-chunks-store);retrieval-only,不调答题
setsid bash -c 'go run ./cmd/locomo-bench --data <locomo.json> \
  --run-dir .locomo-run/010-recall --store-dir <009-store> \
  --chunks --top-k 30 --retrieval hybrid --multi-query --mq-max-subqueries 4 \
  --only-category 1 --recall-diagnostic >recall.log 2>&1; echo $? >recall.exit' </dev/null >/dev/null 2>&1 & disown
# ⚠️ 目标类 multi-hop = --only-category 1(dataset.go categoryLabel:1=multi-hop,2=temporal,4=single-hop)
[ -f recall.exit ] && echo "exit=$(cat recall.exit)" || tail -1 recall.log
```

### 门③ 端到端决胜(box 窗口)
同机配对 `single` vs `multi`,唯一变量=分解,repeats=3,配对 McNemar。**须 multi-hop above-noise + 非目标类不回退 + `answer_context_tokens` 不涨。**
```bash
# 前置:box vllm Qwen 答题栈(隧道)+ deepseek mem0-aligned judge env 已就绪
# single 臂(基线)
setsid bash -c 'go run ./cmd/locomo-bench --data <locomo.json> \
  --run-dir .locomo-run/010-e2e-single --store-dir <009-store> \
  --chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned \
  --retrieval hybrid --repeats 3 >single.log 2>&1; echo $? >single.exit' </dev/null >/dev/null 2>&1 & disown
# multi 臂(+分解)—— 与 single 同批同栈
setsid bash -c 'go run ./cmd/locomo-bench --data <locomo.json> \
  --run-dir .locomo-run/010-e2e-multi --store-dir <009-store> \
  --chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned \
  --retrieval hybrid --multi-query --mq-max-subqueries 4 --repeats 3 >multi.log 2>&1; echo $? >multi.exit' </dev/null >/dev/null 2>&1 & disown
[ -f single.exit ] && echo "single exit=$(cat single.exit)" || tail -1 single.log
[ -f multi.exit ]  && echo "multi exit=$(cat multi.exit)"   || tail -1 multi.log
```

**验 SC**:
- SC-004 提质:两臂 `final_top_k=30` 恒等,`context_parity.jsonl` 里 `multi` 臂 `answer_context_tokens` 不显著 > `single`。
- SC-005 召回诊断:目标 multi-hop 题 gold 升进 top-30 / coverage@30 ↑(诊断)。
- SC-006 判定诚实:GO/NO-GO 由配对 McNemar(above-noise + 非目标类不回退 + context 不涨)决定;超越 cat-top-k(+0.9pp 带税)才算提质赢。
- SC-007 引擎/适配器分离:`git diff --name-only -- memory embedding provider store internal` 为空;US1/US2 分属不同 commit。

## 收口

结论(GO 或 NO-GO)+ 数字(multi-hop delta、p 值、context 对比、coverage 诊断)写入 `docs/locomo-score-levers.md` 杠杆台账;越不过则保留为默认关诊断能力(与 008 reranker / 009 cluster-sweep 同样诚实处理)。
