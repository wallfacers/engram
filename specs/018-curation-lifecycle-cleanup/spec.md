# Feature Specification: Curation 生命周期与记忆索引完整性

**Feature Branch**: `018-curation-lifecycle-cleanup`

**Created**: 2026-07-28

**Status**: Draft

**Input**: User description: "显式开启 curation；MCP 异步持久运行，CLI 同步一次性运行；删除与合并记忆时清理 alias、fact-query 等遗留索引。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 显式开启持久异步 Curation (Priority: P1)

运行 MCP 服务的管理员可以显式开启 curation。开启后，每个已打开的
namespace 都拥有独立的后台维护生命周期；成功写入或批量摄取记忆后，请求无需等待
维护完成即可返回，后台维护会按触发信号和周期检查安全地合并近重复记忆、淘汰低价值
记忆或处理冲突。未显式开启时，服务行为与现有版本一致。

**Why this priority**: curation 引擎目前虽然存在，但正常 MCP 使用路径从未启动它。
这使用户误以为系统会自动整理记忆，实际只有精确内容去重。显式开关既恢复完整能力，
也避免升级后意外产生模型调用或破坏性维护。

**Independent Test**: 分别以默认配置、显式开启配置启动服务，并在两个 namespace
写入可触发维护的记忆。可以独立验证默认配置无后台维护，开启后写请求先返回、维护随后
发生，而且两个 namespace 的维护和关闭互不影响。

**Acceptance Scenarios**:

1. **Given** 管理员未配置 curation，**When** 服务启动并处理记忆写入，
   **Then** 不启动后台维护、不产生 curation 模型调用，现有写入与检索行为保持不变。
2. **Given** 管理员显式开启 curation 且所需模型能力可用，**When** 一个 namespace
   首次被打开，**Then** 该 namespace 启动一个且仅一个后台维护生命周期。
3. **Given** curation 已开启，**When** 单条写入或一次批量摄取成功完成，
   **Then** 请求在维护 pass 完成前即可返回，并向同 namespace 的后台维护发出
   非阻塞、可合并的通知。
4. **Given** curation 已开启，**When** 多个 namespace 同时收到写入，
   **Then** 每个 namespace 只维护自己的记忆，不读取、修改或阻塞其他 namespace。
5. **Given** 管理员显式开启 curation 但缺少所需模型能力，**When** 服务启动，
   **Then** 启动被明确拒绝并返回可操作的配置错误，不能显示开启却静默不工作。
6. **Given** 后台维护正在运行，**When** namespace 被关闭、淘汰或整个服务退出，
   **Then** 系统先停止并等待该维护生命周期结束，再释放它依赖的资源。
7. **Given** judge 正在处理旧快照，**When** 同名记忆在等待期间被重写或变为 pinned，
   **Then** 旧决策不得删除、合并或覆盖该新 revision。

---

### User Story 2 - 删除与合并后保持存储完整 (Priority: P1)

用户删除记忆或由 curation 合并记忆后，与被删除内容关联的搜索表示、实体关联、
事件别名、生成式查询及失效引用都会在同一操作中清除。无关记忆与共享关系不会被误删，
失败时也不会留下只清了一半的状态。

**Why this priority**: 当前删除路径只覆盖早期的部分派生数据，后续增加的别名与
事实查询索引可能成为孤儿。孤儿索引会污染检索结果、浪费存储，并使删除承诺不完整。

**Independent Test**: 预置一组包含正文搜索表示、别名表示、查询表示、实体关系、
事件别名、事实查询、替代关系和无关对照数据的记忆；分别执行删除与合并后，独立核对
目标相关数据为零、存活目标可重建、共享或无关数据保持不变，并注入失败验证整体回滚。

**Acceptance Scenarios**:

1. **Given** 一条记忆拥有正文、别名、查询、实体和事件等关联索引，
   **When** 用户删除该记忆，**Then** 可归属于该记忆的所有关联索引在操作返回前
   一并消失。
2. **Given** 其他记忆把待删除记忆标记为替代目标，**When** 目标被删除，
   **Then** 这些已失效的反向引用被清空，而其他有效替代关系保持不变。
3. **Given** 某实体关系的一端已不再被任何存活记忆引用，**When** 删除或合并完成，
   **Then** 该失去端点的关系被清除；仍被任意存活记忆共享的关系不得删除。
4. **Given** 多条源记忆被合并到一个存活目标，**When** 合并成功，
   **Then** 被消费源的关联数据全部清除，目标的旧派生索引失效并可重新生成，
   目标本身及指向该存活目标的有效引用仍然存在。
5. **Given** 删除或合并的任一步骤失败，**When** 操作结束，
   **Then** 基础记忆和所有关联数据一起回滚，不出现部分删除或部分更新。
6. **Given** 存在无关记忆、无关索引和共享实体，**When** 删除或合并另一条记忆，
   **Then** 无关数据逐项保持不变。
7. **Given** embedding 已读取记忆但仍在模型调用中，**When** Delete/Merge 完成清理，
   **Then** 迟到的 embedding 不得重新创建 orphan 或发布旧 revision 的 vector。

---

### User Story 3 - 显式执行一次 CLI Curation (Priority: P2)

使用命令行进行本地维护的操作者可以运行一次显式 curation 命令。命令在当前
namespace 同步完成一趟维护或在限定时间内明确结束，适合脚本、调试和手工维护；
普通写入与摄取命令不会暗中执行 curation。

**Why this priority**: CLI 是一次性进程，启动后台 worker 后立即退出既不可靠也难以
观察。同步单次命令给操作者确定的完成边界，同时避免给常用写入命令增加一次模型调用
或全库扫描延迟。

**Independent Test**: 在具备模型能力与缺少模型能力两种环境中运行显式命令，
验证前者在当前 namespace 完成一趟并返回，后者给出能力错误；再运行普通写入和摄取，
验证它们不触发 curation。

**Acceptance Scenarios**:

1. **Given** 所需模型能力可用，**When** 操作者对指定 namespace 运行一次
   `curate`，**Then** 命令同步等待一趟维护结束，并只处理该 namespace。
2. **Given** 一趟维护未能在两分钟内结束，**When** 达到时间上限，
   **Then** 命令停止该趟维护、不得继续应用迟到决策，并以失败状态报告超时。
3. **Given** 缺少所需模型能力，**When** 操作者运行 `curate`，
   **Then** 命令在修改记忆前返回明确的能力错误和失败状态。
4. **Given** 模型返回无效、矛盾或不可执行的维护决策，**When** 命令处理该决策，
   **Then** 不修改记忆，并以保守结果报告该趟结束，不虚报发生过合并或淘汰。
5. **Given** 操作者运行普通 `add` 或 `ingest`，**When** 命令完成，
   **Then** 不自动执行 curation，其延迟和模型调用次数不因本功能增加。

### Edge Cases

- 高频连续写入产生的通知必须可以合并，不能为每条记忆无界排队维护任务。
- 周期检查与写入通知同时发生时，同一 namespace 不能并发运行两趟 curation。
- 同一 namespace 被多个进程打开时，任一时刻最多一个进程执行实际维护。
- curation 在候选为空、未达到水位、记忆被固定或模型拒绝行动时，应安全结束且不改库。
- 模型调用、维护决策解析或单个应用动作失败时，原始写请求不得因此失败。
- namespace 在维护 pass、周期等待或通知等待期间关闭时，都必须可确定性结束。
- 删除不存在的记忆保持现有“不存在”语义，且不得清理名称相近的索引。
- 删除名称已经被用作别名或查询表示后缀的记忆时，只删除能明确归属于目标的表示。
- 合并目标同时也出现在源集合时，目标必须存活且只失效其旧派生索引。
- 删除替代链中间或末端记忆时，只清除指向已删除目标的引用，不重写剩余链含义。
- 实体关系由多条记忆共享时，只有端点完全失去存活引用后才能清除关系。
- 关联数据清理失败、服务关闭超时或模型超时都不得留下半提交的可见状态。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 提供默认关闭的 MCP curation 配置，并允许管理员通过受支持的
  启动配置显式开启；直接启动参数 MUST 覆盖同名环境配置。
- **FR-002**: 显式开启 MCP curation 时，系统 MUST 在接受请求前验证所需模型能力；
  能力不完整 MUST 阻止启动并给出可操作错误。
- **FR-003**: 开启后，系统 MUST 为每个已打开 namespace 维护一个且仅一个独立的
  异步 curation 生命周期。
- **FR-004**: 单条写入与批量摄取成功后 MUST 通知对应 namespace 的 curation；
  通知 MUST 非阻塞且 MUST 合并尚未消费的重复通知。
- **FR-005**: MCP curation MUST 同时支持写入触发和至少每 30 分钟一次的周期检查，
  但 MUST NOT 在同一 namespace 并发执行多趟维护。
- **FR-006**: namespace 关闭、淘汰或服务退出时，系统 MUST 停止并等待其 curation
  生命周期结束，然后才能释放该生命周期所使用的资源。
- **FR-007**: 系统 MUST 提供一次性 `curate` CLI 操作；该操作 MUST 对所选
  namespace 同步执行恰好一趟维护，且 MUST NOT 启动持久后台进程。
- **FR-008**: CLI curation MUST 以两分钟为默认整趟上限；超时 MUST 取消后续处理、
  禁止应用迟到决策并以失败状态结束。
- **FR-009**: 普通 CLI 写入和摄取 MUST NOT 自动触发 curation。
- **FR-010**: curation 的无效或不可执行决策 MUST 保守地不修改记忆；后台维护错误
  MUST NOT 反向导致已经成功的原始写请求失败。
- **FR-011**: 同一 namespace 被多个进程使用时，系统 MUST 保证同一时刻最多一个
  执行者应用 curation 决策。
- **FR-012**: 删除一条记忆时，系统 MUST 同步删除可归属于它的正文、别名和生成查询
  搜索表示，以及实体关联、事件别名和事实查询关联。
- **FR-013**: 真正删除记忆时，系统 MUST 清空其他存活记忆指向该已删除名称的失效
  替代引用。
- **FR-014**: 删除或合并移除实体引用后，系统 MUST 清除任一端已无存活记忆引用的
  实体关系，并 MUST 保留端点仍被存活记忆共享的关系。
- **FR-015**: 合并 MUST 删除所有被消费源的关联数据，并 MUST 使存活目标的旧派生
  索引失效，以便基于新内容重建。
- **FR-016**: 合并 MUST 保留存活目标本身以及其他记忆指向该存活目标的有效替代引用。
- **FR-017**: 删除、合并及其关联清理 MUST 构成单个原子操作；任一步失败时全部回滚。
- **FR-018**: 所有关联清理 MUST 严格限制在目标及已确认失效的数据，不得改变无关
  记忆、共享实体或仍有效关系。
- **FR-019**: 默认关闭 curation 时，现有 MCP/CLI 命令契约、工具清单、写入路径和
  检索结果 MUST 保持兼容。
- **FR-020**: 服务状态输出 MUST 明确显示 curation 已开启或关闭，但 MUST NOT
  输出模型凭据或其他秘密。
- **FR-021**: 后台 curation 在应用 delete、merge 或 supersede decision 时 MUST 以
  judge 前观察到的 entry revision 做原子重验证；任一 loser/winner/source/target
  被重写、重建或变为 pinned 时，整个对应 action MUST 保守跳过。
- **FR-022**: 异步 embedding MUST 仅在 owner entry 仍存在且 revision 未变化时原子
  写入 vector；Delete/Merge 返回后不得被迟到任务重新制造 orphan vector。
- **FR-023**: namespace 的 pass 完成边界 MUST 包含 heartbeat goroutine；LRU 等待一个
  namespace 关闭时 MUST NOT 持有阻塞其他 namespace 的 registry 全局锁。
- **FR-024**: 同步 CLI MUST 使用 pass 的显式完成/取消结果，不得仅在返回后读取
  context 状态而把已提交 pass 误报为超时或取消；不完整 LLM 配置仍属于 capability
  错误。
- **FR-025**: `memory_entries` MUST 持久化数据库维护的单调 revision；同一 name 的
  每次状态变更即使复用相同 `updated_at` 也 MUST 推进 revision。后台 CAS MUST 使用
  `id + revision`，不得把调用方可控或可能同微秒重复的时间戳当作版本令牌。

### Key Entities

- **Curation 配置**: 管理员对持久服务作出的显式授权，记录开启状态并依赖可用的
  模型能力；默认状态为关闭。
- **Namespace 维护生命周期**: 一个 namespace 独立拥有的后台维护状态，包含写入
  通知、周期检查、当前 pass、取消与完成边界。
- **Curation Pass**: 对单个 namespace 执行的一趟候选发现、保守决策和原子应用；
  可因无候选、能力错误、超时或取消而不产生修改。
- **记忆条目**: 用户可写入、检索、删除或参与合并的基础事实单元，可被固定、替代，
  并拥有多类关联索引；其持久化 revision 是异步维护的并发边界。
- **关联索引**: 可明确归属于某条记忆的搜索表示、实体关联、事件别名或生成查询；
  生命周期不得长于其归属的已删除内容。
- **实体关系**: 可由多条记忆共同支撑的跨实体关系；只有端点完全失去存活引用时才
  能被清除。
- **替代引用**: 一条存活记忆指向另一条记忆名称的关系；目标被删除后失效，目标仍
  存活时必须保留。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 在默认配置的全部兼容性测试中，后台 curation 生命周期和 curation
  模型调用次数均为 **0**，现有命令与工具契约变更数为 **0**。
- **SC-002**: 显式开启后的 100 次成功写入测试中，写请求均在对应 curation pass
  完成前返回；未消费通知的最大积压量始终不超过 **1**。
- **SC-003**: 两个或更多 namespace 并发运行的隔离测试中，跨 namespace 读取、
  修改或相互关闭影响的次数为 **0**。
- **SC-004**: 100 次启动、重复启动、关闭及淘汰生命周期测试中，每个 namespace
  同时运行的后台维护循环不超过 **1**，资源释放后的后台访问次数为 **0**。
- **SC-005**: CLI 单次维护在正常情况下完整结束；阻塞情况下从开始到报告超时不超过
  **两分钟加 5 秒清理余量**，超时后的迟到修改次数为 **0**。
- **SC-006**: 覆盖删除与合并的全部目标场景中，可归属于已删除内容的残留关联索引
  数为 **0**，失效替代引用数为 **0**。
- **SC-007**: 覆盖无关记忆、共享实体和存活合并目标的全部测试中，误删除或误清空的
  有效数据数为 **0**。
- **SC-008**: 对清理过程中每个可注入失败点执行测试时，可观察到的部分提交次数为
  **0**，操作前后的数据要么全部保留、要么全部完成预期变更。
- **SC-009**: 本功能的存储、提取、检索和 curation 相关确定性回归门全部通过，并在
  合并前完成与当前基线同口径的可比 LoCoMo 评测；若任一回归失败或评测显著回退，
  本功能不得标记为完成或合并。
- **SC-010**: 并发 rewrite/pin、迟到 embedding、heartbeat join、LRU 慢关闭和
  deadline-at-commit 的确定性测试中，旧数据误删、orphan 重建、资源释放后访问、
  跨 namespace 锁阻塞和已提交误报次数均为 **0**。
- **SC-011**: 在 loser、winner、merge source、delete target 与 embedding owner
  复用完全相同 `updated_at` 的并发测试中，过期 decision/vector 成功提交次数为
  **0**，且每次 entry 状态变更后的 revision 均严格增加。

## Assumptions

- curation 继续复用项目现有的候选评分、模型判断、租约和保守应用规则；本 feature
  不改变合并、淘汰或冲突处理算法。
- MCP 的开启配置作用于该服务实例随后打开的全部 namespace；本 feature 不提供
  namespace 级差异化开关。
- 持久 MCP 模式使用项目既有的生产水位与预算默认值；本 feature 只暴露总开关，不
  暴露全部调参项。
- CLI 命令本身即视为单次 curation 的显式授权，不要求额外开启开关。
- 普通 CLI 保持一次性进程模型；不新增 daemon、持久任务队列或后台子进程。
- 项目现有模型配置是 curation 的能力来源；不在本 feature 中引入新的 provider。
- 两分钟是 MCP 单趟维护与 CLI 单次维护的共同默认安全上限。
- 对已成功进入 pass 但没有可执行决策的 CLI 运行，保守 no-op 视为正常完成；
  配置缺失、取消和超时视为失败。
- 关联索引继续由应用维护生命周期完整性；本 feature 不以新增数据库级外键为前提。
- 本 feature 新增 schema v6 的 `memory_entries.revision`，但不增加外键、不执行历史
  孤儿 sweep；已有 entry 升级时从 revision 1 开始。
- 本 feature 不自动创建新的事实查询，也不改变记忆提取、embedding 或检索排序算法。
- 关联清理仅发生在显式删除或合并路径；不会为了历史孤儿数据增加全库迁移或启动扫描。
- LoCoMo 评测仍遵守显式成本授权；未取得授权时可以完成本地实现与免费验证，但 feature
  必须保持未完成、不可合并，不能用确定性 parity 代替宪法 IV 的最终可比评测。
