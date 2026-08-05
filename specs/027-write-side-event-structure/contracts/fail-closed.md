# fail-closed 校验契约（027）

**用途**:保证本地 LLM 抽取的**输出规范性不可控**（023 教训）不污染 store。任何抽取
失败都退回原文 chunk 路径，使 store 永不包含非法/无来源的 event（FR-005）。

## 两层校验

### 1. schema 校验（确定性，纯 Go）

逐条判定（完整规则见 [event-contract.md](event-contract.md)）:

- JSON 可解析 + 顶层 object
- required 字段齐全且类型正确
- source_ledger_ids 全部存在于 ledger
- fact_entries 非空、text 长度有界
- relation_type 在枚举内
- 总体 token 有界

**失败 → 整条消息退回存原文 chunk**，不产生 event，记录失败原因。

### 2. 幻觉审计（阶段 1 抽样，人审）

- 从 event 投影抽样（每 conversation 若干条）
- 人审判定：fact/relation 是否有对话中不存在的陈述（StructMem fidelity audit 式）
- 产出判定统计：抽取数 / 失败率 / 疑似幻觉率 / 有来源支撑率（grounded 率）
- 门：幻觉率 > 5% → 升级抽取（换 35B 或改 prompt 收紧约束）；否则维持 7B

## 判定统计（FR-006，可审计）

每次构建记录:

- 消息总数 / 抽取成功数 / 抽取失败数（含失败原因分类）
- fail-closed 退回的 ledger id 列表
- 每 event 的 grounded 率（source 支撑比例）
- 抽样幻觉判定结果

## 降级路径（无 LLM 端点）

- event 抽取未配置或端点不可达 → **零行为变化**退回现有 chunk 路径（FR-004/005）
- store 中不残留半成品 event；无 event 时检索/渲染与现状一致
