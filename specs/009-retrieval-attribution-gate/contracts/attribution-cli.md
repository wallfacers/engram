# Contract: US1 归因 CLI + trace.jsonl schema(adapter)

## CLI 契约(cmd/locomo-bench 新增,retrieval-only)

```
locomo-bench --attribution-trace \
  --data <locomo.json> --run-dir .locomo-run/009-attribution \
  --store-dir <008 固化 store> --retrieval hybrid --top-k 30 \
  [--only-category 1,2,4] [--join-results .locomo-run/008-us4-e2e/results-hybrid.jsonl] \
  [--embed-probe] [--outrank-cap 5] [--wide-pool <n>]
```

- `--attribution-trace`:进入 retrieval-only 归因模式。**MUST NOT** 调用答题/judge 模型(SC-003:答题调用=0)。
- `--join-results`:提供已归档答题结果做四象限 join;缺省时 `quadrant` 只填检索侧(`gold_in_pool`/`gold_rank`),`correct` 留空、象限降级为 `retrieval_only`。
- `--embed-probe`:附带跑 embedding 确定性探针,输出 `embed_probe.json`。
- 复用现有 `--store-dir`/`--retrieval`/`--top-k`/`--only-category`/`--chunks` 语义,不改其含义。

## 行为契约

1. 引擎零改:归因只调 `memory.Retriever.SearchWithDiagnostics` 公共 API(FR-006)。
2. 确定性:同 store + 同题集 + 同 flag → `trace.jsonl` 逐字段一致(SC-004)。
3. 退化:gold 不可解析 → 该题 `quadrant="gold_unresolved"`,不入分母(edge case)。
4. 无凭据落盘(journal.go 纪律)。

## trace.jsonl schema(逐题一行)

```json
{
  "conv": 2, "q": 111, "category": 1, "category_name": "single_hop",
  "gold_evidence": ["D3:14"], "gold_turns": ["D3:14"],
  "retrieved": [
    {"name":"fact_...","rank":1,"rrf_score":0.031,"covers_gold":false,"mapped_gold_turns":[]},
    {"name":"fact_...","rank":4,"rrf_score":0.024,"covers_gold":true,"mapped_gold_turns":["D3:14"]}
  ],
  "gold_in_pool": true, "gold_rank": 4,
  "outranked_by": [{"name":"fact_...","rank":1,"rrf_score":0.031}],
  "quadrant": "q3_us2_target",
  "correct": false, "correct_source": "008-us4-e2e/results-hybrid.jsonl"
}
```

- `per_signal_ranks` 字段在 US1 阶段**省略**(引擎未暴露;见 research D2);US2 引擎增量后填充。

## distribution 表 schema(`quadrant-distribution.json`)

```json
{"single_hop":{"q1_ok":689,"q2_answer_side":14,"q3_us2_target":37,"q4_extraction_side":14,"gold_unresolved":3,"total_gradeable":841}, "...": {}}
```
（示例数字仅示意格式，非结论）
