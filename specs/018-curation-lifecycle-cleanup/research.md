# Phase 0 Research: Curation 生命周期与记忆索引完整性

## R1 — 自动 Curation 的授权模型

**Decision**: MCP 使用全服务实例的显式开关，默认关闭；开启后应用于随后打开的全部
namespace。开启但没有完整 LLM 能力时启动失败。

**Rationale**: curation 可能发起模型调用并执行 merge/evict，升级后静默开启会改变
成本和数据。显式开关能保持旧版默认行为；启动失败比“开关为真但 worker inert”更诚实。

**Alternatives considered**:

- 默认开启：拒绝，破坏离线默认和升级兼容。
- 开启但无模型时静默 inert：拒绝，运行状态与声明不一致。
- namespace 级动态开关：暂不采用，会扩大配置、热更新和状态管理范围。

## R2 — MCP 异步与 CLI 同步的执行形态

**Decision**: MCP 为每个 namespace 运行持久异步 worker；CLI 只提供显式、同步的一次
`curate`，普通 `add`/`ingest` 不自动触发。

**Rationale**: MCP 是长生命周期服务，适合接收去抖写入通知和周期检查；CLI 是一次性
进程，后台 goroutine 会随进程退出，无法给出完成保证。同步命令适合脚本和手工维护，
且不把模型耗时隐藏进常用命令。

**Alternatives considered**:

- CLI 每次写后同步 curate：拒绝，会给常用写入增加一次模型调用和全库扫描。
- CLI 启动异步任务后退出：拒绝，任务不持久、结果不可观察。
- 新增 CLI daemon/任务队列：拒绝，超出当前一次性适配器范围。

## R3 — 默认水位与超时

**Decision**: 两个适配器复用一份引擎默认配置：entry 80、间隔 30 分钟、lease 60 秒、
manifest 2000 字符、候选 20、snippet 1200 字符、权重 1/1/.5/.5、现有 entry budgets，
整趟 timeout 2 分钟。

**Rationale**: 这些水位来自 curation 原来源项目的已验证运行默认，避免两个适配器
漂移。provider 当前可能使用无 HTTP client timeout 的长连接，pass context deadline
必须成为整趟安全边界。

**Alternatives considered**:

- 暴露全部调参项：暂不采用，首版只需要可靠显式开关。
- 只在 CLI 外层 timeout：拒绝，MCP 后台 pass 同样可能阻塞关闭。
- 强制 provider 全局 HTTP timeout：拒绝，会改变所有 extraction/answer 调用语义。

## R4 — Worker 的停止与等待

**Decision**: `Start` 对单个 Worker 幂等且只允许一个生命周期；新增 `Wait`。namespace
关闭顺序为 cancel → Wait → embedder drain → store close。

**Rationale**: 仅 cancel 不代表 goroutine 已退出。若先关数据库，仍在 scan/judge/apply
的 worker 会访问已关闭资源。一次性启动门和 wait group 能给适配器稳定的完成边界，
同时不把宿主类型带进引擎。

**Alternatives considered**:

- cancel 后立即 close DB：拒绝，存在后台 use-after-close。
- 每次 `Start` 启动一个 loop：拒绝，会重复周期 pass 并制造并发 lease 竞争。
- 在适配器用 sleep 猜测结束：拒绝，不确定且不可测试。

## R5 — RunPass 错误语义与 CLI 退出状态

**Decision**: 保留 `RunPass` 的 fail-safe/no-op 语义。CLI 将“缺少模型能力”“调用者取消”
和“两分钟 deadline”报告为非零；模型返回错误或无效决策由 worker WARN 并保守 no-op，
CLI 只报告 pass 已结束，不声称发生 merge/evict。

**Rationale**: 改成向上抛出所有模型/解析错误会让后台维护错误污染成功写入，与现有
curation 安全契约冲突。CLI 可通过自己的 deadline context 准确识别取消/超时；要报告
精确动作计数则需要扩大公开结果契约，不是本次修复所需。

**Alternatives considered**:

- 所有 judge/provider 错误都令 CLI 失败：暂不采用，会改变现有 fail-safe 定义。
- 无论超时都返回成功：拒绝，脚本无法判断明确的运行边界是否满足。
- 新增完整 PassResult/动作统计：暂不采用，属于可观测性增强，不是接线与完整性修复。

## R6 — Side-table 清理分层

**Decision**: 将清理拆成“任意内容变化后的派生索引失效”和“基础 entry 真正删除后的
反向引用清理”。两者都复用调用方事务。

**Rationale**: Delete 和 Merge 目标有不同生命周期。合并目标仍存活，只应失效其旧
embedding/entities/aliases/queries；若同时清除其他 entry 指向它的 `superseded_by`，
会破坏仍有效关系。被消费 source 才需要完整删除语义。

**Alternatives considered**:

- 继续扩大单一 `deleteDerivedTx`：拒绝，无法表达存活目标与真删除的区别。
- 清理放在事务外：拒绝，失败会暴露半清状态。
- 依赖数据库 FK cascade：拒绝，现有逻辑名称引用和 shadow names 不具备直接 FK 形状，
  revision migration 也不改变这些 side-table ownership 语义。

## R7 — Shadow vector 的归属规则

**Decision**: 删除 name 的向量候选集合固定为精确名称
`{name, name + "#alias", name + "#query"}`，不使用前缀或模糊匹配。若一个 shadow
候选名称同时也是存活 base entry 的真实 name，则保留该共享 key，避免删除无关 entry
的唯一 vector。

**Rationale**: 这是 embedder/retriever 已冻结的 shadow-name 契约。精确集合既能清理
遗漏的 alias/query vector，也不会误删名称相近 entry 的向量。现有表没有 owner 列，
所以真实 entry 名与 shadow key 碰撞时无法证明该 row 只属于已删 owner；保留满足
FR-018 的“不误删”优先级。当前命名协议本身不在本 feature 中迁移。

**Alternatives considered**:

- `LIKE name || '#%'`：拒绝，可能删除未来或无关 suffix。
- 只删正文 vector：拒绝，正是现有孤儿问题。
- 改为独立 source-name 列：暂不采用，需要 schema 和兼容迁移。

## R8 — 共享实体边的清理

**Decision**: 删除实体索引时，只检查触及本次被移除 entity 的边；删除“任一端点在
排除当前 entry 后不再由任何存活 `memory_entities.entity_norm` 引用”的 edge。
仍有引用的端点和边保留，与本次 entry 无关的历史孤儿不顺带 sweep。

**Rationale**: edge 权重是跨 entry 累积值，没有每条 entry 的贡献明细，无法在删除一条
记忆时精确减权。端点无引用时整边一定不可达且可安全删除；仍有引用时保留是唯一不误删
共享信息的保守策略。

**Alternatives considered**:

- 删除所有触及被删 entry 实体的边：拒绝，会误删其他 entry 共享的边。
- 按删除条数扣 weight：拒绝，现有 schema 无 provenance，无法正确计算。
- 永不清 edge：拒绝，会保留明确不可达的孤儿关系。

## R9 — 历史孤儿与 schema

**Decision**: 本功能修正未来 Delete/Merge 行为，不新增启动扫描、迁移或一次性历史修复。

**Rationale**: 用户要求的根因是生命周期不完整。全库历史清理需要定义孤儿识别、
备份和恢复契约，会扩大风险；当前清理可以在既有事务和 schema 内完成。

**Alternatives considered**:

- 启动时自动 sweep：拒绝，增加默认启动延迟和隐式写操作。
- 新增 FK migration：拒绝，shadow vector 与逻辑替代引用仍需应用语义，不能仅靠 FK。
- 单独 repair 命令：可作为后续 feature，不阻塞本修复。

## R10 — 回归门与评测成本

**Decision**: 实现后强制运行 storage/curation/MCP/CLI 测试、全仓 build/test/vet、
确定性 retrieval parity 与 signal degradation；合并前还必须在显式成本授权下运行
与当前基线同口径的可比 LoCoMo。未取得授权时允许完成本地实现，但 feature 保持未完成、
不可合并。

**Rationale**: 变更触及 storage/curation，宪法 IV 明确要求可比评测。默认关闭且
检索算法不变意味着确定性 parity 仍是快速、直接的前置证据，但不能替代最终的基准门。
成本授权只控制何时执行付费评测，不降低或取消合并标准。

**Alternatives considered**:

- 跳过可比评测、只跑 parity：拒绝，违反宪法。
- 未经授权直接跑付费全量评测：拒绝；保持任务未完成并在执行前请求授权。

## R11 — 异步 destructive maintenance 的 revision 边界

**Decision**: schema v6 为 `memory_entries` 增加数据库维护的单调 `revision`。judge
前快照只用于 prompt；Delete 使用单语句 `id + revision` CAS，Merge 在同一事务内
重验证全部 source 和既有/new target，Supersede 在单条 UPDATE 中同时验证 loser 与
winner。任何 revision 变化或 pinned 状态变化都跳过整个 action。write-behind vector
使用 `INSERT ... SELECT ... WHERE owner id + revision` 原子落库。

**Rationale**: 开启后台 worker 后，LLM 等待窗口与正常写入并发。仅按 name 应用会让
旧 decision 删除新内容；仅在 vector Put 前做非事务查询仍有 TOCTOU，可能在 Delete
返回后复活 orphan。`updated_at` 可能由调用方提供或在同一微秒重复，不能作为版本；
持久化单调 revision 消除这个 ABA 窗口，且默认关闭路径不增加模型成本。

**Alternatives considered**:

- judge 返回后在事务外重新 `GetByName`：拒绝，检查与写入之间仍有竞态。
- curation 期间阻塞全部 namespace 写入：拒绝，会把模型延迟带入请求热路径。
- 继续使用 `id + updated_at`：拒绝；same-name conflict upsert 保留 ID，重复时间戳会
  让过期 delete/merge/vector CAS 误通过。
- 新增外键：拒绝；revision migration 足以解决并发版本边界，side-table 历史数据仍
  按当前精确清理契约维护。

## R12 — 关闭与同步完成的线性化

**Decision**: pass 返回前 join heartbeat；LRU 在全局锁内只 detach victim，锁外执行
cancel/Wait/drain/close，同 namespace Acquire 等待 closing marker。CLI 使用
`RunPassContext` 返回的取消结果，提交成功后才到达的 cancel 不反向改写为失败。

**Rationale**: `Wait` 必须代表不再访问 DB，而不只是主 loop 已退出。慢 provider 或
embedding drain 也不能占住 registry 全局锁。同步命令的退出状态必须以 pass 自己的
线性化点为准，不能用函数返回后的 context 快照猜测。
