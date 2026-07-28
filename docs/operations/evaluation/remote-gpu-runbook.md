---
title: 远端 GPU 评测运维
summary: 本文规定远端 GPU 仅用于临时评测的启动、产物与停机纪律；不把远端机器作为 engram 的运行时依赖。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [remote-eval-operations]
tags: [operations, evaluation, gpu, remote]
---

# 远端 GPU 评测运维

远端 GPU 仅服务于临时 answer/extract 或 embedding 评测。engram 产品保持 local-first 和可离线运行，不依赖远端机器。

## 每次启动前

远端实例重启、迁移或更换后，必须重新确认 GPU、持久盘、模型缓存、运行脚本与模型服务健康状态。SSH 主机、端口、密码、令牌和其他会轮换的值只在安全运行环境中传递，绝不写入 tracked 文件或日志。

## 模型服务与端口

将 answer 服务与 embedding 服务使用显式、可验证的本地隧道端口；通过 `/v1/models` 健康检查确认服务，而不是通过进程名猜测。启动时固定模型 revision、上下文长度、显存配额和关闭推理链的参数；这些参数变更会改变评测 regime。

## 产物持久化

远端系统盘和临时内存都可能在迁移或停机后消失。只把可重建的评测产物放入受控的私有持久化位置，并在上传前扫描凭据与敏感内容；产品 namespace 的持久 SQLite 数据仍应留在本地。

## 停机纪律

实例按时计费，评测结束后应先停止模型服务、检查 GPU 显存释放，再以控制台状态确认实例已停止。容器内的关机命令在不同提供商或迁移实例上可能无效；无法确认停止时，必须要求有权限的维护者在控制台执行停机。

## 与评测 recipe 的关系

远端机器只提供执行资源，不定义结果。具体 recipe、环境变量和 run 后验证见[LoCoMo 评测运行手册](locomo-runbook.md)。
