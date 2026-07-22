# Quickstart: 归因门控的检索排序

验证 US1(near-free,现在可跑)与 US2(gated,需机器窗口)的运行/验证指南。实现细节见 contracts/ 与 data-model.md。

## 前置

- 008 固化 store 可用(retrieval-only 复用,不重抽取)。
- 已归档答题结果 `.locomo-run/008-us4-e2e/results-hybrid.jsonl`(四象限 join 源)。
- `CGO_ENABLED=0` 工具链;LoCoMo dataset(gitignored)。
- US1 **不需要**任何答题/embedding 云端点(retrieval-only + 本地 embedding sidecar)。

## US1 — 归因 trace(FREE,先交付)

```bash
# 1) 单测先行(TDD):确定性 golden 应先失败再实现
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run Attribution

# 2) 跑归因(retrieval-only,零答题 token)
setsid bash -c 'go run ./cmd/locomo-bench --attribution-trace \
  --data <locomo.json> --run-dir .locomo-run/009-attribution \
  --store-dir <008 store> --retrieval hybrid --top-k 30 --chunks \
  --join-results .locomo-run/008-us4-e2e/results-hybrid.jsonl --embed-probe \
  >009.log 2>&1; echo $? >009.exit' </dev/null >/dev/null 2>&1 & disown
[ -f 009.exit ] && echo "exit=$(cat 009.exit)" || tail -1 009.log   # 轮询(WSL2 detach 规则)
```

**期望产物**(`.locomo-run/009-attribution/`):
- `trace.jsonl` — 每题一条归因(schema 见 contracts/attribution-cli.md)。
- `quadrant-distribution.json` — 四象限分布表。
- `embed_probe.json` — embedding 确定性判定。

**验收(对应 SC)**:
- SC-001:gradeable 题 100% 有归因记录;unresolved 单独计桶。
- SC-002:诊断"排序题"≥90% 落 Q3 且带 `gold_rank`+`outranked_by`。
- SC-003:log 中答题模型调用数 = 0。
- SC-004:重跑一次,`diff` 两份 trace.jsonl 逐字段一致。
- SC-005:`git diff --name-only -- memory embedding provider store internal` 为空。

**交付**:US1 单独 commit(adapter,引擎零改)。

## US2 — 定向排序(GATED,US1 证据后)

1. 读 `quadrant-distribution.json` 的 Q3 主导模式 → 从三候选(score-aware RRF / MMR 去重 / 实体·时间锚)选一(research D6)。
2. **门①纯 Go 契约**:
   ```bash
   CGO_ENABLED=0 go test -count=1 ./memory   # parity(关时逐字节)+ 新排序单测
   CGO_ENABLED=0 go build ./...
   ```
3. **门②离线归因**:开 `RankingRefine` 复跑 US1 归因,断言 Q3 象限 gold 平均排名上升(无云/付费杠杆)。
4. **门③端到端决胜**(机器窗口):
   ```bash
   # 同机配对,唯一变量=排序机制;先目标类后全量 1540
   go run ./cmd/locomo-bench --data <locomo.json> --run-dir .locomo-run/009-e2e \
     --retrieval both   # hybrid vs hybrid+RankingRefine
   ```
   判定:配对 McNemar above-noise + overall 及任一非目标类不显著回退,超越反证基线(−0.3pp/p=1.0、−0.06pp/p=1.0)。
5. **落账**:GO/NO-GO 结论 → `specs/009-*/eval-log.md` + `docs/locomo-score-levers.md`;coverage 只作诊断。US2 单独 commit(engine)。

## 快速自检清单

- [ ] US1 单测先失败后通过(TDD)
- [ ] trace.jsonl / distribution / embed_probe 三产物齐
- [ ] US1 引擎零改(git diff 空)、答题调用=0
- [ ] US2 parity 关时零变
- [ ] US2 判定唯一由端到端配对 McNemar 决定,coverage 不作 verdict
