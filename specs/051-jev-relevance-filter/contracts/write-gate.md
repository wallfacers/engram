# Contract: 写前门（WriteGate）决策合同

## 决策词汇表（route）

| route | 含义 | 本期启用 |
|-------|------|---------|
| `write` | 放行写入（路径 A 当轮写 + 告知，契约不变） | ✅ |
| `skip` | 拦截，不写、不告知、不追问 | ✅ |
| `defer_to_packet` | 交给短包/整理员线（微软线，未交付） | ❌ 仅占位（clarify Q4：不得与短包线冲突） |

## 判题（§18.1 四题，经 clarify Q4 修订）

| 题 | true 时 |
|----|---------|
| `is_durable`（跨 session 稳定，非一次性/临时） | false → `skip`（「这周赶 demo」类） |
| `is_preference`（用户亲口偏好/约束/身份） | true → `write`（路径 A 当轮写） |
| `is_secret`（含密钥/token/隐私） | true → `skip`，**除非**用户本轮明确要求记录自己的 key（HF/DeepSeek 等厂商 API key）→ `write` |
| `needs_probe`（环境结构，需离库核对） | 本期恒 0、不启用（短包线交付后再开） |

**判定优先级（Slice-1 仲裁冻结）**：`user_requested`+密钥 → `write` ＞ 密钥 → `skip` ＞（durable ∥ preference）→ `write` ＞ `skip`。

## 硬约束

0. **传输边界（互审修订，BLOCKER 修复）**：含密钥形状的草稿先经**本地形状预判**短路——用户明确要求记录自己的 key → 本地直接 `write`；未要求 → 本地直接 `skip`；两者**不发云**。其余草稿发云前对密钥形状子串做遮蔽。写门云调用超时 500ms、fail-open；门控仅覆盖手动 `memory_write`（`memory_ingest`/抽取/curation 不门控）。输入映射（互审修订）：`draft = memory_write.content`；引擎接口规范形 = **`GateRequest{UserTurn, Draft, UserRequested}`**（`Gate(ctx,userTurn,draft)` 为 `UserRequested=false` 简写）；`memory_write` 侧映射为可选参数 `user_turn`（缺省空→`is_durable`/`is_preference` 仅按 draft 判定）与 `user_requested`（缺省 false；true = 用户明确要求持久化，密钥形状内容本地直接 `write`，覆盖「未要求→skip」）。
1. **fail-open**：门失败/超时/无 key → 本次视为无门，现行 skill 写规则照跑；**不得因门失败拦截或延迟写入**（路径 A 契约不可破坏）。
2. **密钥落地边界**：`write` 放行的密钥只入用户本地记忆库；MUST NOT 进入日志、工具响应、受跟踪文件（AGENTS.md Secrets 条款不因此放松）。
3. **当轮告知不变**：`write` 仍走「同一轮写完 + 一行告知」；门只决定「不写」，不引入确认交互。
4. **不替代整理员/探测**：真伪核对（§22 表 4–6 行）不在本门职责；`GateDecision.Reasons` 概率如实透传供审计。
5. 决策出口保留 `defer_to_packet` 位：未来短包线可无破坏接入（同一 `GateDecision` 词汇表，不改语义）。
