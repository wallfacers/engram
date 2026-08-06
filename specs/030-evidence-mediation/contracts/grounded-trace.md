# Contract: 接地证据链（Grounded Trace，US2）

**契约对象**: 引用链证据中介（`cmd/locomo-bench/trace_mediation.go` + `trace_gate.go` + `trace_http.go`）
**范围**: sidecar 生成 trace（opt-in 默认关）→ fail-closed 门（纯 Go 确定性）→ 最终证据 E 喂 answerer；引擎零改动

## Sidecar 输入/输出（DeepSeek-flash，opt-in）

**输入**: `question` + `candidates`（候选集 C_q，每条带 `source_id` + `text`）——模型只见候选集，**闭包边界内生成**。

**输出**（单个 JSON packet，一次生成；`enable_thinking=false` 纯 JSON，029 nav_http 模式）：

```json
{
  "plan": {
    "intent": "temporal_state_tracking | fact_lookup | multi_hop | preference_recall",
    "memory_types": ["workplace", "date", "role"],
    "temporal_scope": "current | recent | historical | any",
    "evidence_requirement": "compare newer and older workplace memories; expose latest valid state",
    "target_count": 2
  },
  "trace": [
    {"role": "old_state",  "cited_ids": ["candidate-123"], "statement": "m1 记录 Daniel 早期在 Northbank Labs", "next_relation": "superseded_by"},
    {"role": "update",     "cited_ids": ["candidate-456"], "statement": "m2 说明 2024-01 转到 Riverbend Robotics", "next_relation": "confirmed_by"},
    {"role": "confirmation", "cited_ids": ["candidate-789"], "statement": "m3 确认转职后角色活跃", "next_relation": "resolution"}
  ],
  "actions": [
    {"action": "DROP",   "cited_ids": ["candidate-123"], "rationale": "旧工作被后来跳槽覆盖"},
    {"action": "KEEP",   "cited_ids": ["candidate-456"], "rationale": "直接说新工作"},
    {"action": "REFINE", "cited_ids": ["candidate-789"], "rationale": "压缩为支持确认"},
    {"action": "DROP",   "cited_ids": ["candidate-111"], "rationale": "聚会≠现在的工作"}
  ],
  "evidence": [
    {"text": "Daniel 现在在 Riverbend Robotics 工作。", "cited_ids": ["candidate-456"]}
  ]
}
```

**动作词表**（MemChain `V_act`，闭包内验证）：`KEEP` / `DROP` / `MERGE` / `REFINE` / `ADD`
- KEEP/MERGE/REFINE/ADD 产出最终证据；DROP 不产出。
- ADD 仅允许从 cited 候选可验证的派生（日期/时长/比较/跨记录关系）。
- 非法动作 / 非法 ID / 结构缺字段 → 走 fail-closed。

## Fail-Closed 门（`trace_gate.go`，纯 Go 确定性，离线单测）

按序降级（宪法 V），绝不产生空 answerer 上下文：

1. **解析**：`extractPacketJSON`（遍历候选 JSON，029 `extractNavJSONCandidates` 模式）→ 失败重试一次（扩展 budget）→ 仍失败 → `fallback`。
2. **闭包校验**：所有 `cited_ids ⊆ IDs(C_q)`；越界引用 → 丢弃该条 evidence/action；丢弃后 E 空 → `fallback`。
3. **E 非空校验**：`evidence` 为空 → `fallback`。
4. **回溯校验**：E 内每条 evidence MUST 有 ≥1 个 `cited_ids` 落在某 trace step 的 `cited_ids`（可回溯）→ 不满足丢弃该条。
5. `fallback` → 回退 US1 装配（现有 `buildAnswerPrompt`）。

**门状态记录**（`FailClosedGate`，data-model.md）：`valid` / `invalid_citation` / `parse_failed` / `fallback`。

## 最终证据 E → answerer

- answerer 上下文 = 仅 E（+ question + system），**不含** plan/trace/actions 原文（MemChain 只暴露 E）。
- E 的 token 记账走 US1 装配的精确 tokenizer；E 总 token ≤ cap。
- 可选：把 `cited_ids` 映射回候选文本作为「可校验附录」（grounding），默认关（保持 E 紧凑）。

## 验收

1. sidecar 未配置（默认）→ 装配路径零行为变化（parity）。
2. 非法 ID（引用候选集外）→ 丢弃 → E 空 → `fallback`（离线单测，stub provider）。
3. 合法 packet → answerer 上下文 = 仅 E，每 evidence 可回溯到 trace/candidates（离线断言）。
4. 解析失败 → 重试一次 → 再失败 `fallback`（stub 断言）。
5. e2e：`--trace-mediation` arm 配对 majority ≥ 基线（008 铁律，US2 verdict）。
