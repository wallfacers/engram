# Contract: US1 引擎 `SearchMulti`(engine, contract-first, 退化保真)

## 公共 API(additive,非破坏)

```go
// SearchMulti runs each subquery through the existing three-signal hybrid and
// fuses the resulting ranked lists with RRF-of-RRF (reusing rrfK=60), returning
// the top-k. It never touches a query-time LLM: the caller decides whether/how to
// decompose and passes the subqueries in.
//
// Degenerate cases (byte-for-byte parity, offline default unchanged):
//   - after normalization (TrimSpace each, drop blanks, exact-dedup):
//       len==0 → returns nil (same as Search on empty query)
//       len==1 → delegates to SearchWithDiagnostics(ctx, sub, k); result is
//                byte-identical to Search(ctx, sub, k), including Score.
// For len>1, Result.Score is the RRF-of-RRF score and is NOT comparable to the
// single-query Search score.
func (r *Retriever) SearchMulti(ctx context.Context, subqueries []string, k int) ([]Result, error)
```

- **无 `RetrieverOptions` 新字段**:`SearchMulti` 是并列新入口,`Search` 语义与零值行为逐字节不变。
- **无 schema 变更**:不碰 `store/migrations.go`。
- **纯 Go / CGO_ENABLED=0**:无云/付费 reranker 依赖(死规则);检索路径无 query-时 LLM(宪法 I)。

## 行为契约

1. **退化保真(SC-001)**:`SearchMulti(ctx, []string{q}, k)` 结果集(顺序 + 每字段含 `Score`)与 `Search(ctx, q, k)` 逐字节相同。实现路径 = 规范化后 `len==1` 短路 `SearchWithDiagnostics`,不进二级融合。
2. **融合语义(SC-002)**:`len>1` 时——
   - 每子查询 `sub_i` 跑 `SearchWithDiagnostics(ctx, sub_i, k)`,取其有序 `[]Result` 的 name 序列为 `L_i`(深度来自引擎既有内部候选池 `k*candidateMultiplier`,非扩最终 k)。
   - `ranks_i = ranksFromOrder(L_i)`(1-based)。
   - `fused = fuseRRF(ranks_1, …, ranks_N)`,`Score(d)=Σ_i 1/(60+rank_i(d))`(未命中该 `L_i` 记 0)。
   - `fused` 截断 top-k,逐条 `GetByName` 装 `[]Result`(与 `Search` 同一装配)。
3. **共同命中优先**:被多个 `L_i` 命中的 doc 累加得分 → 排名高于同名次单命中 doc。
4. **静默降级(宪法 V)**:某 `sub_i` 检索为空 → `L_i` 空、对所有 doc 记 0,不影响其余融合;不 panic。
5. **确定性**:串行精检 + 稳定排序(得分相等按 name 升序,复用现有 tie-break)→ 同输入同输出(golden 可复算)。
6. **无新可调权重**:复用 `rrfK=60`,不引入对 LoCoMo 拟合的融合权重。

## 测试契约(TDD,先写失败)

- `TestSearchMulti_SingleQueryParity`:随机若干 query,断言 `SearchMulti(ctx,[]string{q},k)` == `Search(ctx,q,k)`(深度相等 + 每 `Result` 字段相等,含 `Score`)。**先失败**(方法未实现)。
- `TestSearchMulti_FusionLiftsCoHit`:小 fixture 构造 gold doc 在单查询 `q` 下 rank>k、但被两个子查询分别较高命中;断言 `SearchMulti(ctx, subs, k)` 后 gold 进 top-k。
- `TestSearchMulti_CoHitOutranksSingleHit`:构造 doc A 被 2 子查询命中、doc B 仅 1 子查询命中且各自 rank 相近;断言 A 排在 B 前。
- `TestSearchMulti_Degenerate`:空数组→nil;含空串→跳过不 panic;某子查询空检索→其余正常融合。
- `CGO_ENABLED=0 go test ./memory` 全绿。
