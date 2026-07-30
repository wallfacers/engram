# Quickstart: 022 实现与评测

本页是实现阶段的验收入口。022 当前处于 plan 阶段；标为“目标 CLI”的 flags 是本特性要
实现和测试的合同，在对应 task 完成前尚不可用。

## 1. 固定工作区并检查碰撞

只在 022 独立 worktree 操作：

```bash
cd /home/wushengzhou/workspace/github/engram/.claude/worktrees/benchmark-parity-memory-architecture
export SPECIFY_FEATURE_DIRECTORY=specs/022-benchmark-parity-memory-architecture
git worktree list
git status --short
git log --oneline -15
```

开始实现前必须确认：

- root 的 021 `main.go`/`iris.go`/`iris_test.go` 已提交、暂停或由维护者明确处理；
- 022 已基于包含 lossless LongMemEval chunks 的最新主线；
- 本 worktree 没有来源不明且与 022 重叠的改动；
- `.specify/feature.json` 不作为跨 worktree 真相，shell 始终 pin
  `SPECIFY_FEATURE_DIRECTORY`。

有重叠时停止并报告双方文件/行与 commit，不覆盖另一 feature。

## 2. 先运行当前离线基线

文档/任务开始前确认现有代码健康：

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./memory/... ./store/... ./mcpserver/...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench
```

实现每个 engine increment 时先添加会失败的 contract/integration test，然后仅运行触及
package；全部通过后再跑 `CGO_ENABLED=0 go test -count=1 ./...`。

## 3. Ledger 增量的完成检查

实现 v7 与 [engine-api.md](./contracts/engine-api.md) 后，至少能通过：

```bash
CGO_ENABLED=0 go test -count=1 ./store/... -run 'Migration|Evidence|Purge'
CGO_ENABLED=0 go test -count=1 ./memory/... -run 'Evidence|Projection|Episode|Lineage'
CGO_ENABLED=0 go test -count=1 ./mcpserver/... -run 'IngestV2|Evidence|Namespace|Parity'
```

人工/fixture 验收：

1. 两条带 caller source ID 的 message 被先写入 Ledger。
2. Extractor 失败，Evidence 仍 active，projection 数为 0。
3. 由两条 message 支持的 fact 返回两个 source IDs。
4. direct `memory_write` 建立 self Evidence，重复同内容不增生。
5. tombstone 使无其他支持的 projection 退出搜索；restore 只恢复 raw Evidence，
   projection 重建后才 active。
6. purge 后 source recovery、FTS、vector、entity、query/alias、依赖 projection 均不可见，
   WAL checkpoint 状态明确。
7. 两个 namespace 使用相同 caller source ID，交叉可见数为 0。
8. 003 graph schema/data/API parity 仍通过。

对 100k fixture 运行批量 lineage benchmark/SQL counter，确认候选数增加不会产生一条
candidate 一次 query 的 N+1。

## 4. 校准实际 TokenCounter

实现 [compiler-contract.md](./contracts/compiler-contract.md) 后，先在同一个本地
answerer runtime 上运行 calibration，不能直接开始正式分数实验。

目标 CLI：

```bash
go run ./cmd/locomo-bench \
  --token-counter-calibrate \
  --answerer-model-dir /absolute/path/to/local-model \
  --run-dir "$ENGRAM_022_SCRATCH/counter-calibration"
```

`ENGRAM_022_SCRATCH` 必须由维护者设为本次 session scratchpad 的绝对路径；不要使用 repo
root、系统 `/tmp` 或 tracked 目录。校准输出必须满足：

```text
fixtures_complete = 100%
preflight_vs_runtime_input_token_delta = 0 for every fixture
counter_fingerprint is non-empty and stable
```

覆盖 CJK、emoji、长数字、时间、role boundary 与 cap±1。任一差值不为 0 时不生成正式
protocol，不能退化成字段数/字符数。

## 5. 冻结 `022.v1` Protocol

目标 CLI 用单一 manifest 激活 022 path，避免继续堆无约束 flags：

```bash
go run ./cmd/locomo-bench \
  --eval-freeze-protocol "$ENGRAM_022_SCRATCH/protocol/locomo-low.json" \
  --data /absolute/path/to/locomo.json \
  --dataset-format locomo \
  --retrieval both \
  --top-k 30 \
  --repeats 3 \
  --no-idk-retry \
  --eval-stage b1 \
  --answer-input-cap <frozen-low-cap> \
  --counter-fingerprint <calibrated-fingerprint>
```

分别冻结：

- LoCoMo low/high；
- LongMemEval-S low/high；
- B0 continuity、B1 causal ruler；
- 每个阶段唯一 primary arm/cohort。

高低 cap 的精确值必须在 treatment 前写入 manifest；`<frozen-low-cap>` 不能在看完结果
后回填。每个 protocol 记录 dataset/题目分母、store、answerer/judge/prompt/extractor、
embedding、candidate rule、tokenizer、cap、repetitions、flags 和 git commit。

## 6. 跑 B0 与 B1

WSL2 的长任务必须 detach。下面是目标 CLI 形状；凭据只通过运行时 env，绝不写入 command
示例、log 或 tracked file。

```bash
setsid bash -c '
  go run ./cmd/locomo-bench \
    --eval-protocol "$ENGRAM_022_PROTOCOL" \
    --data "$ENGRAM_022_DATASET" \
    --dataset-format "$ENGRAM_022_FORMAT" \
    --run-dir "$ENGRAM_022_RUN_DIR" \
    --retrieval both \
    --repeats 3 \
    >"$ENGRAM_022_RUN_DIR/run.log" 2>&1
  echo $? >"$ENGRAM_022_RUN_DIR/run.exit"
' </dev/null >/dev/null 2>&1 & disown
```

为该 shell 设置专用变量：

```bash
export ENGRAM_022_PROTOCOL="$ENGRAM_022_SCRATCH/protocol/locomo-low.json"
export ENGRAM_022_DATASET=/absolute/path/to/locomo.json
export ENGRAM_022_FORMAT=locomo
export ENGRAM_022_RUN_DIR="$ENGRAM_022_SCRATCH/runs/locomo-b1-low"
```

轮询只做一次即时检查：

```bash
[ -f "$ENGRAM_022_RUN_DIR/run.exit" ] \
  && sed -n '1p' "$ENGRAM_022_RUN_DIR/run.exit" \
  || tail -1 "$ENGRAM_022_RUN_DIR/run.log"
```

远程 GPU 若用于 answer/extract，遵守
[remote GPU runbook](../../docs/operations/evaluation/remote-gpu-runbook.md)：空闲必停，
每次重启使用现场新凭据，本地 tunnel/runtime 信息不进 repo。正式 stack 不得启用付费
hosted reranker/recall。

B0 验收：

- lossless store；
- 1,540/500 分母完整；
- 三次独立输出与 majority 保存；
- legacy retry 若发生，真实记录；只作 continuity。

B1 验收：

- legacy retry 关闭；
- 每 repetition answerer 一次；
- ranked anchors/rendered candidates、counter/cap 固定；
- legacy packer 在新硬 cap 下运行；
- 后续所有 A/B 指向 B1 control hash。

Judge/oracle 诊断：

- protocol 先冻结全部 discordant + concordant 分层抽样；
- 两位 reviewer 盲标并裁决，保存 raw/corrected score、FN/FP 与一致率；
- fixed-gold Evidence oracle 使用独立 diagnostic arm，不进入正式 accuracy；
- source coverage 连续报告并按预注册 strata 分层，不把外部 `0.95` 当硬阈值。

## 7. 表示 Bake-off

目标 CLI 分开导航和渲染，不把两种实验合成一个分数：

```bash
go run ./cmd/locomo-bench \
  --eval-protocol "$ENGRAM_022_PROTOCOL" \
  --eval-stage representation_navigation \
  --representation chunk_900,raw_turn_window,semantic_episode \
  --run-dir "$ENGRAM_022_SCRATCH/runs/representation-navigation"

go run ./cmd/locomo-bench \
  --eval-protocol "$ENGRAM_022_PROTOCOL" \
  --eval-stage representation_rendering \
  --candidate-replay "$ENGRAM_022_SCRATCH/runs/b1/candidates.jsonl" \
  --representation chunk_900,raw_turn_window,semantic_episode \
  --run-dir "$ENGRAM_022_SCRATCH/runs/representation-rendering"
```

检查：

- Navigation 三臂同算法/embedding/pool/candidate limit；
- Rendering 每题 `anchor_digest` 100% 一致；
- source expansion、gold-source survival、cap truncation 全记录；
- Episode 删除后 Ledger 不变且可确定性重建；
- verdict 为 GO 才能选进 Compiler stage；否则保留旧表示。

## 8. 固定候选 Compiler

先冻结获选表示的完整 `rendered_candidates`，再逐字节 replay：

```bash
for ENGRAM_022_COMPILER in \
  legacy_count \
  exact_token_relevance \
  deterministic_extractive \
  local_planner
do
  go run ./cmd/locomo-bench \
    --eval-protocol "$ENGRAM_022_PROTOCOL" \
    --eval-stage compiler \
    --candidate-replay "$ENGRAM_022_SCRATCH/runs/frozen-rendered/candidates.jsonl" \
    --compiler "$ENGRAM_022_COMPILER" \
    --no-idk-retry \
    --run-dir "$ENGRAM_022_SCRATCH/runs/compiler-$ENGRAM_022_COMPILER"
done
```

正式比较前运行 artifact validator：

```bash
go run ./cmd/locomo-bench \
  --eval-validate "$ENGRAM_022_SCRATCH/runs/compiler-deterministic_extractive"
```

Hard checks：

- candidate set digest 一致率 100%；
- post-freeze retrieval calls 为 0；
- span 复原、MERGE 逐句引用、within-cap、prompt/counter fingerprint 为 100%；
- unattributed ADD=0；
- 每 repetition answerer calls=1；invalid/counter/source/budget error 时为 0；
- Planner fallback 率单列，不把 fallback 冒充 model treatment。
- MERGE 仅在 raw over-cap 且 EXTRACT 仍不满足全部 Need 时可进入验证。

## 9. 条件阶段

只有上一阶段 verdict 与 residual cohort 支持时才运行：

```text
event: E0 / E1-event-object / E2-date-operator / E3-source-recovery
gap: control-one-pass / structured-one-refetch
projection: scene-cross-session / profile-current-state / graph-missing-bridge
```

Gap treatment 必须首轮 `N-r`、补检最多 `r`、union `<=N`，只接受
`entity|time_range|second_operand`，最多一次。Scene/Profile/graph 分开 run；003 graph
合同不改。核心路径已经达到两个数值目标时，后续条件阶段可以停止。

## 10. 生成 Verdict

目标 CLI：

```bash
go run ./cmd/locomo-bench \
  --eval-compare-exact "$ENGRAM_022_SCRATCH/runs/control" \
  "$ENGRAM_022_SCRATCH/runs/treatment" \
  --other-benchmark "$ENGRAM_022_SCRATCH/runs/other-benchmark-treatment"
```

GO 需要同时满足：

- validity 100%；
- primary cohort `Δ >= +2.0pp`；
- two-sided exact McNemar `p < 0.05`；
- 另一 benchmark overall `Δ >= -0.5pp` 且无显著伤害；
- 任一预注册 category 无 Holm-corrected 显著负向；
- judge audit 已完成且 corrected labels 不改变 verdict；
- candidate/gold-source coverage 不退；
- 离线、默认成本和公开 recipe 约束通过。

完整规则见 [evaluation-artifacts.md](./contracts/evaluation-artifacts.md)。最终共同默认还要
达到 LoCoMo ≥1,425/1,540 和 LongMemEval-S ≥473/500。达到北极星不自动等于受控超过
Mem0，除非全栈对齐。

## 11. 提交前总门

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
git diff --check
git diff --name-only -- memory embedding provider store internal
git status --short
```

最后一条 engine diff 在本特性预期非空；它用于逐项确认每个改动确实属于已冻结的 Ledger/
projection/Compiler contract，而非 adapter 越界。MCP-only 子提交必须单独检查并保证该
engine diff 为空。

触及 storage/retrieval/extraction/curation 的提交在合并前必须附 comparable slice；
改变默认 recipe 前必须附两个 full benchmark、逐题 artifact hash 和 GO verdict。
