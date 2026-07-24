# Quickstart: dual-index alias 向量

## US1 —— 引擎 dual-index(free,单测即验)

先写失败测试,再实现(TDD)。

```bash
# 1) 契约门:parity + 归并正确 + 去重 + 影子不泄漏 + 退化(先失败 → 实现 → 全绿)
CGO_ENABLED=0 go test ./memory -run 'TestAliasShadow' -count=1 -v

# 2) 全引擎回归(退化保真不破坏既有 parity golden)
CGO_ENABLED=0 go test ./memory -count=1
CGO_ENABLED=0 go build ./...
```

**验 SC**:
- SC-001 `TestAliasShadow_NoAliasParity` 绿(无 alias fact + chunk semantic 逐字节等于现状)。
- SC-002 `TestAliasShadow_MergeLiftsSource` + `_DedupSingleVote` + `_ShadowNameNeverLeaks` 绿(确定性,无 LLM)。
- SC-003 `CGO_ENABLED=0` 构建+测试通过,无云 reranker 依赖,无 α。

**US1 交付 = 单独 commit**(engine),`git diff --name-only` 仅 `memory/`。

## US2 —— adapter re-embed + 三道门

### 门① 纯 Go 契约(free)
```bash
CGO_ENABLED=0 go test ./cmd/locomo-bench -run 'TestAliasShadow|TestReembed' -count=1
git diff --name-only -- memory embedding provider store internal   # 必空
```

### 门② 分层召回诊断(near-free,retrieval-only)
在 009 固化店上产/不产影子两版,同 query 检索,**主看「gold 有 alias」子层**。**仅诊断,不作 verdict**。
```bash
# EMBED 走 box bge-large 8001 隧道(与 009/010 同);retrieval-only 不调答题
export EMBED_BASE_URL=http://127.0.0.1:8001/v1 EMBED_MODEL=bge-large-en-v1.5 EMBED_API_KEY=local-eval
setsid bash -c 'go run ./cmd/locomo-bench --data testdata/locomo/locomo10.json \
  --store-dir <009-bge-chunks-store> --chunks --top-k 30 --retrieval hybrid \
  --alias-shadow baseline --only-category 1 --recall-diagnostic \
  --run-dir .locomo-run/011-recall >recall-base.log 2>&1; echo $? >recall-base.exit' </dev/null >/dev/null 2>&1 & disown
[ -f recall-base.exit ] && echo "exit=$(cat recall-base.exit)" || tail -1 recall-base.log
# baseline exit=0 后再跑 treatment;两次共用 run-dir 以生成配对分层 delta
setsid bash -c 'go run ./cmd/locomo-bench --data testdata/locomo/locomo10.json \
  --store-dir <009-bge-chunks-store> --chunks --top-k 30 --retrieval hybrid \
  --alias-shadow treatment --only-category 1 --recall-diagnostic \
  --run-dir .locomo-run/011-recall >recall-treatment.log 2>&1; echo $? >recall-treatment.exit' </dev/null >/dev/null 2>&1 & disown
[ -f recall-treatment.exit ] && echo "exit=$(cat recall-treatment.exit)" || tail -1 recall-treatment.log
# 判定:「gold 有 alias」子层 gold 净升 top-30(entered>left,mean rank 前移)才算有信号;否则 NO-GO 止损,不跑门③
# 目标类:multi-hop=--only-category 1;open-domain=--only-category 3(dataset.go categoryLabel)
# 方案 A:两臂均复制 009 店到 <run-dir>/alias-store;baseline Backfill 后剥离影子,treatment 保留;canonical 从不被打开为运行店
```

### 门③ 端到端决胜(box 窗口,repeats=3,唯一变量=alias 影子向量)
两臂 recipe 逐字一致,只差 `--alias-shadow baseline|treatment`。canonical 四 flag 缺一作废。
**方案 A 两店隔离**:baseline/treatment 都把 009 canonical 店复制到各自 `<run-dir>/alias-store`;baseline 副本在 Backfill 后剥离影子,treatment 副本保留影子。canonical 从不被写,两臂唯一变量=影子向量。
```bash
source ~/.config/engram/locomo-vllm.env; source ~/.config/engram/judge.env
export EMBED_BASE_URL=http://127.0.0.1:8001/v1 EMBED_MODEL=bge-large-en-v1.5 EMBED_API_KEY=local-eval
# baseline 臂
setsid bash -c 'go run ./cmd/locomo-bench --data testdata/locomo/locomo10.json \
  --store-dir <009-bge-chunks-store> --chunks --chunk-quota 12 --top-k 30 \
  --force-answer --judge-mem0-aligned --retrieval hybrid --concurrency 40 --repeats 3 \
  --alias-shadow baseline \
  --run-dir .locomo-run/011-e2e-base >base.log 2>&1; echo $? >base.exit' </dev/null >/dev/null 2>&1 & disown
# treatment 臂(+影子,其余逐字相同)
setsid bash -c 'go run ./cmd/locomo-bench --data testdata/locomo/locomo10.json \
  --store-dir <009-bge-chunks-store> --chunks --chunk-quota 12 --top-k 30 \
  --force-answer --judge-mem0-aligned --retrieval hybrid --concurrency 40 --repeats 3 \
  --alias-shadow treatment \
  --run-dir .locomo-run/011-e2e-shadow >shadow.log 2>&1; echo $? >shadow.exit' </dev/null >/dev/null 2>&1 & disown
```

**验 SC**:
- SC-004 提质:两臂 `final_top_k=30` 恒等,`context_parity.jsonl` treatment `answer_context_tokens` 不显著 > baseline。
- SC-005 分层召回:目标类「gold 有 alias」子层 gold 升进 top-30 / coverage@30 ↑(诊断)。
- SC-006 判定诚实:GO/NO-GO 由配对 McNemar(above-noise + 非目标类不回退 + context 不涨 + top-k 恒等)决定;超越反证基线(010 分解 NO-GO)。
- SC-007 引擎/适配器分离:`git diff --name-only -- memory embedding provider store internal` 为空;US1/US2 分属不同 commit。

## 收口

结论(GO/NO-GO + 子层/全局 delta、p 值、context 对比、coverage 诊断)写入 `docs/locomo-score-levers.md` 台账 Feature 011;越不过则保留 `--alias-shadow` 默认关(与 008 reranker / 010 分解同样诚实处理)。**box 空闲必停。凭据只走 env/隧道。WSL2 长跑 setsid 分离。**
