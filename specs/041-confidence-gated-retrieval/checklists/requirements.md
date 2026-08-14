# Specification Quality Checklist: Confidence-Gated Iterative Retrieval

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-14
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

- 本 feature 是 040（adaptive-topk，gap-knee）的**直接续作**：040 用检索分数拐点判深度 → NO-GO（r 上限 20% < 45% 门槛）；041 改用 **answerer 生成中的确定性犹豫/置信度信号**判「读到多少停」。这一机制转向的依据是 040 的「89% 犹豫、仅 7% 自信地错」发现 + 三篇论文综合（确定性不确定信号 > 模型自主判断；避免 closed-book probe 成本；retrieval 有 harm 故按需分配）。
- US1（犹豫信号检测 + 区分度诊断）是 P1 **生死前提**：先证明「犹豫」能从 answerer 正常生成中确定性流式提取、且能区分「答对/答错」，再谈迭代；证伪即停，不改任何检索路径。
- SC-002 是**条件结果**而非承诺：「省 ~4.8×（收敛到 ~31）」是目标上限，正确率无统计显著下降是硬门槛；正确率永远优先于省预算。
- SC-004 明确不夸大：91.75% 是完美信号下的 oracle ceiling（1332+31+50=1413/1540），非承诺值；「自信地错」的 7% 盲区须在诊断中显式报告。
- 信号形态（流式犹豫启发式 vs log-prob 置信度）与迭代结构（两轮 vs 多轮阶梯）defer 到 plan 阶段定夺；首版默认流式 + 两轮。
- scope 边界：只在 eval harness 层（`cmd/locomo-bench`）实现，**不触碰引擎**（宪法 II）；生产接线是独立后续 feature（同 023 planner 接线教训）。
