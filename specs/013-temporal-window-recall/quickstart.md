# Quickstart: Retrieval-Side Temporal Window Recall

三阶段验证弧,严格 P1→P2→P3 门控。**US1 门未 GO 不进 US2。**

## US1 — 免费召回诊断门(先跑,零答题 token)

前置:固化 bge-large store(`.locomo-run/…-store` 正本,如 `009-bge-chunks-store`)+ LoCoMo 数据集。box bge-large @8001 或本地 embedder。

```bash
# retrieval-only,零答题/judge token(拟入口,US1 实现后生效)
CGO_ENABLED=0 go run ./cmd/locomo-bench \
  --temporal-diagnostic \
  --store-dir <bge-large-store> \
  --data <locomo.json> \
  --run-dir ./.locomo-run/013-temporal-diag \
  >diag.log 2>&1
```

**期望产物**:四层分层表 —
- Layer 0 `parse_coverage`(时间意图解析覆盖率)
- Layer 1 `event_date_coverage`(gold event 日期覆盖率)
- Layer 2 `buried_ratio` + rank 分位(gold 是否深埋池外)
- Layer 3 `oracle_lift@30`(纯时间窗臂能抬多少深埋 gold 进 top-30)
- 末行 `GO` / `NO-GO(cause=…)`

**门**:四层全过 → 进 US2。任一层塌 → **止损**,把病因(解析器/抽取侧/非召回/天花板不足)落 `docs/locomo-score-levers.md` 收口,feature 结束。

**隔离断言**(US1 期间必须为空):
```bash
git diff --name-only -- memory embedding provider store internal   # 期望:空
```

## US2 — 时间窗召回臂(引擎,条件于 US1 GO)

TDD:先写失败测试(contracts/ 的 test obligations),再实现。

```bash
# 引擎单测(offline,free)
CGO_ENABLED=0 go test -count=1 ./memory/...
# 关注:NamesByEventWindow 范围/相交/半开/回退/降级;臂 parity(关时 byte-identical);
#       深埋 gold 抬升;乘子交互无害;tuning-free 不回归
CGO_ENABLED=0 go build ./...
```

**期望**:全绿。`git diff --name-only -- memory embedding provider store internal` 只反映
`memory/entrystore.go` + `memory/retriever.go`(+ 对应 `_test.go`);`store/` 与 schema 零改。

**parity 硬门**:既有 parity golden(`testdata/parity/`)+ 跨 namespace 隔离测试全绿(臂默认关,无时间意图 query byte-identical)。

## US3 — 端到端配对 eval(box,条件于 US2)

前置:box vllm 答题栈(Qwen @8000)+ bge-large @8001 + SSH 隧道;judge 端点(JUDGE_* env)。**省钱:空闲必停。**

```bash
# 臂 off/on 配对,repeats=3(WSL2 必须 setsid detach)
setsid bash -c '
  for arm in off on; do
    go run ./cmd/locomo-bench --temporal-arm $arm --repeats 3 \
      --store-dir <bge-large-store> --data <locomo.json> \
      --run-dir ./.locomo-run/013-arm-$arm >arm-$arm.log 2>&1
  done
  echo done >013.exit
' </dev/null >/dev/null 2>&1 & disown
[ -f 013.exit ] && echo "done" || tail -1 arm-on.log   # poll
```

**GO 判据**(宪法 IV):temporal 类答题分 on ≥ off 且噪声带外 **且** 非 temporal 类总分不回归。
否则 within-noise / 污染 → NO-GO,诚实收口(不出货、不计赢)。

**收尾**:结论落 tracked `docs/locomo-score-levers.md`;eval-config 改动单独 commit;box teardown(vllm 杀、GPU 0 MiB、隧道拆、凭据仅 env 不落盘)。
