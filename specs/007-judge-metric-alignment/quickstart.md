# Quickstart: LoCoMo Judge 口径对齐 验证

验证 US1 免费交付(不量化新分)。所有命令 `CGO_ENABLED=0`。

## 前置

- 仓库根,分支 `007-judge-metric-alignment`。
- golden 层需可选 judge endpoint(003 冻结 relay,如 gpt-5.6-luna),经 env 提供;无则该层跳过。

## 1. 引擎零改门(硬门)

```bash
git diff --name-only master...007-judge-metric-alignment -- memory embedding provider store internal
# 期望:空输出
```

## 2. 构建 + 离线确定性测试(进 CI,零 LLM)

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/...
```
期望:全绿。含
- mem0-aligned 模式 `judgeSystemPrompt` 含 7 条规则关键子句(部分给分 / ±14 天 / 情绪同价 / …)。
- `parseJudgeVerdict` 表驱动正确。
- flag off 时 judge prompt 与改动前逐字一致(SC-005 回归断言)。

## 3. 近免费 golden 层(env-gate,防放水)

```bash
LOCOMO_JUDGE_GOLDEN=1 \
LOCOMO_API_KEY=... LOCOMO_BASE_URL=... LOCOMO_MODEL=... \
CGO_ENABLED=0 go test -count=1 -run TestJudgeMem0AlignedMatchesGolden ./cmd/locomo-bench/
```
期望:
- 每条"宽松该判对"项 → CORRECT(SC-002)。
- **每条 anti-放水陷阱项 → WRONG(SC-001);任一被判对即红。**
- 成本:仅夹具 ~25-30 次判分调用(近免费),零答题调用(SC-003)。

## 4. fingerprint 口径隔离(SC-006)

```bash
# 对比两次 run 的 fingerprint(可从 run 元数据/日志读取):
#   off: force_answer=...;abstain_prompt=...;no_idk_retry=...
#   on : ...;judge=mem0-aligned
```
期望:两者不同 ⇒ 新口径分不会与旧 judge 65.4% 基线被静默配对。

## 不在 US1 范围(勿做)

- 全量重判 / 重跑量化新基线 → US2,需显式成本授权。
- 翻默认 judge → 待 US2 声明新基线后单独决策。
- 答题 prompt 5 步升级 → 另一特性。

## 新基线声明(US2,授权后)

同 transcript 双口径对跑或重判 → eval-log 新节 "Judge 口径 v2 (mem0-aligned) — 新基线 X%(老 65.4%)" + flip 抽查;**eval 结果单独提交**,明标"口径对齐非涨点"。
