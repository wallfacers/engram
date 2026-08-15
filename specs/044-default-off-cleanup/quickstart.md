# Quickstart: 默认关闭机制清理(044)

**Date**: 2026-08-16 | 清理全程离线,无模型调用,引擎零改动。

## 前置

```bash
# 独立 worktree(已建)
git worktree list                     # 确认 .claude/worktrees/044-default-off-cleanup 在 master 上
cd .claude/worktrees/044-default-off-cleanup
# 基线确认:清理前全量测试必须绿
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
# 引擎五目录基线
git diff --name-only -- memory embedding provider store internal   # 必须为空
```

## 执行(按验收顺序)

每批删后立即验证:`CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./...`。

```bash
# 1. 第一类纯 flag 级(assoc/cluster-sweep/temporal-score/temporal-hard-filter/
#    conflict-resolution/filter-pool/opinion-pass)
#    → main.go flag/options/fingerprint + eval_runner arm 路由 + 冲突表

# 2. 010/011/012(multi-query 作答路径删,保留 recallDiagnostic/SearchMulti;
#    alias-shadow/doc2query 整删 + 改引用测试)

# 3. 专属文件机制(abstain 作答分支删保留 probe/029 nav/021 iris/027 gap-refetch;
#    temporal_resolution 保留 classifyQueryMode)

# 4. 030/031 整链 + trace 默认值移除(单独 commit)
#    → trace_mediation/consolidate/assembly 系/relation_graph/entity_grouping
#    → main.go trace-mediation 默认值移除

# 5. 042 协议(13 个 counterfactual 文件 + utility 族 flag)

# 6. 收尾:文档同步 + 全量门
```

## 验证门(每批)

```bash
CGO_ENABLED=0 go build ./...                     # 零错误
CGO_ENABLED=0 go test -count=1 ./...             # 全绿(含 byte-parity)
CGO_ENABLED=0 go vet ./...                       # 零告警
git diff --name-only -- memory embedding provider store internal  # 空(引擎零改动)
git diff --stat                                   # 确认净删
./engram-mcp --help | grep -c "<已删flag>"        # 已删 flag 不再出现
```

## 收尾检查

- [ ] 已删 flag 从 `--help` 消失
- [ ] unified 契约 digest `1d8a8d0f` 锁锚断言仍绿(runner.go 常量未动)
- [ ] `--temporal-answer-prompt`(032)/`--temporal-date-scaffold`(017)/`--abstain-probe`/`--oracle`/`--rerank`/`--pcic`/`--compiler-arm`/`--recall-diagnostic` 仍可用
- [ ] result-matrix「过时/已证伪」表、清理计划文档、README 同步
- [ ] trace 默认值移除单独 commit,消息注明 Step A 依据(−3.44pp)
- [ ] commit 消息前缀 `chore(cleanup)`,无 push

## 红线

- 引擎五目录零改动;不碰 box/AutoDL;不重跑已证伪机制
- 不删 032/017/诊断/未定论能力;`SearchMulti`/`recallDiagnostic`/`classifyQueryMode`/`computeAbstainSignal` 保留
