# Specification Quality Checklist: implicit-memory-flywheel

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-29
**Last reviewed**: 2026-09-01
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs) — 三工具 CLI 名以评测矩阵身份出现(用户点名的 runner),数据集形态沿用 020 既有交付物惯例;未绑定实现机制
- [x] Focused on user value and business needs — 核心是用户反馈"没记录记忆"的修复
- [x] Written for non-technical stakeholders — US1/US2/US6 为纯行为语言;数据集/runner 章节偏工程但为此 feature 本体
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain — 隐式写入边界已由维护者拍板(直接写+当轮告知)
- [x] Requirements are testable and unambiguous — FR-001..017 均有可观察判定
- [x] Success criteria are measurable — SC-1..9 均量化，双正式分数按 host × split × 3-run median 验收
- [x] Success criteria are technology-agnostic — SC 以通过率/误触发率/圈数表达,不含实现
- [x] All acceptance scenarios are defined — 每个 US 含 Given/When/Then
- [x] Edge cases are identified — 9 类(纠错更新/他人归属/时间限定/假触发词/一次多条/空结果/blocked/中英混合/宿主差异)
- [x] Scope is clearly bounded — FR-016/017 硬边界(引擎零改动、不触 LoCoMo 基线)
- [x] Dependencies and assumptions identified — Assumptions 5 条

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows — 写入/读取/数据集/runner/飞轮/安装六链路
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- 方向变更声明已显著记录:推翻 020 "反隐式写入"立场,护栏以负例集+误触发率门+当轮告知替代。
- 最终 runner/judge 冻结前的 baseline 是 exploratory diagnostic-only:它只供诊断,不得进入 SC-5 FailureArchive。SC-5 使用修订前 skill 快照在最终执行契约下产生的 core-only `dev-comparison` 三宿主 × 三 ordinal series，并以二元 case median 做可比前后判定；它同样不得改变或豁免 SC-1～SC-4 的冻结门槛。
- 三工具非交互输出的判定通道(各 CLI 的调用痕迹暴露方式)留给 plan 核实;spec 已含降级路径(仅冒烟不判分,如实记录)。
- 2026-09-01 第二轮整改已冻结递归闭集 `BlindCandidateV1`，author envelope 不得携带作者身份或最终 family id；controller 仅在 admission CAS 成功后分配 `hfam-*`，并以 append-only attempt/admission chain 与 source-bound family summary 验证双盲审状态。
- 正式评测只能使用不可变 `FrozenSkillPackageSnapshot`；`skill-eval package validate` 是 package-validation receipt 的唯一 producer。任何 snapshot 后的 package 修改都不得继承原正式分数，必须重新快照、校验和评测。
- 不可逆操作前必须存在当前且通过的 fixed-suite `GreenTestReceipt`：`holdout-pipeline`、`formal-tooling`、`series-prepare` 和 `pre-holdout`各自绑定冻结输入，不得用事后或漂移 receipt 补门。
- holdout 绑定后任一 `INVALID` 只允许在完全相同 binding tuple 下新建 series，并从零完整重跑 core172 + holdout96、三宿主、三 ordinal；不得复用任何旧 ordinal receipt，最终 report 仅引用该完整 recovery series。
- 以上整改已同步到 spec/plan/data-model/contracts/quickstart；`tasks.md` 任务顺序审计与第二次只读 `$speckit-analyze` 仍是 implement 前门禁。
