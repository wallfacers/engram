# Specification Quality Checklist: LoCoMo 跑分杠杆探索(008)

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-07-22
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — *注:本仓库评测特性惯例含具体 flag/端点锚点(见 007),用于让外部执行者傻瓜式执行;WHAT/WHY 仍为主*
- [x] Focused on user value and business needs(对齐/超越竞品跑分、合法可移植的赢)
- [x] Written for stakeholders(维护者=用户)
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous(每条 FR 有确定判定门/命令)
- [x] Success criteria are measurable(turn@k ≥+4pp、零调用、无回归、git diff 空)
- [x] Success criteria tied to outcomes(可移植赢、预算伪影消除)
- [x] All acceptance scenarios are defined(每 US 含 Given/When/Then + 具体配置)
- [x] Edge cases are identified(追不平/格式不符/维度漂移/预算混淆/凭据泄漏)
- [x] Scope is clearly bounded(adapter/sidecar/env only;引擎零改;每 US 独立)
- [x] Dependencies and assumptions identified(远端机/固化 store/同栈/死规则)

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows(US1 旗舰 / US2 免费 / US3 备胎 / US4 落成)
- [x] Feature meets measurable outcomes in Success Criteria
- [x] No implementation details leak beyond the executable-anchor convention

## Constitution / Death-Rule Gates(本仓库额外硬门)

- [x] 死规则:reranker 本地 only,禁付费云 rerank(FR-001, SC-005)
- [x] 引擎零改(FR-002, SC-004)
- [x] 宪法 IV 免费闸优先 + eval 单独提交 + 声明新参考点(FR-003/010, SC-006)
- [x] 宪法 V:采纳杠杆默认关/opt-in、离线可降级(FR-009)
- [x] 凭据零落盘(FR-011)

## Notes

- 评测特性惯例(同 007):spec 内保留具体 flag/端点/命令,目的是「外部 Agent 傻瓜式执行、无需再头脑风暴」——这是本特性的显式目标,非泄漏。
- 下一步 `/speckit-plan` 应把每 US 展开为:sidecar 启动脚本、确切 bench 命令、判定门计算、产物目录、提交切分(mechanism vs eval)。
