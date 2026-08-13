---
title: 涨点计划复盘 2026-08-12：ENTITY_SHIFT 与反证据路线
summary: 基于 commitment-routes-feasibility 诊断启动、并按后续实验证据更新的行动记录。当前锚点 LoCoMo 91.10% / LME 84.60%（clean）。ENTITY_SHIFT 候选删除/排序已收口；counter-refine+trace 组合臂无正向信号且独立效应未识别；LME 时序路线仍以可靠写侧元数据为前置。全部机制按宪法 IV 单变量归因，禁止把错因上限或混合臂观测写成实测涨点。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
tags: [plan, locomo, longmemeval, entity-shift, scoring]
---

# 涨点计划复盘 2026-08-12

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
- 037 在重新抽取 store、随机回答的两次独立 run 中曾观察到 8.6pp 漂移；这不是通用噪声尺度，
  但足以要求 repeats≥3、store 复用与配对报告。

## 错因地图（诊断依据）

[commitment-routes-feasibility](commitment-routes-feasibility-2026-08-12.md) 量化：
- **ENTITY_SHIFT（显著记忆压制）**：LoCoMo 66/137（48%）、LME 32/77（42%）——最大未动用错因，两文献路线均不覆盖。
- **候选冲突（多值/时序选错）**：LoCoMo temporal 仅 ~23%；LME knowledge-update 44%（7/16 题）。
- **event 锚定/推算错**：LoCoMo temporal 38%——answerer 信 harness `[event:]` 标记，读侧确定性信 event 会固化错误。
- **覆盖错**：LoCoMo ~8 题 temporal / LME ~3 题——gold 记忆未进 thinking。

## 杠杆路线状态

| 杠杆 | 目标错因 | 当前证据 | 成本 | 机制类型 |
|---|---|---|---:|---|
| **L1 候选压缩/去重** | ENTITY_SHIFT 66 题 | 删除 7/16 vs 8/16；排序 6/16 vs 6/16，已收口 | 低·纯 Go 确定性 | 减竞争噪音 |
| **L2 反证据验证门** | 候选冲突 ~6–10+7 题 | 混合臂 −0.4pp、`p=0.8776`；独立效应未识别，按成本停止 | 中·LLM +1 call/题 | answer-conditioned 反查 + KEEP/REVISE |
| **L3 LME 时序确定性选择** | LME knowledge-update 7 题 | 未验证；约 +1.4pp 仅为错因覆盖上限 | 高·依赖写侧元数据 | 读侧确定性选择 |
| ~~时间戳写侧抽取~~ | — | — | **排除**（027/028 证伪 + LoCoMo 方向反） | — |

表中的 L1/L2 是已观察结果，不是跨实验可叠加的收益；L3 的数字只是错因映射上限，必须经独立 eval
验证后才可作为实测结果。

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

### M3 · 反证据 cohort 验证（副线）—— ⚠️ 组合臂无正向信号，独立效应未识别 2026-08-13
- **机制**：harness `--counter-refine` flag（default-off）——草稿 `a0` → `(q, a0)` 反查 → 显式 LLM KEEP/REVISE 验证/纠错门；候选选择、结果解析与失败回退是确定性的。
- **flash 先导**（`m2c_flash_revise.py` → `m2c-flash-results.json`）：取 M2b flash R 答错的 10 题，给
  「R 里含 gold 词的记忆」作 answer-conditioned 反证据 + 显式验证/纠错 prompt（draft vs counter-evidence，
  KEEP/REVISE）。**结果 2/10 真实救回**——conv-8-q-60（Painting→Kayaking）、conv-8-q-96（拒答→
  strength and resilience），逐题核对非 judge 假象。
- **关键对比**：候选排序（M2b）0/16 救回 vs 验证框架（M2c）2/10——验证/纠错框架在这个小样本中
  确实触发了答案改变（不是"重新看候选"，而是"验证草稿"）。顽固压制题（conv-9-q-151 Fixing cars、
  conv-3-q-54）反证据也救不回 → 收益上限有限。
- **历史实现门**：flash 定向先导达到当时的实现门，但不构成总体效应证据；**harness 实现完成（2026-08-12）**：
  - `--counter-refine` flag（default-off）已实现：`cmd/locomo-bench/main.go`（options 字段 + flag 注册 +
    `counterRefineAnswer`/`selectCounterEvidence`/`counterRefineKeys`/`counterRefineHit`/`counterRefineUserPrompt`
    函数 + 插入点=IDK retry 后 judge 前），`runner.go`（`counterRefineSystemPrompt` 常量），
    `eval_runner.go`（`densityMechanismKeys` + `densityMechanismFlagsForOptions` + `isFormalControlMechanismFlags`
    归因）。
  - 机制：草稿 a0 → **候选内反证据**（hits 里含 a0 关键词的记忆优先，fallback 头部，cap 截断）→
    REVISE prompt（+1 LLM call）→ 空/IDK/err 回退 a0 保持草稿（fail-safe）。
  - 单元测试 `counter_refine_test.go`（keys 提取/证据筛选/回退/归因）全过；
    `CGO_ENABLED=0 go build ./...` + `go test ./cmd/locomo-bench` 全绿。
  - 2026-08-13 Qwen LME 500 已跑完，但 treatment 实际为 `counter-refine + trace-mediation`，对照为
    trace-off baseline：432/500 vs 434/500，McNemar `p=0.8776`。**该组合配方无正向信号**；trace
    未对齐，不能据此识别 counter-refine 独立效应。
- **资源裁决**：额外一次 answer 调用/题没有换来组合臂收益，路线可按成本停止投入；这不等同 isolated
  causal NO-GO。完整边界见 [全量组合臂报告](counter-refine-verdict-2026-08-13.md)。
- **诚实边界**：小样本 2/10 仅为启动全量的方向信号；全量又有 trace 混杂。当前既不能声称涨点，也
  不能声称 counter-refine 单机制已被因果证伪。

### M4 · LME 时序确定性选择（条件进入）
- **前置**：先诊断 LoCoMo `[event:]` 标记质量（多少题 event 标记与 gold 事件日期一致）；若标记不可靠，关闭此线（与 M2 的 conv-4-q-10/80 教训一致）。
- **机制**：读侧「结构化 event 优先 + 文本推算 fallback」规则，把新旧/时序选择移出 LLM 判断。
- **门**：标记质量诊断 PASS 才进入；否则不投（避免重蹈 027/028）。

## 执行顺序建议

1. **M1 已完成**：竞争症状显著，但只建立相关性。
2. **M2/M2b 已完成并收口**：删除会误伤，排序没有改变答案；L1 不进入全量。
3. **M3 已完成混合臂验证**：组合配方无正向信号；因 trace 混杂，独立效应仍未知，但按调用成本停止投入。
4. **M4 暂不进入**：只有新的写侧时间元数据可靠性证据通过预注册门，才设计单变量实验。
5. 后续任何新杠杆先冻结假设、配置、held-out 与停止规则，再运行可比较的宪法 IV eval。

## 预期收益（诚实边界）

- 历史错因映射给出的收益数字都只是**机制上限估算**，不是实测；任何新收益宣称必须在宪法 IV eval 后。
- L1 已由 M2a/M2b 收口；L2 的全量混合配方实测 −0.4pp、独立效应未识别，不再保留 +0.5–1pp
  的收益预期；L3 仍只是未验证上限估算。
- 任何后续收益必须来自重新预注册且配置完全对齐的实验，不能叠加这里的历史乐观估计。

## 与既有文档衔接

- 诊断依据：[commitment-routes-feasibility](commitment-routes-feasibility-2026-08-12.md)、[commitment failure 审计](../../research/committed-failure-retrieval-wrong-answer.md)。
- 错因画像：[LoCoMo 错题画像](locomo-error-patterns-2026-08-12.md)（机制三=显著记忆压制，sweep 配对证 k150 不反伤 single-hop）。
- LME 90 目标：[90pp 方向探索](90pp-direction-exploration.md)。
- 竞争噪音文献：Separating Semantic Competition（arXiv:2605.27294，竞争者越多越有害）。
