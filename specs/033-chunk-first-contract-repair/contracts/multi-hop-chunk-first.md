# Contract: Multi-hop Chunk-First Entity Assembly

## Boundary

输入是检索与 chunk quota 已经确定的候选闭包。该合同不搜索、不扩大 top-k、不读取 gold/judge
信息，只规定 `--evidence-assembly` 下 multi-hop 候选如何排序、截断与呈现。

## Normal mode: `kind_layered`

1. 候选按 kind 分成 chunk 与 fact 两个全局 layer，chunk layer 永远先输出。
2. 实体 coverage 在完整输入闭包上计算；每个 layer 按 coverage desc、entity asc 遍历相同组序。
3. 组内按 score desc、source ID asc、原 ordinal asc。
4. ungrouped 在每个 layer 的实体组之后。
5. 同一实体跨 kind 时允许重复实体 header；不得因此把 fact 提前。
6. `EvidenceAssembly.Units` 是上述 canonical flat sequence 的预算前缀。
7. renderer 必须按 `Units` 顺序逐项输出编号 evidence 行，只能插 header，不能 partition/sort。

## Legacy control: `legacy_grouped`

- 仅供 benchmark 配对和回退，默认关闭；没有开启 evidence assembly 时不得单独启用。
- 完整复用修复前的 group-major 顺序和 renderer。
- mode 必须同时出现在 assembly audit 与 answer regime fingerprint；run-dir 不得跨 mode resume。

## Cap semantics

- 精确 token counter 可用：渲染完整 canonical sequence；若超 cap，每轮删除最后一个 unit 并重新
  渲染/计数，直到不超 cap或 units 为空。
- counter 不可用：保留既有 estimate fallback 并标记 `tokens_estimated=true`；不得假装精确。
- relation block 如启用，必须在每个 prefix 上重算，边端点只能来自最终 admitted units。

## Fail-soft cases

| Input | Required behavior |
|---|---|
| empty | 合法 `(none)` prompt，零 units |
| chunks only | 只输出 chunk layer |
| facts only | 只输出 fact layer |
| no liftable entity | 每个现存 kind 进入其 ungrouped 段 |
| duplicate score | source ID/ordinal 决胜，输出确定 |

## Non-interference

- temporal、single-hop、open-domain：修复前后 byte-identical。
- evidence assembly off：legacy answer context byte-identical。
- input closure：candidate ID/text 多重集不变；cap 后只要求各模式输出自己的规范 prefix。
- 额外 retrieval/LLM/rerank/judge 调用：0。
- engine directories changed：0。

## Offline acceptance

- mixed multi-hop 100% chunk-before-fact；
- prompt evidence line sequence == assembly unit sequence；
- same input and equal-score input permutations produce identical output；
- exact cap keeps canonical prefix；
- normal/legacy mode are both fingerprinted and cannot share a resumed run dir；
- changing private gold answer/evidence, correctness, judge verdict or gold-rank sentinels leaves order, prompt bytes,
  call count and mode selection unchanged。
