# Quickstart: 025 跨消息语义聚类实现与评测

本页是实现与评测阶段的验收入口。

## 1. 固定工作区并确认状态

```bash
cd /home/wallfacers/project/engram
git worktree list                 # 025 在独立分支,无需 worktree
git status --short
git log --oneline -5
export SPECIFY_FEATURE_DIRECTORY=specs/025-semantic-episode-cluster   # shell 内 pin
```

确认:当前在 `025-semantic-episode-cluster` 分支;无来源不明的未提交改动。有重叠时停止并
报告,不覆盖。

## 2. 承接资产基线(US2 接线验证前)

```bash
CGO_ENABLED=0 go test -count=1 ./memory/ ./cmd/locomo-bench/
```

预期全绿。重点确认 022 交付的 episode 单测已存在:

- `memory/episode_test.go`(13 tests,同 session 连续边界/narrative/删除/降级/幂等)
- `cmd/locomo-bench/representation_eval_test.go`(14 tests,shared anchor/digest/渲染)
- `representation_integration_test.go`(6 tests)

**接线缺口确认**:`grep -rn "RebuildSession" --include="*.go"` 应只有 `memory/episode.go`
定义处与 `episode_test.go`——正式 eval 从未构建 episode 投影。这是 US2 的真实焦点。

## 3. 实现验证(Phase A/B — 引擎跨 session 能力)

```bash
CGO_ENABLED=0 go test -count=1 ./memory/          # semantic_cluster_test.go 红→绿
CGO_ENABLED=0 go build ./...
```

新增 `memory/semantic_cluster.go` + `episode.go` 的 `RebuildAll`;零新 migration。
检查点:跨 session 相关 Evidence 聚成同 episode;不相关不聚;有界截断确定性;config-hash
幂等;无 embedding 端点时纯离线可判定。

## 4. Eval 接线(Phase C)

```bash
# --episode-cluster 默认关;开启时渲染前对每个 conversation 的 store RebuildAll
go build ./cmd/engram-mcp   # 不影响
go run ./cmd/locomo-bench --help | grep episode   # 确认 flag 存在
```

零行为变化验证:关 `--episode-cluster` 时 artifact digest 与现状一致(FR-003)。

## 5. 配对验证(Phase D — 宪法 IV 回归门)

在 022 冻结协议(cap 3600,repeats≥3)下:

```bash
# 控制臂:chunk_900 基线(022 冻结 manifest)
# 处理臂:--representation semantic_episode --episode-cluster
go run ./cmd/locomo-bench --data <locomo.json> --run-dir ./.locomo-run-025 \
  --eval-protocol <024/022 control manifest> --representation semantic_episode --episode-cluster
```

报告:分类别(multi-hop/open-domain/single-hop/temporal)+ 配对统计 + token 记账;表示差异与
检索差异分离(同 anchor)。负结果按 FR-010 记录 verdict,不进入默认路径。

## 6. 约束速查

- **默认关**:`--episode-cluster` 默认 false;关闭时零行为变化。
- **纯本地**:无 embedding/LLM 端点时离线信号(实体+关键词)完整可判定。
- **零新 migration(预期)**:跨 session 聚类靠 RebuildAll 跳过同 session 校验,不动 schema。
- **不引入付费 reranker/LLM**(death rule);embedding overlay 是本地 sidecar,默认关。
- **append-only**:episode 可丢弃可重建;聚类删除/重建不删改 Evidence 原文。
- **评测**:触及聚类的提交须在合并前完成配对 slice;正式默认变更前完成双基准 full(宪法 IV)。
