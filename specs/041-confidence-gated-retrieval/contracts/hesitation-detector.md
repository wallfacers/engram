# Contract: Hesitation Detector API

`cmd/locomo-bench/confidence_gate.go` 的公开函数。确定性（FR-002）、从正常生成提取（FR-003）、纯文本规则（research Decision 1）。

## `detectHesitation(pred string) (hesit hesitancy, deepened bool)`

**输入**：`pred` = answerer 单次生成的完整文本（含 thinking 段 + 最终答案，原样保留）。

**输出**：
- `hesit hesitancy` — 犹豫强度，取值：
  - `0` = 自信（`Confident`）
  - `1` = 弱犹豫（`WeaklyHesitant`）
  - `2` = 强犹豫（`StronglyHesitant`）
- `deepened bool` — `true` 当且仅当 `score >= confidenceThreshold`（由 `--confidence-threshold` 决定；强度 → score 的映射见下）。

**强度 → 得分映射**（research Decision 1 规则集，参数化可调）：
- 强信号（拒答 / 明确不确定词 / 多候选未决断）：每个 +3
- 中信号（猜测语气）：每个 +2
- 弱信号（低确信修饰 / 空洞输出）：每个 +1
- `score` = 各类命中权重之和；`hesit` 由 score 分段（`>=6` → 2、`>=3` → 1、否则 0；分段阈值可调）。

**确定性契约**：同一 `pred` → 恒定的 `(hesit, deepened)`。不得调用模型、不得读随机源。

**边界**：
- `pred` 为空 → `hesit=0, deepened=false`（无信号可判，不加深——但空回答本身是弱信号，见下注）。
- `pred` 无 thinking 结构（如 `LOCOMO_NO_THINKING=1`）→ 规则只在 final 文本上跑；`isIDK` 仍生效；最终 `deepened` 判定走 FR-005 回退逻辑（由调用方处理，见 `iterative_retrieval.go` 契约）。

> 注：空回答（`strings.TrimSpace(pred)==""`）既记为「空洞输出」弱信号（+1），也由调用方视为「可能信息不足」。首版不特殊处理，按弱信号走。

## `signalHits(pred string) []signalHit`

（内部函数，供单测与审计。）返回命中的信号明细（`{Signal, Weight, Snippet}`），供 `conf_gate_decisions.jsonl` 审计落盘。

## 依赖

- 复用 `isIDK(pred)`（`runner.go:422`）作为「拒答」强信号的判定。
- 复用 `extractFinalAnswer(pred)`（`runner.go:662`）区分 thinking 段与 final 段（规则在两侧分跑）。
- 不依赖任何检索/作答状态——纯文本纯函数。

## 验证（US1 生死前提）

在**全量**已有 run 的 `results-hybrid.jsonl`（top-k 30 与 150，`correct/gold/predicted`）上验证（research Decision 2）：
- 答错题犹豫率（recall）`>= 60%`
- 答对题犹豫率（假阳性）`<= 30%`
- 两者同时满足才判 US1 通过；不通过 → spec US1 Acceptance 3 停线。
