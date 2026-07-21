# Contracts: LoCoMo Judge 口径对齐

外部契约:(1) 新 CLI flag,(2) judge 输出 JSON 契约(**保持不变**),(3) run fingerprint 标签,(4) golden 夹具 schema。Phase 1 冻结(宪法 III)。无引擎 API / SQLite schema 变更。

## 1. CLI flag(`cmd/locomo-bench`)

| Flag | 类型 | 默认 | 含义 |
|---|---|---|---|
| `--judge-mem0-aligned` | bool | `false` | on 时 judge 使用 Mem0 对齐的宽松规则,并在 run fingerprint 追加 `;judge=mem0-aligned`。off 时行为与当前实现逐字一致。 |

**行为契约**:
- 默认 off ⇒ 旧严格 judge,零回归(SC-005)。
- flag 仅切换 `judgeSystemPrompt` 规则文本 + fingerprint 标签;**不改** judge 输出解析、答题路径、检索。
- flag 不触发任何全量重判/重跑(US1 零答题成本);量化在 US2。

## 2. judge 输出 JSON 契约(不变)

```json
{"correct": true}
```
或 `{"correct": false}`。两种口径下**输出格式相同**;`parseJudgeVerdict` 容错解析("correct" 后首个 true/false)保持不变。契约不变 ⇒ 无破坏性变更。

## 3. run fingerprint(扩展)

```
force_answer=<bool>;abstain_prompt=<bool>;no_idk_retry=<bool>[;temporal_answer_prompt=true][;judge=mem0-aligned]
```
**不变量**:`;judge=mem0-aligned` 仅在 flag on 时出现;两种口径的 fingerprint 必不相同 ⇒ 跨口径 run 不可被静默配对/复用缓存(SC-006)。

## 4. golden 夹具 schema(`testdata/judge_golden.jsonl`)

每行:
```json
{"question": "...", "gold": "...", "predicted": "...", "expected_correct": true, "rule": "partial-credit", "note": "gold 列表 2 项,预测命中 1 项"}
```
**不变量**:
- `expected_correct=false` 的项(anti-放水陷阱)在 golden 层真跑中判 WRONG,否则测试红(SC-001)。
- `expected_correct=true` 的项判 CORRECT(SC-002)。
- 夹具须覆盖 data-model 的覆盖矩阵(宽松三类 + 陷阱六类 + 边界三类),条数 ~25-30。

## 5. 引擎-不可碰契约(硬门)

```
git diff --name-only master...007-judge-metric-alignment -- memory embedding provider store internal
# 每个 mechanism 提交都 MUST 为空(FR-007 / SC-004)
```
