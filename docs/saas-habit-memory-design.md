# SaaS「用户操作习惯记忆」MVP 产品与技术设计

> **状态**：设计草案，尚未立项、尚未实现。本文只定义产品与技术形态，不代表
> engram 已具备习惯抽取、主动召回或新鲜度一致性能力。
>
> **上位约束**：[能力认知与产品北极星](./capability-and-product-north-star.md)
> 第 5 节与 M3；[抽离背景](./background-extraction-from-workhorse-agent.md)；
> [新鲜度与召回问题](./memory-freshness-and-retrieval-policy.md)；宪法 I-V。
> 本设计不修改 `memory/`、`embedding/`、`provider/`、`store/`、`internal/`
> 或任何现有适配器。后续实现必须另行立项。

## 1. 命题与边界

### 1.1 产品命题

engram Habit Memory 是面向车机、手机等设备/应用厂商的**端侧用户操作习惯记忆层**：
宿主用统一事件契约上报“何时、做了什么、处于什么场景”，运行时在本地把重复行为沉淀为
偏好、作息模式和操作序列；当相同场景再次出现时，宿主用结构化上下文召回可解释的建议，
由宿主决定提示还是执行。

卖点不是“再建一个通用记忆云”，而是：

- **一天完成接线**：设备侧只需 `ingest(events)` 和 `recall(context)` 两个主动作；
- **数据默认不上云**：原始行为与习惯在设备或客户私有环境内处理，断网仍可工作；
- **能主动利用而不擅自行动**：场景事件触发 recall，但 engram 只返回建议，不直接控制车辆、
  应用或账户；
- **能解释、能改口**：结果携带支持次数、证据时间窗、状态和原因，习惯变化时保留历史版本，
  当前召回不让旧习惯冒充现状。

这里的“SaaS”指可标准化交付的 API、SDK、配置和商业支持，不等于把敏感数据集中到公有云。
MVP 的数据面是端侧嵌入库或本地 sidecar；云控制面、计费和舰队运营不在本期。

### 1.2 “主动”的准确含义

MVP 不常驻监听厂商系统，也不自行发起设备动作。“主动召回”是指宿主在
`vehicle.entered`、`screen.unlocked` 等场景出现时，以结构化上下文调用 recall，
无需用户先说一句自然语言，也不依赖模型临时决定要不要搜索记忆。

返回结果分为 `candidate`、`active`、`historical` 等状态。自动执行仍须经过宿主的本地授权、
动作白名单和风险策略。默认建议优先；涉及支付、通信、门锁、驾驶控制、健康诊断等高风险动作
永不因一条记忆自动执行。

### 1.3 方案取舍

| 方案 | 优点 | 硬伤 | 结论 |
|---|---|---|---|
| 把事件渲染成对话文本，调用现有 `pipeline.Ingest`，再把场景拼成 query 调 `Retriever.Search` | 几乎不用新契约，可快速演示 | 不理解频率、机会数、顺序和本地时区；文本相似不等于条件成立；容易把相关性写成偏好 | 只允许做 smoke demo，不叫 MVP |
| 在 HTTP/SaaS 适配器内自建事件库、统计器、冲突消解和条件排序 | 可绕开引擎改动 | 复制记忆算法，适配器变厚；SDK/HTTP/MCP 行为会分叉，违反宪法 II | 拒绝 |
| **端侧 Habit Runtime：引擎提供结构化习惯 pipeline 与 conditional recall，HTTP/SDK/MCP 只做协议和隔离** | 三个集成面同一语义；离线、可评测、可演进；与 temporal/supersedes 共用投入 | 需要新的公共引擎契约和专项评测 | **推荐；M3 的目标形态** |

推荐方案并不授权本次修改引擎。第 8 节列出的所有引擎增量均为**未立项**，必须先冻结契约、
建立评测门，再单独实现。

## 2. MVP 场景与数据流

### 2.1 两个锚点场景

| 场景 | 摄入事件 | 抽取结果 | 触发与利用 | 安全边界 |
|---|---|---|---|---|
| 车机：上车后常播放音乐 | `vehicle.entered`，随后两分钟内 `media.play`；按驾驶员 profile、车辆、时间段和媒体目标记录 | 例：“工作日早晨进入车辆后，用户通常继续播放音乐”，支持 12/15 次、最近一次为昨日 | 下次 `vehicle.entered` 时 recall；宿主可提示“继续播放”，经用户明确授权后才可自动恢复 | engram 不调用播放器；来电、导航播报、访客模式等抑制条件由宿主政策处理 |
| 手机：夜间高频刷抖音 | `app.foreground` / `app.background`，应用类别、会话时长、本地时区；默认不上传内容或精确位置 | 例：“过去 7 天中有 5 天在 00:00-02:00 使用短视频”，归入 `routine`，语义只陈述 observed pattern，不作医学判断 | `wellbeing.check` 或睡眠模式前 recall；宿主决定是否给健康提醒 | 不把相关性写成“用户想熬夜”；不做诊断、家长管控裁决或暗中跨应用跟踪 |

“一天接入”指完成 SDK/HTTP 接线、隔离和离线验收，不表示一天内就能可靠形成习惯。开发环境可回放
经过同意的历史 fixture；生产必须达到证据门槛后才把 `candidate` 提升为 `active`。

### 2.2 端到端数据流

```text
设备场景/操作
    │  vendor event mapping
    ▼
薄 SDK / 本地 HTTP / MCP
    │  校验、批量、幂等、tenant→namespace 映射
    ▼
HabitPipeline.IngestEvents（目标公共 API，当前不存在）
    │  规范化 → 去重 → 时间/会话排序 → 有界增量统计
    │  preference / routine / sequence 候选 → 证据门槛
    ▼
版本化 Habit Memory
    │  EntryStore 时间字段、来源、supersedes 原语；原始事件短期 TTL
    ▼
Retriever.RecallContext（目标公共 API，当前不存在）
    │  结构化条件匹配 + temporal + 当前状态 + 可选语义信号
    ▼
建议 + 置信度 + 原因 + 证据窗 + freshness watermark
    │
    ▼
宿主策略：忽略 / 提示 / 经授权自动执行，并把结果作为新事件反馈
```

### 2.3 数据分层

1. **Observation（短期行为事件）**：不可直接当偏好；以 `event_id` 幂等，按本地策略短期保留。
   MVP 建议默认保留 30 天且可配置/立即清除。精确保留期须在立项时按客户合规要求冻结。
2. **Candidate（候选习惯）**：由有界窗口内的支持次数、不同日期/会话数、机会数和反例生成，
   未过门槛不参与主动利用。
3. **Active habit（有效习惯）**：可参与场景 recall，包含条件、结果、统计、证据窗和版本状态。
4. **Historical habit（历史习惯）**：被新版本取代但不删除；仅在历史查询、审计或用户查看时返回。

原始事件量通常远大于记忆条目量，不能把无限遥测逐条永久塞进 engram。MVP 必须采用短期 observation
窗口和增量聚合，只把候选/有效/历史习惯作为长期记忆；这也是守住单 namespace 约 10 万条边界的前提。

## 3. 事件摄入契约草案

### 3.1 Habit-native HTTP

本地 sidecar 首选批量接口；单事件也作为一项 batch 发送。监听默认限于 loopback 或 Unix domain
socket，公网暴露必须由厂商自己的网关完成认证、限流和 TLS。

```http
POST /v1/habit-events:batch
Idempotency-Key: 01K0...
Content-Type: application/json
```

```json
{
  "user_id": "driver-42",
  "device_id": "head-unit-7",
  "scope": "device",
  "events": [
    {
      "event_id": "evt-20260722-0001",
      "occurred_at": "2026-07-22T08:03:11+08:00",
      "timezone": "Asia/Shanghai",
      "session_id": "trip-8848",
      "sequence": 1,
      "action": {
        "name": "vehicle.entered"
      },
      "context": {
        "scene": "vehicle",
        "profile": "driver",
        "day_type": "weekday"
      }
    },
    {
      "event_id": "evt-20260722-0002",
      "occurred_at": "2026-07-22T08:03:46+08:00",
      "timezone": "Asia/Shanghai",
      "session_id": "trip-8848",
      "sequence": 2,
      "action": {
        "name": "media.play",
        "target": "playlist:morning-mix"
      },
      "context": {
        "scene": "vehicle",
        "app": "com.vendor.media"
      }
    }
  ]
}
```

字段规则：

- `user_id`、`device_id` 是厂商域内稳定 ID，不直接成为文件名；适配器按第 7 节派生 namespace。
- `scope` 默认 `device`；只有用户明确同意跨设备共享时才可选 `user`。
- `event_id` 在 namespace 内唯一。重复 ID + 相同负载返回 duplicate；重复 ID + 不同负载只拒绝
  该事件，并在 `rejected[]` 返回 `idempotency_conflict`，不影响同批其他合法事件。
- `occurred_at` 必须是带偏移的 RFC 3339；`timezone` 使用 IANA 名称，用于夏令时和本地作息聚合。
- `session_id` 与 `sequence` 对操作序列必需；无序事件仍可参与单动作频率，但不得推导序列。
- `action.name` 采用小写点分命名；厂商扩展使用反向域名前缀。`context` 只允许有界标量、标量数组
  和经契约登记的键，拒绝任意大对象。
- 地点默认使用 `home`、`work`、`vehicle` 等端侧粗粒度标签；不要求原始 GPS。文本内容、通讯录、
  页面正文和密钥不得作为默认事件属性。

建议 MVP 限制每批最多 500 个事件、解压后最多 1 MiB。它是防滥用契约，不是吞吐 SLA；吞吐和
延迟须实测后再发布。

异步响应：

```json
{
  "receipt_id": "rcpt-01K0...",
  "accepted": 2,
  "duplicates": 0,
  "rejected": [],
  "state": "accepted",
  "accepted_at": "2026-07-22T08:03:47Z",
  "materialized_watermark": 9812
}
```

`202 Accepted` 只表示 observation 已持久化，不表示习惯已经可召回。调用方可查询：

```http
GET /v1/habit-ingests/rcpt-01K0...
```

返回 `accepted | materializing | materialized | partial | failed`、逐项拒绝原因和单调递增的
`materialized_watermark`。这使异步一致性窗口可见，而不是让调用方猜测。

### 3.2 错误与降级语义

| 情况 | 结果 |
|---|---|
| 批次合法，部分事件非法 | `202`，合法项入队，`rejected[]` 精确指出 index、event_id、code |
| 同一 event_id 负载冲突 | `202`，该项进入 `rejected[]` 且不覆盖旧事件，同批其他合法项继续 |
| 请求级 Idempotency-Key 被不同整批负载复用 | `409 idempotency_conflict`，整批不接收 |
| 批次超限 | `413 batch_too_large`，整批不接收 |
| 本地队列到达水位 | `429 backpressure` + `Retry-After`，不静默丢事件 |
| 可选 embedding 不可用 | 事件与确定性习惯抽取继续；receipt 标记语义信号降级 |
| 可选 LLM 不可用 | 规范化标签可能降级，频率/序列统计继续；不得将“未抽取”报成“无习惯” |

### 3.3 与当前公共 API 的实际映射

今天可以用公共 API 搭一个**不承诺习惯质量的 smoke slice**：

| Habit 字段/动作 | 当前可映射 | 还缺什么 |
|---|---|---|
| `event_id` | 作为稳定 `Entry.Name` 调 `EntryStore.Upsert`，获得基本幂等 | 没有“相同 ID 不同负载”的专用冲突语义 |
| `occurred_at` | `Entry.EventDate/EventStart/EventEnd` | 无 `received_at`、索引可见时刻或流水水位 |
| 动作与场景 | 写入 `Content/Trigger/Category/FactSource`，用 `PutEntities/PutAliases` 建辅助索引 | 没有结构化 action/context schema；编码到文本后无法可靠条件过滤 |
| 手工形成的习惯 | `EntryStore.Upsert` 可保存；`Supersede` 可非破坏标旧 | 没有支持数、机会数、置信度、证据范围和 typed condition 字段 |
| 对话事实 | `pipeline.Ingest(sessionDate, sourceSessionID, []Message)` 已有 | 输入限定 user/assistant 文本；不能从事件流计算频率、作息和序列 |

因此禁止把“事件转成一句对话”当正式实现。MVP adapter 只能调用完成立项后的公共
`HabitPipeline.IngestEvents`；不得直接读写引擎表，也不得在 HTTP/MCP 层复制抽取算法。

## 4. 习惯抽取设计

### 4.1 与对话事实抽取的差异

| 维度 | 当前对话事实 pipeline | 习惯 pipeline 需要 |
|---|---|---|
| 输入 | 一批 user/assistant 自然语言 | 高密度、结构化、可能乱序/重复的行为事件 |
| 信号 | 用户或助手明确陈述 | 多次观察后的统计规律；单次行为不能等同偏好 |
| 时间 | session date + 文本中的事件日期 | 精确发生时刻、本地时区、日期/会话跨度、周期窗口 |
| 关系 | 多数事实彼此独立，ADD-only | 条件→动作、动作序列、机会→选择、反例和取消动作 |
| 输出 | 自包含事实句、实体、时间、类别 | typed condition、action/sequence、support/opportunity、置信度、证据窗、状态 |
| 风险 | 漏事实或抽错事实 | 把偶然行为、系统默认或强制流程误判为用户意图 |
| 模型角色 | 一次 LLM 调用为主；无 LLM 时 pipeline 惰性 | 确定性统计必须离线可跑；LLM 仅可选地规范化未知标签或生成说明 |

### 4.2 MVP 的三类模式

每个模式必须满足“跨多个独立日期或会话”的证据要求；具体阈值是可评测配置，不能由模型随意决定。
立项时可从以下保守默认值起步，再用 fixture 和真实匿名回放校准：

- **偏好 `preference`**：在同一条件与可选集合中，某目标支持数至少 5、跨至少 3 天，选择占比
  至少 0.65。例如在车内主动播放时多数选择音乐而非播客。
- **作息/例程 `routine`**：场景发生后有界时间窗内重复出现同一动作，至少 3 个独立会话，条件概率
  至少 0.70。夜间使用采用“不同本地日期”计数，避免一次长会话刷高频次。
- **操作序列 `sequence`**：同一 session 中按 `sequence` 或发生时刻形成 2-3 步 n-gram，至少在
  3 个独立 session 重复，转移置信度至少 0.70。MVP 不挖掘任意长序列。

阈值以下保持 `candidate`，不参与主动召回。系统默认事件、企业策略强制动作、后台自动播放等应通过
`context.initiator = system | policy | user` 区分；只有用户发起或用户确认的行为进入偏好分子。

### 4.3 目标习惯记录

下面是产品契约中的逻辑模型，不是现有 `memory.Entry` 已有字段：

```json
{
  "habit_id": "hab-01K0...",
  "version": 3,
  "kind": "routine",
  "state": "active",
  "condition": {
    "scene": "vehicle",
    "event": "vehicle.entered",
    "day_type": "weekday",
    "local_time": { "start": "07:00", "end": "09:30" }
  },
  "outcome": {
    "actions": [
      { "name": "media.play", "target": "playlist:morning-mix" }
    ]
  },
  "statistics": {
    "support": 12,
    "opportunities": 15,
    "distinct_sessions": 12,
    "confidence": 0.8
  },
  "evidence_window": {
    "first_observed_at": "2026-07-01T00:03:12Z",
    "last_observed_at": "2026-07-21T23:58:02Z"
  },
  "provenance": {
    "sample_event_ids": ["evt-...", "evt-..."],
    "sample_truncated": true
  },
  "supersedes": "hab-previous",
  "updated_at": "2026-07-22T00:00:00Z"
}
```

证据 ID 只保留有界样本与聚合摘要，避免一个习惯无限增长；完整 observation 是否仍可审计取决于
本地 TTL。置信度表示观察支持度，不表示因果关系，也不应伪装成全局概率校准。

### 4.4 新 pipeline 的职责边界

目标公共 API 可命名为 `pipeline.HabitPipeline.IngestEvents`，具体 Go 类型须在后续 spec 冻结。
它负责：事件校验后的规范化、幂等、乱序容忍、窗口统计、候选状态机、版本写入、来源闭包和
materialization watermark。它不负责厂商认证、HTTP、动作执行或跨用户学习。

抽取顺序应是：

1. 纯规则规范化时间、session、动作和上下文；
2. 在固定事件窗口内做确定性计数、条件概率和短序列统计；
3. 应用最小支持数、不同日期/会话数、反例与用户反馈门槛；
4. 与同一 `subject + kind + condition slot` 的当前习惯比较：强化、保持、产生新版本或降为历史；
5. 可选本地 LLM 只处理厂商未登记动作的语义归一或可读说明，失败即跳过该增强；
6. 写入后推进 watermark，异步 embedding 完成状态另行可观测。

## 5. 条件召回与主动利用

### 5.1 Recall 契约草案

```http
POST /v1/habits:recall
Content-Type: application/json
```

```json
{
  "user_id": "driver-42",
  "device_id": "head-unit-7",
  "scope": "device",
  "at": "2026-07-23T08:04:00+08:00",
  "timezone": "Asia/Shanghai",
  "context": {
    "event": "vehicle.entered",
    "scene": "vehicle",
    "profile": "driver",
    "day_type": "weekday"
  },
  "kinds": ["preference", "routine", "sequence"],
  "limit": 3,
  "minimum_confidence": 0.7,
  "consistency": {
    "after_receipt_id": "rcpt-01K0...",
    "wait_ms": 150
  }
}
```

```json
{
  "results": [
    {
      "habit_id": "hab-01K0...",
      "kind": "routine",
      "state": "active",
      "outcome": {
        "actions": [
          { "name": "media.play", "target": "playlist:morning-mix" }
        ]
      },
      "confidence": 0.8,
      "support": 12,
      "last_observed_at": "2026-07-21T23:58:02Z",
      "reason_codes": [
        "scene_exact",
        "time_window_match",
        "current_version",
        "support_threshold_met"
      ],
      "recommended_use": "suggest"
    }
  ],
  "as_of_watermark": 9814,
  "freshness": "current",
  "degraded": []
}
```

无匹配返回 `200` 和空 `results`，不是错误。若 `after_receipt_id` 尚未物化且在 `wait_ms` 内未追上，
返回 `409 stale_view`，同时给出当前 watermark 和可重试信息；不能拿旧视图冒充 read-your-writes。

### 5.2 排序与过滤原则

1. **先资格、后排序**：namespace、`state=active`、结构化条件、显式抑制条件和最低证据门槛先过滤；
2. **当前状态优先**：非历史请求不让 superseded 版本参与自动利用；状态不确定时只可返回
   `recommended_use=inspect` 或不返回；
3. **时间是结构化输入**：直接使用 `at + timezone + condition.local_time`，不依赖把时间拼进
   自然语言后再解析；
4. **分数可解释**：条件匹配、支持度、最近证据、反例和用户反馈分别记 reason code。语义相关性
   只能辅助排序，不能越过硬条件；
5. **优雅降级**：无 embedding 时仍可走结构化条件 + 关键词/实体；无可选 LLM 时返回规范化程度较低
   的结果。降级项必须显式列出；
6. **不执行动作**：`recommended_use` 只是建议强度。宿主才拥有用户授权、当前设备状态和动作权限。

当前 `Retriever.Search(ctx, query, k)` 是被动文本 query，候选由关键词、向量、实体等信号融合；
`RetrieverOptions` 虽已有 temporal 和 superseded penalty 原语，但没有结构化 context、硬条件、习惯
证据分或 freshness watermark。当前 MCP registry 还使用零值 `NewRetriever`，并未启用这些可选原语。
所以不能宣称现有 `memory_search` 等价于上述 recall。

## 6. 新鲜度、状态一致性与一份投入两份产出

### 6.1 五个时间与两个水位

习惯路径必须区分：

- `occurred_at`：行为实际发生时间，对应事实的 event time；
- `received_at`：事件被 runtime 接收时间，对应 mention/observation time；
- `created_at/updated_at`：习惯版本写入时间；
- `materialized_at`：统计和结构化检索可见时间；
- `embedded_at`：可选语义索引可见时间；
- `accepted_watermark`：原始事件已持久化到哪里；
- `materialized_watermark`：习惯视图已处理到哪里。

结构化召回不应等待 embedding 才可用；embedding 落后只降级语义排序。receipt + watermark 暴露异步
窗口，并为 `after_receipt_id` 提供 read-your-writes 语义。

### 6.2 习惯变化

以“以前上车播放 X，现在稳定播放 Y”为例：

1. X 和 Y 归入相同的 `subject + kind + condition slot`，但 outcome 不同；
2. Y 只有单次证据时保留 X 为 active，Y 为 candidate，避免一次偶然操作推翻长期习惯；
3. Y 达到替代门槛后写**新版本**，旧 X 通过 supersedes 链标为 historical，不删除证据；
4. 当前场景 recall 只采用 Y；带历史时间窗的审计查询仍可恢复 X；
5. 误判可撤销 supersede，用户明确“不要再自动播放”则作为强负反馈，不等同于普通反例。

当前引擎已经有可复用的 `EventStart/EventEnd`、`SupersededBy`、`EntryStore.Supersede/Unsupersede`、
`TemporalScore` 和 `SupersededPenalty`，curation 也已有保守的 conflict 决策。这些是**原语，不是完整
freshness 能力**：目前没有结构化 habit slot、替代阈值、superseded 生效时间、链/环校验、精确
source span、watermark 或 read-your-writes；基于近重复文本的 conflict 聚类也不能可靠发现结构化习惯冲突。

### 6.3 与拉平分数共用的技术投入

| 共用投入 | 习惯产品产出 | 评测/引擎产出 |
|---|---|---|
| 结构化 event/effective time + 时间窗 | 按本地时段、星期和证据窗召回作息/例程 | temporal 查询不再只靠文本时间词 |
| 非破坏 supersedes 版本链 | 新偏好压旧偏好，历史可审计 | knowledge-update/current-vs-history 正确率 |
| conflict 四分类与 slot 识别 | 区分重复、补充、冲突和替代 | 降低旧状态采用与错误 merge |
| 来源闭包 + 可见性水位 | 解释“为什么推荐”，暴露异步滞后 | freshness/记忆幻觉专项评测基础 |

共享代码不等于共享结论。任何触及引擎写入、检索、curation 或 schema 的实现，都必须在同一配置下过
LoCoMo 可比回归门，并新增习惯域的重复事件、乱序、漂移、历史召回、错误触发和 read-your-writes
评测。只涨 LoCoMo 或只跑产品 demo 都不够。

## 7. 对接、隔离与隐私

### 7.1 Namespace 策略

HTTP 身份由宿主认证层提供，`tenant_id` 不接受请求体覆盖。适配器用每安装密钥做 HMAC，派生只含
`[A-Za-z0-9._-]` 且不超过 64 字符的 namespace：

```text
device scope = HMAC(install_key, tenant_id || user_id || device_id)
user scope   = HMAC(install_key, tenant_id || user_id)
```

- 共享车机默认 `device scope + driver profile`，未识别驾驶员进入临时 guest namespace；
- 手机默认一位本地用户一个 device scope；
- 跨设备 user scope 默认关闭，只有用户同意且厂商提供安全同步层时开启；
- 原始 tenant/user/device ID 不进入 DB 文件名、日志或模型 prompt；
- 删除账户/访客退出必须删除或轮换对应 namespace，不能只清 UI 缓存。

现有 MCP 已实现“一个 namespace 一个独立 SQLite store”、路径白名单和 LRU 打开句柄缓存；这是可复用
的隔离形态，但**不是认证、租户管理或云端访问控制**。云部署若存在，仍需厂商网关承担这些职责。

### 7.2 三个薄接入面

| 接入面 | MVP 用途 | 约束 |
|---|---|---|
| 嵌入式 Go SDK | Go/Linux 车机或设备服务直接链接 | 调目标公共 Go API；不访问表、不复制算法 |
| 本地 HTTP + 生成式 SDK | Android/iOS/非 Go 应用通过 loopback/UDS 调用 | HTTP 只做校验、身份、namespace、错误映射和 backpressure |
| MCP | Agent/开发调试，把 `habit_ingest`、`habit_recall` 暴露为工具 | 参考现有 stdio 工具形态；不是高频遥测主通道 |

三者必须跑同一组契约 fixture，确保同一事件批次与场景返回同一习惯结果。不能在某个 SDK 内偷偷增加
一套启发式逻辑。

### 7.3 Mem0 兼容

提供独立的 **Mem0-compatible facade** 降低已有记忆调用的迁移成本：已有 add/search/get/delete
概念映射到 namespace 下的 EntryStore/Retriever 公共能力；`user_id`、`agent_id`、`app_id` 等兼容身份
先按冻结的优先级映射为 namespace。正式支持的 endpoint、字段、过滤器和错误码必须由兼容契约测试
锁定，不笼统宣称“完全兼容”。

行为事件与条件召回是 habit-native 扩展，Mem0 通用 memory API 没有等价语义，不能为了表面兼容而
把事件伪装成聊天消息。迁移路径是：原 CRUD/search 可低改造迁移；要获得习惯能力，再增加
`habit-events:batch` 和 `habits:recall` 两个调用。

### 7.4 隐私默认值

- 数据面默认本地、断网可运行；外部 embedding/LLM 均为显式 opt-in，推荐本地 sidecar；
- 确定性频率/序列抽取不依赖云模型；未配置模型不能导致核心路径消失；
- 最小化采集：默认不收精确 GPS、内容正文、通讯录或设备密钥，位置先在端侧变为粗粒度标签；
- 原始 observation 有 TTL，聚合习惯可查看、可清除；日志只记录计数、错误码和匿名 namespace；
- 任何跨设备同步、集中分析或远程运维读取都是新的数据出境/授权面，不属于 local-first 默认路径。

“数据不上云”只能在端侧/私有部署模式下宣称；若厂商主动把 sidecar 部署在云上，产品文案必须改为
“客户控制的数据域”，不能继续声称数据留在设备。

## 8. 引擎能力清单与缺口

### 8.1 现成、可直接复用

| 能力 | 当前真实水位 | MVP 用法 |
|---|---|---|
| 本地存储 | pure-Go SQLite、WAL、无 CGO | 每 namespace 独立持久化，断网工作 |
| `EntryStore` | CRUD、时间字段、来源 session、实体/别名、`Supersede/Unsupersede` | 作为习惯版本与索引的底层公共原语 |
| `Retriever` | 文本 query 的关键词/语义/实体 RRF；可选 temporal、superseded penalty，逐信号降级 | 复用排序信号和时间/状态原语；不能直接冒充 conditional recall |
| 对话 `pipeline` | ADD-only，从 user/assistant 文本抽自包含事实；LLM 失败安全跳过 | 复用 pipeline 依赖注入、预算和失败安全模式；不复用其输入语义 |
| curation | 确定性评分、近重复、LLM judge、非破坏 conflict | 复用保守状态更新思想；习惯 slot 仍需新增 |
| MCP namespace adapter | 一 namespace 一 store、路径防逃逸、LRU 句柄、结构性降级标记 | 作为 HTTP/SDK registry 与工具契约的参考 |

注意：现有 MCP 的 `memory_ingest` 只处理对话，`memory_search` 使用零值 retriever options；它们不是
habit ingest/recall 的现成交付物。

### 8.2 必须新增，全部未立项

以下每项状态统一为：**未立项，须 contract-first + 评测门**。在对应 feature 通过前，不得写进
“已支持”或销售承诺。

| 增量 | 最小契约 | 评测门 |
|---|---|---|
| 结构化行为输入与 observation ledger | 公共 `HabitEvent`、批量幂等、乱序规则、TTL、receipt/watermark；adapter 只能调用公共入口 | 重复/冲突 ID、乱序、崩溃恢复、TTL、断网 fixture |
| HabitPipeline | preference/routine/sequence 的确定性聚合、候选门槛、反例/反馈、可选模型降级 | 三类标注数据的 precision/recall；低证据不得激活；无模型结果可用 |
| typed habit/version schema | condition、outcome、support/opportunity、证据窗、状态、版本和来源闭包 | X→Y 漂移、误判回退、历史恢复、环/悬空引用保护 |
| Conditional Recall 公共 API | 结构化 context、硬条件、当前/历史模式、可解释 reason、degraded 状态 | false activation、stale adoption、历史召回、缺信号降级 |
| 一致性契约 | accepted/materialized/embedded 水位，`after_receipt_id` read-your-writes | 并发写读、超时、重启和索引滞后压力集 |
| habit 专项 harness | 固定事件流、期望习惯、触发上下文、用户反馈与漂移数据集 | 与 LoCoMo 回归同时为必过门；报告延迟、错误触发率和陈旧采用率 |
| HTTP/SDK/MCP adapter | 同一 JSON schema、错误码、namespace 映射、namespace 原子清除、秘密不落日志、契约 parity | 三接入面 parity、跨 namespace 泄漏为 0、path escape 为 0、清除后残留为 0 |
| Mem0 兼容 facade | 明确支持的版本/endpoint/字段矩阵，不支持项返回稳定错误 | 上游兼容 fixture；不得以“类似”代替机器契约 |

## 9. MVP 范围、非目标与诚实规模

### 9.1 MVP 包含

- 车机作为第一个参考集成；两种 canonical 场景事件：进入车辆、媒体播放/停止；
- preference、routine、2-3 步 sequence 三类习惯和 `candidate → active → historical` 生命周期；
- 本地批量 ingest、receipt/watermark、结构化 recall、空结果与降级语义；
- namespace-per-user/device、guest 隔离、单 namespace 清除；
- 确定性离线抽取；embedding 和本地 LLM 可选；
- 一套 Go 调用面、一套 local HTTP 契约，MCP 用于集成测试和 agent demo；
- provenance 摘要、置信度、reason code 和“建议而非执行”的宿主边界；
- temporal/supersedes 共用机制以及 LoCoMo + habit 双评测门。

### 9.2 非目标

- 通用记忆云、计费、控制台、SOC 2、跨地域多租户 SLA；
- 跨用户学习、广告画像、群体推荐或把个人事件上传训练；
- 默认跨设备同步、精确位置轨迹或无限期原始事件保存；
- 从一天或一次行为推断偏好，因果推断、健康诊断或家长管控裁决；
- engram 自主执行车控、支付、通信等动作；
- 任意长流程挖掘、复杂规则编排、推送调度平台；
- ANN/量化、百万级单用户条目或无限 token；
- 本文内修改任何引擎或现有适配器代码。

### 9.3 诚实规模

当前边界是**单用户/单 namespace 约 10 万条记忆级**，向量路径仍有 Go 侧扫描成本，SQLite 是
单写者形态。高频设备遥测可能很快超过该量级，所以 observation 必须短期保留并增量聚合，长期只留
有价值的习惯版本。MVP 不声明每秒事件数、P99 延迟、可承载 namespace 总数或云端并发 SLA；这些数字
必须由目标硬件上的 1k/10k/100k 条阶梯基准给出。超过边界的 ANN、量化、分片与云多租户是未来工作，
不能写成隐含能力。

## 10. 车机“一天接入”参考走查

本走查描述目标契约落地后的厂商体验，不代表当前仓库已有 HTTP habit server。

### 10.1 上午：部署与身份隔离

1. 厂商把纯 Go sidecar 随车机镜像交付，数据目录放入驾驶员数据分区，只监听 Unix domain socket；
   不配置网络 embedding/LLM，先跑完全离线模式。
2. 登录驾驶员 profile 时，SDK 传 `user_id=driver-42`、`device_id=head-unit-7`、
   `scope=device`；adapter 派生匿名 namespace。访客模式使用临时 namespace，熄火或退出后按策略清除。
3. 用两个事件完成映射：profile 切换/上车发 `vehicle.entered`，播放器真正开始播放后发
   `media.play`。系统自动恢复的播放标 `initiator=system`，不计入用户偏好支持数。

伪代码：

```go
receipt, err := habits.Ingest(ctx, Batch{
    UserID: "driver-42", DeviceID: "head-unit-7", Scope: DeviceScope,
    Events: []Event{
        {ID: "evt-1", At: enteredAt, TZ: "Asia/Shanghai", SessionID: tripID,
            Sequence: 1, Action: Action{Name: "vehicle.entered"},
            Context: map[string]string{"scene": "vehicle", "initiator": "user"}},
        {ID: "evt-2", At: playedAt, TZ: "Asia/Shanghai", SessionID: tripID,
            Sequence: 2, Action: Action{Name: "media.play", Target: "playlist:morning-mix"},
            Context: map[string]string{"scene": "vehicle", "initiator": "user"}},
    },
})
```

类型名是说明性草案，最终以 contract-first 的 Go API 为准。

### 10.2 下午：场景召回与宿主利用

开发环境回放至少跨 3 个日期/会话的同意数据 fixture，等待 receipt 变为 `materialized`。再次上车时：

```go
result, err := habits.Recall(ctx, RecallRequest{
    UserID: "driver-42", DeviceID: "head-unit-7", Scope: DeviceScope,
    At: now, TZ: "Asia/Shanghai",
    Context: map[string]string{"event": "vehicle.entered", "scene": "vehicle"},
    AfterReceiptID: receipt.ID,
})
```

宿主处理规则：

1. 空结果、`candidate`、`freshness != current` 或任何关键降级：不自动动作；
2. `active` 且结果在本地媒体白名单：默认弹出“继续播放 morning mix？”；
3. 用户在设置中明确开启“上车自动续播”后，宿主才可自动调用播放器；engram 本身没有播放器权限；
4. 用户接受、忽略、停止或选择另一播放列表，均作为带 `initiator=user` 的新事件回流；连续改变后由
   supersedes 生命周期把旧播放偏好转为 historical。

### 10.3 当天验收清单

- 拔掉网络后 ingest、materialize、recall 仍工作；未配置模型时降级标记真实；
- 同一事件重放不增加 support，同 ID 不同负载被拒绝；
- `driver-42`、另一个驾驶员和 guest 三个 namespace 互不可见；
- 未达证据门槛时上车 recall 为空；回放达标 fixture 后只返回匹配场景的 active 习惯；
- 写入 Y 替代 X 后，当前 recall 不采用 X，历史审计仍能找到 X；
- `after_receipt_id` 未追上时明确返回 stale，不静默读旧状态；
- 宿主未授权时没有自动播放；日志、响应和 DB 文件名不出现原始身份、密钥或敏感事件正文；
- 记录目标车机在 1k/10k/100k 长期习惯条目下的延迟和 DB 体积，只报告实测结果。

完成这些检查才可称为“一天接入样板”；习惯准确率、错误触发率和长期漂移仍须经过第 8 节评测门，
不能用接线成功代替产品有效性。

## 11. 立项顺序与发布口径

1. **Contract feature**：冻结 HabitEvent、HabitRecord、ingest/receipt/recall、错误语义、namespace
   映射和 Mem0 兼容子集；先写跨 Go/HTTP/MCP 的 fixture。
2. **Extraction feature**：实现 observation ledger 与确定性三类模式，先过断网、幂等和抽取精度门。
3. **Freshness/recall feature**：完成 typed slot、supersedes 生命周期、watermark 与 conditional recall，
   同时过 LoCoMo 可比回归和 habit 漂移/错误触发门。
4. **Reference adapter feature**：只调用公共 API，交付车机样板与一天接入 runbook；引擎目录必须与该
   adapter feature 的 diff 完全无关。

在前三项通过前，对外只能说“engram 已有本地存储、混合检索、对话事实抽取、namespace 和
temporal/supersedes 原语，正在设计习惯记忆”；不能说“已经能学习用户习惯”“已经解决记忆新鲜度”
或“已经支持主动召回”。
