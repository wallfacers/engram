# 验收报告：企业级文档信息架构

**Feature**: `019-docs-information-architecture`
**Branch**: `019-docs-information-architecture`
**Implementation baseline**: `c86e47e` (`docs(readme): 精简 License 节,去掉多余贡献条款声明`)
**Started**: 2026-07-28

## 实施起点

- 初始工作树状态：clean。
- 根 `README.md`、`README.zh-CN.md`、`LICENSE` 和 `CLAUDE.md` 属于已接受的并行工作，
  本 feature 只通过 12 个 relocated 页面保持其 docs 深链有效。
- Node.js：`v24.14.1`；后续 validator 只使用 Node.js 标准库。
- 不运行 LoCoMo、付费模型或联网检查。

## TDD 记录

| 组 | RED 证据 | GREEN 证据 |
|---|---|---|
| Metadata / headings | `node --test docs/validation/check-docs.test.mjs`：预期 `ERR_MODULE_NOT_FOUND`，因为 `check-docs.mjs` 尚未实现；1 test file / 1 fail | T004 后：3 tests / 3 pass；验证完整元数据、重复主题、单一 H1、层级和 slug |
| Links / navigation | `node --test docs/validation/check-docs.test.mjs`：metadata/headings 3 pass；新增 2 tests 因 `validateLinks` / `validateNavigation` 尚未实现而失败 | T006 后：5 tests / 5 pass。 |
| Retrieval / relocation | T007：原有 5 tests 继续通过；新增 2 tests 因 `validateRetrieval` / `validateRelocation` 尚未导出而失败。 | T008 后：7 tests / 7 pass；覆盖 Q1–Q8、迁移正文限制、归档条件字段、分数消费者和能力边界。 |

测试环境：Node `v24.14.1`；定向测试命令为 `node --test docs/validation/check-docs.test.mjs`。

## Success Criteria Evidence

| Criterion | 验收证据 | 结果 |
|---|---|---|
| SC-001 两跳可达 | `--navigation` 输出与门户人工导航 | 待验证 |
| SC-002 Q1–Q8 唯一正本 | fixture、validator、两份独立检索复核 | 待验证 |
| SC-003 本地链接与锚点 | `--links` 输出和语义链接审阅 | 待验证 |
| SC-004 现行元数据 | `--metadata --headings` 输出 | 待验证 |
| SC-005 archive / relocated | `--relocation`、archive 索引 | 待验证 |
| SC-006 已知状态漂移清零 | quickstart drift scan | 待验证 |
| SC-007 唯一结果矩阵 | 分数消费者检查和人工复核 | 待验证 |
| SC-008 无状态误判 | fixture 与两份独立检索复核 | 待验证 |
| SC-009 入链与删除证明 | 逐设计删除门和 disposition manifest | 待验证 |
| SC-010 范围隔离 | `git diff` 与 Go 回归 | 待验证 |
| SC-011 治理分类一致 | 两份 G1–G3 复核 | 待验证 |

## 后续证据区

实施任务会在对应阶段补充 validator 输出、独立复核、删除门证明、链接语义审阅和最终
disposition manifest；本节不作为完成结论。
