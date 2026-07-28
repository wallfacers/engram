---
title: 记忆新鲜度与状态一致性 Backlog
summary: 本文定义尚未实现的记忆新鲜度与状态一致性工作边界；不把现有 provenance 或 supersession 当作已完成保证。
status: proposed
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [memory-freshness]
tags: [product, backlog, freshness, consistency]
---

# 记忆新鲜度与状态一致性 Backlog

这是未实现的 proposed backlog，不能作为当前能力证据。当前已交付边界请看[当前能力边界](../capabilities.md)。

## 问题定义

系统需要能够在事实更新、冲突、过期或来源撤回时，以可审计方式表达何者仍有效、何者被替代，以及检索与回答如何处理这些状态。当前 schema 中的 provenance、event、supersession、revision 提供基础原语，但没有构成端到端状态一致性保证。

## 进入实现前的验收条件

- 明确状态模型、写入冲突策略和可回滚语义。
- 为检索、引用和 curation 指定一致的过滤规则与可观察指标。
- 为跨 namespace、离线降级和部分失败写出测试与迁移策略。
- 在[记忆系统架构](../../architecture/memory-system.md)与当前能力页中更新实际出货状态后，才可将此项转为 active。
