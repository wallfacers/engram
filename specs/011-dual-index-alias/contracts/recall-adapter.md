# Contract: re-embed + 三道门 gated(adapter,US2)

冻结 US2 adapter(`cmd/locomo-bench`)行为。引擎零改:`git diff --name-only -- memory embedding provider store internal` 必空;与 US1 分属不同 commit。

## Flags

- `--alias-shadow`:treatment 臂**将目标店复制为副本、对副本 re-embed 补影子向量、检索用副本**;缺省 = baseline 用**无影子的原店**、逐字节不变。**语义澄清(M1)**:此 flag 只控制「是否产影子副本并检索它」;检索归并是引擎**固有行为**(见 `#alias` 影子即 max-pool),由**店内是否有影子向量**触发,非 flag 直接开关。
- 复用 `--recall-diagnostic`(retrieval-only,不初始化答题/judge caller)+ `--only-category`。
- `--alias-shadow` 与 `--multi-query` 互斥(避免与 010 变量混淆);要求 `--top-k 30`。

## re-embed 编排(retrieval-only,物理两店隔离 — H1)

- **原店绝不写影子**:treatment 臂先把目标 009 店**复制为副本**(`.locomo-run/011-*/shadow-store`),再对**副本**枚举 `AliasShadowNames`、`Enqueue` 影子、等 embedder 落盘;**不重抽取**(断言抽取 caller calls=0)。baseline 臂直接用**原店**(无影子)。
- 两臂物理隔离(原店 vs 副本+影子)保证 baseline 逐字节 parity(SC-001)、决胜门唯一变量=影子向量(research D7)。
- 凭据(box bge-large 8001 嵌入)只走 env/隧道,绝不落盘。

## 门① 纯 Go 契约(FREE)

- US1 测试全绿 + `CGO_ENABLED=0 go test ./cmd/locomo-bench` 相关新测绿 + 引擎 diff 空(SC-007)。

## 门② 分层召回诊断(NEAR-FREE,retrieval-only,仅诊断不作 verdict)

- baseline(无影子)vs treatment(有影子),同 query single 检索,复用 `buildAttributionTrace`/`evidenceRecallAt`。
- 输出**按 `gold_has_alias` 分层**:「gold 有 alias」子层 + 全局 各自的 `gold_entered/left_top_30`、`mean_gold_rank` delta、`coverage@30` delta → `.locomo-run/011-recall/`。
- **止损判据**:「gold 有 alias」子层 gold **净升 top-30**(entered > left 且 mean rank 前移)才算机制有信号;否则 NO-GO,不启动门③。coverage 仅诊断(FR-011)。

## 门③ 端到端配对 McNemar(BOX 窗口,repeats=3)

- 配对 baseline 店 vs treatment 店,**唯一变量 = alias 影子向量**;canonical recipe(`--chunks --chunk-quota 12 --force-answer --judge-mem0-aligned`)、box vllm Qwen + deepseek mem0-aligned judge、`--top-k 30`、`--repeats 3`。
- 跑完每臂 `cat regime.json` 核 `force_answer=true`/`judge=mem0-aligned`/`judge_model=deepseek-v4-flash`。
- **GO 判据(全满足)**:①目标类(open-domain/multi-hop)配对 McNemar above-noise ↑ ②overall 及任一非目标类不显著回退 ③`context_parity.jsonl` treatment `answer_context_tokens` 不显著 > baseline ④`final_top_k` 两臂恒等=30。否则 NO-GO。

## 收口

- 结论(GO/NO-GO + 子层/全局 delta + p 值 + context 对比 + coverage 诊断)写 `docs/locomo-score-levers.md` 台账 Feature 011;对比反证基线(008 reranker −0.06/p=1.0、009 cluster-sweep +0.4、010 分解 NO-GO)。NO-GO 则 `--alias-shadow` 保留默认关。adapter 改动单独 commit(FR-014)。
