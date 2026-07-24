# 契约：US2 adapter backfill + 三道门（冻结 CLI + 判据，照抄）

**范围**：仅 `cmd/locomo-bench/`。US2 提交 `git diff --name-only -- memory embedding provider store internal` **必空**。引擎零改。照搬 011 的 `--alias-shadow` 三臂 + 两店隔离骨架。

## A1. CLI

```
--doc2query off|baseline|treatment   (default off)
```
- `off`：不触碰伪查询路径（现状）。
- `baseline`：复制 009 canonical 店到 `<run-dir>/doc2query-store`，backfill 伪查询后**剥离** `#query` 影子向量（assert `memory_fact_queries` 行可留但 `#query` 向量行=0），Backfill 只补 text/alias。
- `treatment`：同复制 + backfill + **保留** `#query` 影子。
- canonical 店**从不被打开为运行店**（方案 A）。校验：拒绝 `--doc2query != off` 与 `--multi-query`/`--top-k != 30` 并用（守提质硬约束）。

## A2. Backfill 解耦为一次性 `--doc2query-build`（关键架构）

**LLM 生成问句不能在 diagnostic/e2e 时做**（那里 caller=`extractNever`，任何 LLM 调用即报错，守零抽取）。故把它解耦成一次性预建步，与两臂检索分离——与「009 店建一次后复用」同构：

```
--doc2query-build --store-dir <009-canonical> --run-dir <B>
```
1. 复制 `<009-canonical>` → `<B>/doc2query-store`（reuse 011 `copyStoreDir`，canonical 只读）。
2. 对每个 conv 店内每条 fact（`FactSource=="extraction"` 且无 `memory_fact_queries` 行）：LLM（答题/抽取模型，`LOCOMO_*` env）生成 **2-3** 问句（提示词 A5，温度固定，解析失败/空→跳过不阻断）→ `entries.PutFactQueries`。
3. `embedder.Backfill(ctx)` 让 `QueryShadowNames` 把 `#query` 影子嵌出（EMBED 走 box bge-large 8001）。
4. 产物 `doc2query_backfill.jsonl`（name→queries，无凭据）。**付费一次**。

**两臂都以 `<B>/doc2query-store` 为 `--store-dir`**（不是 canonical 009）：
- `baseline`：复制到 `<run-dir>/doc2query-store` 后 `enforceDoc2QueryStoreMode` 剥离 `%#query` 向量（assert 0）。`memory_fact_queries` 行留存不影响检索。
- `treatment`：复制后保留 `#query` 向量（assert >0）。
- 两臂唯一检索差异 = `#query` 向量；`<B>/doc2query-store` 与 canonical 009 均从不被写为运行店。

## A3. 门② 分层召回诊断（near-free，retrieval-only）

同 011：baseline/treatment 两臂同 query 检索，写 `doc2query_recall_<arm>.jsonl` + 配对 `doc2query_recall.json`。分层键=「gold fact 命中」子层。
**判据（`rank_delta = treatment − baseline`，负=前移=变好）**：
- 目标类 gold 子层 gold **净升 top-30**（`entered > left`，mean rank 前移）**且** coverage@30 delta > 0 → 有信号，进门③。
- 否则（子层不前移 / coverage@30 delta ≤ 0，复现 011 对称抬噪）→ **NO-GO 止损，不启动门③**。
- 目标类：multi-hop `--only-category 1`；open-domain `--only-category 3`。

## A4. 门③ 端到端配对 McNemar（box，repeats=3，唯一变量=`#query` 影子）

两臂 recipe 逐字一致，只差 `--doc2query baseline|treatment`。canonical 四 flag（`--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned`）缺一作废。EMBED 走 box bge-large 8001 隧道。
**GO 须**：目标类 above-noise + 非目标类不回退 + `context_parity.jsonl` treatment `answer_context_tokens` 不显著 > baseline + `final_top_k=30` 恒等。

## A5. Backfill 提示词（冻结）

```
System: You generate the questions a memory fact directly answers, for a
retrieval index. Given ONE self-contained fact, output 2-3 SHORT, natural
questions a user might ask that this fact answers. Each question must be
answerable by the fact alone. Vary phrasing (who/when/what/where). Return
STRICT JSON: {"queries":["...","..."]}. No prose.

User: FACT: <fact content>
Return the JSON now.
```
解析失败 / 空 queries → 该 fact 跳过（no-op，不阻断 backfill）。

## A6. 单测（门①，mock，引擎零改）
- canonical 店全程 `#query` 向量行=0（两臂前后未污染正本）。
- baseline 剥离后 `#query` 向量行=0；treatment > 0。
- backfill 不触发抽取（extraction call 计数=0，用 mock caller 断言）。
- 校验拒绝 `--doc2query treatment` + `--top-k 40` / `--multi-query`。
- 分层诊断 gold 子层桶正确（`gold_hit` 分层键）。
