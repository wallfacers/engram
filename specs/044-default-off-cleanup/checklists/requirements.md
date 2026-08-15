# Specification Quality Checklist: 默认关闭机制清理

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-16
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — 范围/判定/验收以行为与结果描述为主;机制清单属领域实体而非实现细节
- [x] Focused on user value and business needs — 用户=维护者/评测运营者;价值=降低维护负担、消除"代码仍在=能力仍在"误导
- [x] Written for non-technical stakeholders — 以机制行为、verdict 判定、清理结果描述为主
- [x] All mandatory sections completed — User Stories/Requirements/Success Criteria/Assumptions 全齐

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — 关键决策(042 关/043 NO-GO/trace 移除)已由维护者拍板写入 spec
- [x] Requirements are testable and unambiguous — FR-001~FR-010 均以"移除后 --help 不再列出/byte-parity 保持/构建测试全绿"等可测方式表述
- [x] Success criteria are measurable — SC-001~SC-006 全部量化(100% 通过/flag 消失/diff 为空/绝对分对齐)
- [x] Success criteria are technology-agnostic (no implementation details) — 以 flag、字节一致、目录 diff 等可观察结果表述
- [x] All acceptance scenarios are defined — 每个 User Story 含 Given/When/Then 场景
- [x] Edge cases are identified — 依赖悬空、默认值切换、043 资产归属、eval-config 分离、诊断工具边界
- [x] Scope is clearly bounded — 三类明确(清理/保留/待定核查),红线(引擎零改动/byte-parity)
- [x] Dependencies and assumptions identified — 043 收口状态、042 不重开、trace 收敛到锚配方、引擎零改动

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria — FR 与 US 场景一一对应
- [x] User scenarios cover primary flows — 纯冗余清理 → 042/043 → trace 行为变更 → 文档收尾
- [x] Feature meets measurable outcomes defined in Success Criteria — SC 覆盖全部 FR
- [x] No implementation details leak into specification — 机制 flag 名是领域标识(verdict 记录中的对象),非实现方案

## Notes

- 本 feature 是内部工程清理,spec 的用户故事以"维护者/评测运营者"视角编写,非终端用户——这是刻意的,已在上方 User Scenarios 开头注明。
- **已核实(2026-08-16)**:master 不含 043 代码(deepen 文件/flag 均无),只有 042 的 13 个 counterfactual 文件 + trace-mediation 默认开启。044 实际清理对象 = master 上的 042 协议 + 其余已证伪机制 + trace 默认值切换;043 仅记录结论。
- 待 plan 阶段核查项:042 清理时其 logprob caller/manifest-seal 协议是否被其他保留机制引用(已确认 043 不在 master,无此依赖);trace 移除后 `consolidate`/`evidence-assembly` 的去留(030 同族)。
