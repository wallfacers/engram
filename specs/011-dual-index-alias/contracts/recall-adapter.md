# Contract: re-embed + 三道门 gated(adapter,US2)

冻结 US2 adapter(`cmd/locomo-bench`)行为。引擎零改:`git diff --name-only -- memory embedding provider store internal` 必空;与 US1 分属不同 commit。

## Flags

- `--alias-shadow` 是 enum string:`off|baseline|treatment`,默认 `off`。`baseline`/`treatment` 都把 `--store-dir` 下的 `conv*.db`(含存在的 `-wal`/`-shm`)复制到 `<run-dir>/alias-store/`,并把本次有效 store-dir 指向副本;区别仅是 Backfill 后 baseline 剥离 `#alias` 行、treatment 保留并断言存在。**语义澄清(M1)**:flag 只控制副本内影子行的评测臂状态;检索归并是引擎**固有行为**(见 `#alias` 影子即 max-pool),由店内是否有影子向量触发,非 flag 直接开关。
- **Rationale**:US1 Backfill 无条件产影子,故 baseline 也需显式复制+剥离以保 canonical 纯净。
- 复用 `--recall-diagnostic`(retrieval-only,不初始化答题/judge caller)+ `--only-category`。
- `baseline|treatment` 与 `--multi-query` 互斥(避免与 010 变量混淆),要求 `--top-k 30`,且必须提供 `--store-dir`(不能纯内存)。

## re-embed 编排(retrieval-only,物理两店隔离 — H1)

- **canonical 绝不打开为运行店、绝不写影子**:两臂都先复制 009 店到各自 run-dir 的 `alias-store`;对副本跑现有 `Backfill`,且复用店 `countExtracted>0` 必须跳过抽取(断言抽取 caller calls=0)。
- baseline 在 Backfill 后执行 `DELETE FROM memory_embeddings WHERE entry_name LIKE '%#alias'`,再断言 `COUNT(*) ... LIKE '%#alias' = 0`;treatment 断言对应 count `>0`。两臂因此拥有同样的本地复制/重嵌入路径,唯一变量是检索时副本内是否保留影子行。
- 凭据(box bge-large 8001 嵌入)只走 env/隧道,绝不落盘。

## 门① 纯 Go 契约(FREE)

- US1 测试全绿 + `CGO_ENABLED=0 go test ./cmd/locomo-bench` 相关新测绿 + 引擎 diff 空(SC-007)。

## 门② 分层召回诊断(NEAR-FREE,retrieval-only,仅诊断不作 verdict)

- baseline(无影子)vs treatment(有影子)依次写入同一 run-dir,同 query 做 single 检索,复用 `buildAttributionTrace`/`evidenceRecallAt`;该模式不创建 query decomposition/答题/judge caller。
- 用 attribution 的事实覆盖判定从全部已存 fact 得到 gold stored entry names,再查 `memory_event_aliases` 判 `gold_has_alias`;输出「gold 有 alias」子层 + 全局各自的 `gold_entered/left_top_30`、`mean_gold_rank` delta、`coverage@30` delta。臂级 JSONL 与配对报告落 `.locomo-run/011-recall/`。
- **符号口径**:`rank_delta=treatment-baseline`(负=排名前移/改善);`coverage_delta=treatment-baseline`(正=改善)。
- **止损判据**:「gold 有 alias」子层 gold **净升 top-30**(entered > left 且 mean rank 前移)才算机制有信号;否则 NO-GO,不启动门③。coverage 仅诊断(FR-011)。

## 门③ 端到端配对 McNemar(BOX 窗口,repeats=3)

- 配对 baseline 店 vs treatment 店,**唯一变量 = alias 影子向量**;canonical recipe(`--chunks --chunk-quota 12 --force-answer --judge-mem0-aligned`)、box vllm Qwen + deepseek mem0-aligned judge、`--top-k 30`、`--repeats 3`。
- 跑完每臂 `cat regime.json` 核 `force_answer=true`/`judge=mem0-aligned`/`judge_model=deepseek-v4-flash`。
- **GO 判据(全满足)**:①目标类(open-domain/multi-hop)配对 McNemar above-noise ↑ ②overall 及任一非目标类不显著回退 ③`context_parity.jsonl` treatment `answer_context_tokens` 不显著 > baseline ④`final_top_k` 两臂恒等=30。否则 NO-GO。

## 收口

- 结论(GO/NO-GO + 子层/全局 delta + p 值 + context 对比 + coverage 诊断)写 `docs/locomo-score-levers.md` 台账 Feature 011;对比反证基线(008 reranker −0.06/p=1.0、009 cluster-sweep +0.4、010 分解 NO-GO)。NO-GO 则 `--alias-shadow` 保留默认关。adapter 改动单独 commit(FR-014)。
