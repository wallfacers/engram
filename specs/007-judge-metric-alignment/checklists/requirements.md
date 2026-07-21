# Specification Quality Checklist: LoCoMo Judge 口径对齐(mem0-aligned)

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-21
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

## Notes

- 这是评测口径特性(harness),"用户"= 维护者,与 003/005/006 同类。部分机制名词(judgeSystemPrompt、fingerprint、flag、golden 夹具)出现在需求中,与既往 harness spec 一致——以"可测量的口径/结果"而非代码结构描述,可接受。
- 冠上珠是 **SC-001**(anti-放水硬门):新 judge 对每条陷阱必判 WRONG,零放水。这是"对齐非放水"的判据。
- **SC-005/SC-006** 守回归与口径隔离:flag off 逐字不变、开关 flag 的 fingerprint 不同。
- 宪法 IV 诚实约束编码于 FR-008/FR-010/SC-007(新基线协议、eval 单独提交、"口径对齐非涨点"标注、默认不翻)。
- US2(付费量化)双门控:依赖 US1 交付 + 显式成本授权;默认不执行,与 006 的付费 US2 结构一致。
