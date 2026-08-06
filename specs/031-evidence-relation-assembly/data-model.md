# Data Model: 读侧证据关联装配（Evidence Relation Assembly）

**Phase 1 输出** | 2026-08-06 | 实体与关系定义（实现无关）。

## 实体

### EvidenceUnit（复用 030，不变）

候选证据集合中的单个单元（chunk 或 fact），是关系计算的节点。030 已定义：

| 字段 | 类型 | 说明 |
|---|---|---|
| `SourceID` | string | 证据来源名（chunk 名或 fact 名），关系引用的锚 |
| `Text` | string | 证据内容 |
| `Kind` | chunk \| fact \| consolidated | 证据类型 |
| `Score` | float | 检索分 |
| `EventDate` | string (可选) | 归一化日期键 `YYYY-MM-DD`（来自 `memory.Result.EventDate`） |
| `TokenCount` | int | 估计 token 数（逐条记账） |

### RelationEdge（新）

两证据单元之间的显式关系边。

| 字段 | 类型 | 说明 |
|---|---|---|
| `From` | string | 源证据 `SourceID` |
| `To` | string | 目标证据 `SourceID` |
| `Type` | related_to \| temporal_next \| caused_by | 关系类型 |
| `Evidence` | string | 依据：共享实体名 / 日期对（`from→to`）/ 命中因果指示词 |
| `Rank` | int | 优先级（同类型多条边时的展示序，越小越前） |

约束：
- `From ≠ To`（无自环）
- 同 `(From, To)` 至多一种 `Type`（不重复建边）
- 单证据出边数量 cap：`related_to ≤ 4`、`temporal_next ≤ 1`、`caused_by ≤ 2`（R-4）
- 有向：`temporal_next`（早→晚）、`caused_by`（因→果）；`related_to` 无向（按 `From` 侧记账）

### StructuralContextBlock（新）

装配器产出的附加段落，按类别组织关系边，注入 answerer 用户 prompt。

| 字段 | 类型 | 说明 |
|---|---|---|
| `Category` | temporal \| multi-hop | 生效类别（其余类别不产出块） |
| `Edges` | []RelationEdge | 关系边集合（按类别筛选后的） |
| `Text` | string | 渲染后的结构上下文块（多行，每行一条关系 + 依据） |
| `TokenCount` | int | 块 token 数（计入 030 exact-token 记账） |

## 关系

### 计算关系

| 关系 | 输入 | 规则 | 依据 |
|---|---|---|---|
| `related_to` | 两证据 `Text` 的实体集合 | 共享实体 ≥ 1 即建边，按共享实体数降序 | 共享实体名列表 |
| `temporal_next` | 两证据 `EventDate` | 同类别内按日期键排序，`A.date < B.date` 且 B 是 A 的最近后继 | 日期对 `from→to` |
| `caused_by` | 两证据 `Text` | 含因果指示词 **且** 共享核心实体（R-3 双条件） | 命中指示词 |

### 类别映射（R-6）

| 类别 | 生效关系 | 组织方式 |
|---|---|---|
| multi-hop | `related_to` + `caused_by` | 按链序组织（共享实体/因果串联） |
| temporal | `temporal_next` | 按时间后继链组织 |
| single-hop / generic / open-domain | 无 | 不产出块（fail-soft） |

## 状态与生命周期

- **无持久化**：关系边是装配时的临时计算产物，不写入 store、不跨问答保留（避免写侧重构红线）。每次问答独立计算。
- **生命周期**：检索 → 030 排序 → 031 关系计算 → 结构上下文注入 → 作答。关系计算在 cap 截断**之前**对完整有序候选集运行，注入时随截断的证据一起丢弃。

## 校验规则（对应 FR）

- FR-001：关系计算离线、确定性（同输入同输出）；三类关系仅用 `Text`/`EventDate` 与内置词典
- FR-002：块不修改证据内容与排序（纯附加元数据）
- FR-003：类别映射如上（temporal≠graph）
- FR-005：边 cap K=4；块 token 计入 cap
- FR-006：关系为空 → 不产出块 → 走 030 路径（fail-soft）
