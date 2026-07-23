# Contract: US2 adapter 分解 + context-parity(cmd/locomo-bench,引擎零改)

## CLI 契约(新增 flag)

```
locomo-bench \
  --data <locomo.json> --run-dir .locomo-run/010-multiquery \
  --store-dir <009 固化 bge-large chunks store> \
  --chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned \
  --retrieval hybrid \
  --multi-query [--mq-max-subqueries 4] \
  [--repeats 3]
```

- `--multi-query`:开分解 → 用答题 LLM 把 question 分解成 ≤N 子查询 → 调 `Retriever.SearchMulti`。**缺省(不带此 flag)= 现基线单查询,逐字节不变。**
- `--mq-max-subqueries`:N 上限(默认 4,adapter 侧策略上限;引擎不硬编码)。
- 其余 flag 复用 canonical recipe 语义,不改含义。**两臂唯一变量 = 是否分解。**

## 行为契约

1. **引擎零改(SC-007)**:adapter 只调 `memory.Retriever.SearchMulti` / `Search` 公共 API;`git diff --name-only -- memory embedding provider store internal` 为空。
2. **分解退化(FR-009)**:LLM 失败/超时/返回 >N/全同质 → `[]string{question}`;此时 `SearchMulti` 走 len==1 短路 = 端到端等价单查询基线。
3. **提质硬约束(FR-010 / SC-004)**:两臂 `final_top_k` 恒等(=30);逐题记 `answer_context_tokens`,写 `context_parity.jsonl`。`multi` 臂 context **显著上升即判负**(隐性加量)。
4. **无凭据落盘**:分解 prompt/响应不含密钥;run 目录 gitignored。

## context_parity.jsonl schema(逐题一行)

```json
{"conv":2,"q":111,"category":2,"arm":"multi","final_top_k":30,"answer_context_tokens":3712,"subquery_count":3}
```

## 三道门(判定契约)

| 门 | 内容 | 通过条件 | 成本 |
|---|---|---|---|
| ① 纯 Go 契约 | `SearchMulti` 单测 + parity + `CGO_ENABLED=0` | 全绿 + len==1 逐字节 | free(本地) |
| ② 离线召回 | canned/LLM 子查询在固化 store 复跑 | 目标 multi-hop 题 gold rank>30→进 top-30 / coverage@30 ↑(**仅诊断,不作 verdict**) | near-free(retrieval-only) |
| ③ 端到端决胜 | 同机配对 `single` vs `multi`,box 栈 + deepseek mem0-aligned,repeats=3,McNemar | **multi-hop above-noise ↑ + overall 及任一非目标类不显著回退 + `answer_context_tokens` 不显著涨** | box 窗口 |

- 越不过 ③ → **NO-GO 出货**,保留为诊断能力,结论写 `docs/locomo-score-levers.md` 台账(含 coverage / 答题差分 / context 对比)。
- 须超越反证基线:008 reranker −0.06pp/p=1.0、009 cluster-sweep +0.4pp(噪声内)、cat-top-k +0.9pp(加量带税)。

## 提交分离(FR-014)

US1(`memory/`)与 US2(`cmd/locomo-bench`)分属不同 commit(归因,宪法 IV)。
