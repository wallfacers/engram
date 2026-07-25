# Specification Quality Checklist: LongMemEval 子集先行 · 跨 benchmark 复现 coverage≠answer

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-26
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## 验证记录（第 1 轮）

初稿自查发现并已就地修正的问题：

1. **实现细节泄漏** — FR-004 初稿写死了轮次标识的具体字符串格式
   （`"D<session>:<turn>"`）与具体函数名。已改为「其形式 MUST 与既有 benchmark 的
   轮次标识同构，使既有覆盖计量、归因追踪逻辑无需修改即可复用」——保留可测的**约束**
   （同构、下游零改动），把格式落到 plan 阶段。
2. **实现细节泄漏** — 多处初稿直呼具体文件名与命令行开关（`longmemeval.go`、
   `--coverage-only`、`memory_embeddings` 表名）。已改为能力描述：「只读覆盖诊断」
   「记忆向量行数」「评测工具」。
3. **成功判据不可测** — 初稿 SC 中有「时间信息不丢失」这类无法直接观测的表述。
   已改为 SC-002「日期塌缩为单一值的题目数为 0」，可直接计数。
4. **判据表述含糊** — 初稿写「条件增益接近 LoCoMo 的 35.2pp」。已改为 FR-019 的
   三分区闭式判据（20–50pp / >60pp / 其余），并显式禁止事后调整与四舍五入。
5. **边界未封口** — 初稿未说明灰区如何处置。已补 Edge Case「判决落在灰区 ⇒ 报无法
   判定」与 FR-019 对应条款。

## Notes

- **判据前置固化是本 spec 的核心纪律**：FR-019 与 SC-008 共同保证判据在测量前写死、
  测量后逐字比对。这直接源于 015 的教训（设计建在未经测量的前提上）与 A 实验的教训
  （伪影差点被当作模型结论报出）。
- **证伪是有效结果**：spec 不预设「复现」为成功。SC-008 只要求结论落在三选一内，
  不要求落在某一个。
- 无 [NEEDS CLARIFICATION] 标记：设计文档已逐段确认，spec 不引入新决策。
