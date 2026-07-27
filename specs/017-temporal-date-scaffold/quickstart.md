# Quickstart: 确定性日期脚手架 — 怎么验证

**Feature**: 017-temporal-date-scaffold | **Date**: 2026-07-27

两层验证,**严格分开**:第 1 层零成本、可断网跑,钉死"实现对不对";
第 2 层花钱花 box,只回答"有没有用"。**没过第 1 层不准进第 2 层。**

---

## 第 1 层:离线正确性(US1)—— 零成本,任何人任何时候可跑

### 前置

无。不需要数据集、不需要 embedding sidecar、不需要 LLM 端点、不需要网络。

### 跑

```bash
# 硬门:CGO 关闭下构建 + 测试
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/

# 只看脚手架相关断言
CGO_ENABLED=0 go test -count=1 -run 'Timeline|Scaffold' ./cmd/locomo-bench/ -v

# 引擎零改硬验证(必须无输出)
git diff --name-only -- memory embedding provider store internal
```

### 期望

- 全部测试 PASS,含 [contracts/scaffold-contract.md](./contracts/scaffold-contract.md)
  契约测试清单的 **CT-1 ~ CT-17** 共 17 条;
- `git diff` 对引擎目录**无任何输出**;
- 构建零错误、零警告。

### 这一层证明了什么 / 没证明什么

✅ 证明:脚手架**算得对**(排序/编号/推导/跨度)、**降级正确**(不臆造)、
**确定**(重复调用逐字节相同)、**关掉等于不存在**(逐字节不变)。

❌ **不**证明:它对答题准确率有任何帮助。**不得**用本层的绿测宣称任何涨点
——这正是 008 铁律反复惩罚的模式。

---

## 第 2 层:端到端门(US2)—— 需显式成本授权 + box

> ⚠️ **未获授权不得执行。** 本层产生答题与判题的 token 成本,并占用租用 GPU box
> (计费,**空闲必停**)。

### 前置

- LoCoMo 数据集就位、store 已建(复用既有 store,不重建 —— 抽取零成本);
- box 上 vllm 已起、SSH 隧道已通(隧道 MUST 打包进 `setsid` 脚本内,否则脚本活着隧道死);
- judge 端点配置就位(`JUDGE_*` env);
- **凭据只走 env**,绝不写进脚本文件、日志或产物。

### 臂设计(三臂,一次 run)

| 臂 | 配置 | 作用 |
|---|---|---|
| `base` | canonical recipe,脚手架**关** | 干净基线 |
| **`ref`** | **与 `base` 完全相同,只是重跑一遍** | **噪声标尺 —— 不可省** |
| `scaffold` | canonical recipe + `--temporal-date-scaffold` | 处理臂 |

> **为什么 `ref` 不可省**:LongMemEval 消融实测「同配置重跑差 2 分、per-rep 带宽 9–10 分」。
> 没有 `ref`,`scaffold` vs `base` 的任何差值都无法与噪声区分——那张表会被读成
> "有效/有害",而真相可能是两条结论都在噪声里。这是 FR-012。

### 跑法(WSL2 硬纪律:必须 detach)

```bash
# 长跑必须 setsid 脱离;>log 2>&1 单独不够(Bash 工具靠 stdout EOF 判完成)
setsid bash -c '<隧道启动> && go run ./cmd/locomo-bench ... >run.log 2>&1; echo $? >run.exit' \
  </dev/null >/dev/null 2>&1 & disown

# 轮询:单次即时检查,绝不用前台 sleep 循环
[ -f run.exit ] && echo "exit=$(cat run.exit)" || tail -1 run.log
```

日志与中间产物 → 会话 scratchpad,**不进仓库**。

### 冷启动纪律

box **冷启动首臂偏低约 2.25pp**(014 assoc 诊断实测,险些酿成假 GO)。
故:**首臂结果丢弃或复跑**,配对检验只对**干净复跑基线**做,
**绝不**拿冷首臂当 `base`。

### 判据(GO / NO-GO)

```
GO  ⟺ temporal 类(n=321)配对检验显著抬升  AND  overall 不回退
```

任一不满足 = **NO-GO**。此外必须同时产出以下五项,**缺一即判 inconclusive 而非 GO**(SC-006):

1. temporal 类准确率变化;
2. 配对显著性(McNemar,`scaffold` vs 干净 `base`);
3. overall 回退检查;
4. `ref` vs `base` 的噪声标尺差值;
5. 答题上下文 **token 增量**实测(FR-013)。

### 跑完必核

- `regime.json` 四要素与预期一致,且 `scaffold` 臂的 fingerprint 含
  `;temporal_date_scaffold=true`(否则开关根本没生效,整轮作废);
- 名义上限对照:答题侧 temporal 38 题 ≈ 全量 **2.47pp**(台账标注"实际远低")。
  **任何超过该上限的"增益"都是错误信号,不是好消息** —— 先查 bug 再说。

---

## 第 3 层:收口(US3)—— 零成本,不可省

无论 GO 还是 NO-GO:

1. verdict(判据 + 五项数字 + 归因 + 实测成本 + 产物指针)落
   [`docs/locomo-score-levers.md`](../../docs/locomo-score-levers.md);
2. 更新该文「剩余未验方向盘点」中本方向的状态(当前标为"已立项 017");
3. NO-GO 时归因**必须**区分三种:**思路错** / **上下文被稀释**(014 翻车模式) /
   **落在噪声内**(看 `ref` 标尺);
4. 产物核无凭据泄漏后再归档。

> 本项目纪律:实验结论只进会话记忆会在换环境时丢失,**必须落 tracked docs**。

---

## 回滚

若 NO-GO:开关维持默认关即可(canonical recipe 从未受影响)。
需要彻底移除时,`cmd/locomo-bench/timeline.go` + `timeline_test.go` 可原子删除,
其余改动是签名扩展,还原为传 `""` / 不传参即可 —— 回滚面在设计时已刻意收窄(plan 的 Structure Decision)。
