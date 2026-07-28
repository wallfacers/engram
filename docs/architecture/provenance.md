---
title: Provenance 架构
summary: 本文说明记忆来源、事件与宿主边界的通用模型；不绑定任何个人工作流或特定宿主代理。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [memory-provenance]
tags: [architecture, provenance, ingestion]
---

# Provenance 架构

Provenance 让记忆能追溯其显式来源和写入原因；它不授权宿主自动抓取或把环境上下文当作记忆。

## 来源模型

每次显式 ingest 或直接写入都应保留调用方提供的来源标识、时间、namespace 和必要的事件关系。revision 与 supersession 用于表达修订和替代，不应被解释为对事实真伪的自动判定。

## 宿主边界

宿主、客户端或 agent 负责选择何时调用 `ingest`、`write` 或 `add`。engram 不从后台会话、开发工具或个人环境隐式抽取；这样同一存储模型可以被不同宿主安全复用。

## 审计使用

检索与 curation 可以引用来源信息解释结果，但不应泄露跨 namespace 内容。变更来源字段、事件语义或删除策略时，同时核对[记忆系统架构](memory-system.md)和[当前能力边界](../product/capabilities.md)。
