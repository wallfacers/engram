---
title: 涨点计划 2026-08-12：ENTITY_SHIFT 竞争缓解为主线
summary: 基于 commitment-routes-feasibility 诊断的行动路线图。当前锚点 LoCoMo 91.10% / LME 84.60%（clean）。最大未动用错因是 ENTITY_SHIFT（显著记忆压制，LoCoMo 48% / LME 42%），两文献路线（时间戳/反证据）合计可救仅 ~0.6–1pp。计划：零代码竞争诊断 → 候选压缩/去重（主线）→ 反证据 cohort 验证（副线，default-off）→ LME 时序仅在有可靠写侧元数据时进入。全部机制宪法 IV 归因、单变量、禁止未验证声称涨点。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-12
tags: [plan, locomo, longmemeval, entity-shift, scoring]
---

# 涨点计划 2026-08-12

## 锚点（clean 口径，可复现）

| 数据集 | 分数 | 栈 |
|---|---:|---|
| LoCoMo 1540 | **91.10%** | thinking + top-k150 + Qwen3.6-35B-FP8 + flash judge，3-rep majority（[复现报告](locomo-9110-repro-2026-08-12.md)） |
| LongMemEval-S 500 | **84.60%** | local-first 3-rep majority（[clean 重判](lme-clean-rejudge-2026-08-12.md)） |

## 约束（宪法 + 维护者哲学）

- **宪法 IV**：任何触及 retrieval/extraction/curation/storage/embedding 的改动跑可比较 eval，不回归 baseline；eval-config 与算法改动分开 commit。
- **预算下提质**（`lever-philosophy-signal-not-volume`）：不加 top-k、不加上下文预算、不加量；只用确定性/去噪/验证类机制提精度。
- **无 paid cloud rerank/recall**（DEATH RULE）。
- **纯 Go 确定性优先**：需 LLM 调用的机制（反证据 refine）只能作副线，且 flag default-off。
- 单次 run 噪声 ~8.6pp（037 教训）→ 一切结论需要 repeats≥3 + store 复用。

## 错因地图（诊断依据）

[commitment-routes-feasibility](commitment-routes-feasibility-2026-08-12.md) 量化：
- **ENTITY_SHIFT（显著记忆压制）**：LoCoMo 66/137（48%）、LME 32/77（42%）——最大未动用错因，两文献路线均不覆盖。
- **候选冲突（多值/时序选错）**：LoCoMo temporal 仅 ~23%；LME knowledge-update 44%（7/16 题）。
- **event 锚定/推算错**：LoCoMo temporal 38%——answerer 信 harness `[event:]` 标记，读侧确定性信 event 会固化错误。
- **覆盖错**：LoCoMo ~8 题 temporal / LME ~3 题——gold 记忆未进 thinking。

## 杠杆栈（按优先级）

| 杠杆 | 目标错因 | 预期收益* | 成本 | 机制类型 |
|---|---|---|---:|---|---|
| **L1 候选压缩/去重**（主线） | ENTITY_SHIFT 66 题 | +1–2pp（救 1/4–1/2） | 低·纯 Go 确定性 | 减竞争噪音 |
| **L2 反证据验证门**（副线） | 候选冲突 ~6–10+7 题 | +0.5–1pp | 中·LLM +1 call/题 | answer-conditioned 反查 + KEEP/REVISE |
| **L3 LME 时序确定性选择** | LME knowledge-update 7 题 | +1.4pp | 高·依赖写侧元数据 | 读侧确定性选择 |
| ~~时间戳写侧抽取~~ | — | — | **排除**（027/028 证伪 + LoCoMo 方向反） | — |

\* **全部为机制上限估算（错因映射，非实测）**；实际需 eval 验证。L1 的 1–2pp 是最乐观估计（假定竞争缓解对压制题有效且不反伤大池收益）。

## 里程碑与验证门

### M1 · ENTITY_SHIFT 竞争诊断（零代码）—— ✅ PASS 2026-08-12
- **问题**：top-k150 大池里同人物相似记忆的「竞争密度」是否与错题相关？「显著记忆压制」假设是否成立？
- **方法**：从 3-rep 重判 run-1 的 thinking 提取竞争信号（hes 犹豫词数 / mem_refs 记忆引用量 /
  candidates 值候选数 / event_markers），Mann-Whitney 对比 137 错题 vs 300 随机对题。脚本
  `~/.claude/session-scratch/lme-rejudge/m1_competition_diag.py`，逐题信号 → `m1-wrong-signals.json`。
- **结果**（错 vs 对，全类别，Mann-Whitney U 正态近似 p）：
  | 信号 | 错均值/中位 | 对均值/中位 | p |
  |---|---|---:|---|
  | hes | 6.32 / 3 | 1.78 / 0 | **4.4e-16** |
  | mem_refs | 10.41 / 7 | 7.02 / 4 | **3.2e-10** |
  | candidates | 6.66 / 4 | 5.73 / 3 | 2.3e-03 |
  | event_markers | 7.21 / 4 | 4.45 / 2 | 1.9e-02 |
  四信号全部显著同向；single-hop 错题 hes 5.8 vs 1.7（Δ+4.17）、mem_refs 7.0 vs 4.5（Δ+2.56）。
- **压制样本**：high 竞争错题（conv-9-q-151 Photography→"Fixing cars" mem_refs=19/hes=16、
  conv-8-q-123 helping lost tourists→"Hiking" mem_refs=23/hes=25）——gold 实体常在 thinking 中被
  引用（ENTITY_SHIFT 定义成立）但被更显著记忆带走。
- **结论**：**压制假设成立** → **L1 进入**。错题机制 = answerer 在竞争过载中选显著错误记忆；
  L1 方向 = 降低同人物相似记忆对精确候选的压制。
- **因果注意**：hes/mem_refs 高是错题伴随症状，方向性结论强但「压缩必然改选择」仍需 M2 cohort
  先导实测；避免大池收益张力是 M2 验证门的核心。

### M2 · 候选压缩/去重（主线实现）—— ⚠️ flash 先导 NO-GO 倾向 2026-08-12
- **先导方法**：云机 vllm 被占 → 用 deepseek-v4-flash 做低成本机制验证。16 题（M1 high 竞争单跳 +
  temporal 候选冲突，候选含 gold），从 Qwen thinking 提取候选集 R，同主题聚类去重得 C，flash 分别
  用 R/C 回答（生产 answer prompt）+ flash judge。脚本 `~/.claude/session-scratch/lme-rejudge/m2_flash_verify.py`。
- **结果**：R 基线 8/16 对 → C 7/16。救回 2、有害 3、同 5、双错 6。**逐题核对后可靠救回仅
  conv-3-q-54**（压缩保留 "Sunday prior to October 25" 独特线索，flash 从 2022-06-04 转向正确——
  唯一清晰正向信号）；conv-8-q-102 救回是 judge 泛化假象（gold="grilled dish with salmon"，R 答具体
  chicken 判错、C 答泛化 "Grilled dish" 被判对）。**有害 2 例 = 压缩误删关键记忆**（conv-9-q-48 删掉
  10 月第一周末记忆致 C 拒答、conv-7-q-15 C=1 过度合并丢 Susie）。
- **结论**：**删除型去重 NO-GO 倾向**。两个原因：(a) 压缩不知道哪条记忆承载 gold，删除有误伤固有
  风险；(b) **flash 不是 Qwen 压制行为的可靠代理**——flash 基线 R 就 8/16 对（Qwen 这 16 题全错），
  且 judge 对同主题有语义宽松噪声（conv-9-q-48）。

### M2b · 聚焦排序（非删除）—— ❌ 无效果，L1 收口 2026-08-12
- **机制**：候选按「问题主题词相关性(剔人物名) + 独特性」加权重排（保留全部，只改顺序），验证
  "减竞争噪音"的非删除表达。脚本 `m2b_flash_focus.py` → `m2b-flash-results.json`。
- **结果**：16 题 R 6/16 对 = D 6/16 对，**救回 0 / 有害 0，答案完全不变**。即使 dry-run 确认 gold
  相关记忆被排前（conv-9-q-147 的 "writing lyrics and notes" 排第 1、conv-9-q-151 的 photography 线索
  前置），flash 的 final 逐字不变（conv-9-q-151 仍答 "Fixing cars"）。
- **结论**：**L1（候选侧减竞争：删除/排序）收口 NO-GO**。两轮 flash 覆盖两种候选侧机制均不成立——
  删除有误删风险、排序无任何效果。**决定性证据**：gold 线索被明确排在最前，flash 仍坚定选显著错误
  候选 → **压制是模型本身的判断/偏好问题，不是候选呈现顺序或数量问题**（flash 与 Qwen 同病）。
- **影响**：ENTITY_SHIFT 无候选侧快杠杆。剩余候选侧方向需 model-side（训练/推理偏好调整，SaaS 类）或
  放弃该类。涨点计划主线清空 → 看 L2 反证据（副线）是否仍是唯一候选机制。

### M3 · 反证据 cohort 验证（副线）—— ✅ flash 先导正向信号 2026-08-12
- **机制**：harness `--counter-refine` flag（default-off）——草稿 `a0` → `(q, a0)` 反查 → KEEP/REVISE 门 + 确定性验证。
- **flash 先导**（`m2c_flash_revise.py` → `m2c-flash-results.json`）：取 M2b flash R 答错的 10 题，给
  「R 里含 gold 词的记忆」作 answer-conditioned 反证据 + 显式验证/纠错 prompt（draft vs counter-evidence，
  KEEP/REVISE）。**结果 2/10 真实救回**——conv-8-q-60（Painting→Kayaking）、conv-8-q-96（拒答→
  strength and resilience），逐题核对非 judge 假象。
- **关键对比**：候选排序（M2b）0/16 救回 vs 验证框架（M2c）2/10——**验证/纠错框架本身触发选择改变，
  CounterRefine 机制成立**（不是"重新看候选"，而是"验证草稿"）。顽固压制题（conv-9-q-151 Fixing cars、
  conv-3-q-54）反证据也救不回 → 收益上限有限。
- **门**：✅ 通过（机制有真实信号）→ **harness 实现完成（2026-08-12）**：
  - `--counter-refine` flag（default-off）已实现：`cmd/locomo-bench/main.go`（options 字段 + flag 注册 +
    `counterRefineAnswer`/`selectCounterEvidence`/`counterRefineKeys`/`counterRefineHit`/`counterRefineUserPrompt`
    函数 + 插入点=IDK retry 后 judge 前），`runner.go`（`counterRefineSystemPrompt` 常量），
    `eval_runner.go`（`densityMechanismKeys` + `densityMechanismFlagsForOptions` + `isFormalControlMechanismFlags`
    归因）。
  - 机制：草稿 a0 → **候选内反证据**（hits 里含 a0 关键词的记忆优先，fallback 头部，cap 截断）→
    REVISE prompt（+1 LLM call）→ 空/IDK/err 回退 a0 保持草稿（fail-safe）。
  - 单元测试 `counter_refine_test.go`（keys 提取/证据筛选/回退/归因）全过；
    `CGO_ENABLED=0 go build ./...` + `go test ./cmd/locomo-bench` 全绿。
  - 全量验证需云机 Qwen vllm（当前被占）；默认 off 保证 byte-identical（宪法 IV）。
- **诚实边界**：小样本（10 题）2/10 是方向信号非精确率；救回集中在"草稿错但候选含 gold 信息"的题。

### M4 · LME 时序确定性选择（条件进入）
- **前置**：先诊断 LoCoMo `[event:]` 标记质量（多少题 event 标记与 gold 事件日期一致）；若标记不可靠，关闭此线（与 M2 的 conv-4-q-10/80 教训一致）。
- **机制**：读侧「结构化 event 优先 + 文本推算 fallback」规则，把新旧/时序选择移出 LLM 判断。
- **门**：标记质量诊断 PASS 才进入；否则不投（避免重蹈 027/028）。

## 执行顺序建议

1. **M1 零代码诊断**（今天可做，零成本）→ 决定主线方向。
2. M2 cohort 先导（低成本，先看选择是否改变）。
3. M3 反证据 cohort（可与 M2 并行）。
4. M1/M2/M3 的 cohort 结果齐了 → 决策：全量宪法 IV eval 走哪条（或都不走）。
5. M4 仅当 M1 显示 event 标记可靠才开。

## 预期收益（诚实边界）

- 全部杠杆收益为**机制上限估算**，非实测；任何宣称必须在宪法 IV eval 后。
- L1 乐观 +1–2pp（LoCoMo），L2 +0.5–1pp，L3 +1.4pp（LME）——乐观叠加 ~2–3pp，但每条都有独立验证门，允许任意一条 NO-GO 收口。
- 若 M1 显示竞争假设不成立，L1 关闭，主线上限即降至 L2（~1pp）——计划本身接受该结果。

## 与既有文档衔接

- 诊断依据：[commitment-routes-feasibility](commitment-routes-feasibility-2026-08-12.md)、[commitment failure 审计](../research/committed-failure-retrieval-wrong-answer.md)。
- 错因画像：[LoCoMo 错题画像](locomo-error-patterns-2026-08-12.md)（机制三=显著记忆压制，sweep 配对证 k150 不反伤 single-hop）。
- LME 90 目标：[90pp 方向探索](90pp-direction-exploration.md)。
- 竞争噪音文献：Separating Semantic Competition（arXiv:2605.27294，竞争者越多越有害）。
