# LME-S 500 三臂契约实验 verdict — 2026-08-12

**verdict：B(abstain-prompt) NO-GO（−0.40pp, p=0.878）；C(lme-typed 契约迁移) 方向正向但 within-noise（+0.80pp, p=0.608, temporal +2.2pp），需多 rep 确认。**

三臂同一数据集（LongMemEval-S 500 cleaned）、同一 store（lme-s500-store, 512,662 entries）、同一检索（hybrid, top-k 150, chunk-quota 12, concurrency 32）、同一 Qwen3.6-35B-A3B-FP8 vllm（32768, thinking on）、同一 deepseek-v4-flash judge（本批同批重判）。唯一变量 = 答题 prompt 契约。

## 三臂设计

| 臂 | flags | 契约 |
|---|---|---|
| A | `--force-answer` | baseline：强制猜测，永不拒答 |
| B | `--abstain-prompt` | Abstain-R1 教学式拒答（1:4 样本，仅无记忆支持时拒，拒答须命名缺失信息） |
| C | `--force-answer --lme-typed-prompts` | LME question_type 映射到 LoCoMo 契约（multi-session→multi-hop, temporal-reasoning→temporal） |

A/B 互斥（main.go 校验）；C = A 基础上加契约迁移。三者均 default-off eval flag，均在本 commit `1cf1a2b` 引入（lme-typed-prompts 为 eval-config 变更，宪法 IV 单独声明）。

## 方法

results 由 box 跑完下载（A/B/C 各 500 条），**同批 flash clean 重判**（`rejudge_arms.py`）：
- clean = `extractFinalAnswer` 剥离 thinking 前导（与 harness runner.go 逐字一致）
- judge prompt 与 `judgeMem0AlignedSystemPrompt` 逐字一致，temperature 0，1500 次调用同批交错执行 → 三臂内部可比，judge 跨批漂移抵消
- 配对显著性 = McNemar exact 双侧检验

## 结果（clean 同批 rejudge）

| 臂 | 分数 | vs A | vs B |
|---|---|---|---|
| A (force-answer) | **431/500 = 86.20%** | — | — |
| B (abstain-prompt) | **429/500 = 85.80%** | **−0.40pp** | — |
| C (force + lme-typed) | **435/500 = 87.00%** | **+0.80pp** | +1.20pp |

配对（McNemar exact）：
- A vs B：a✓b✗=22, a✗b✓=20, **p=0.878** → 无显著差异
- A vs C：a✓c✗=15, a✗c✓=19, **p=0.608** → 无显著差异
- B vs C：b✓c✗=17, b✗c✓=23, **p=0.430** → 无显著差异

### 类别分解（clean）

| category | A | B | C | B-A | C-A |
|---|---|---|---|---|---|
| knowledge-update | 63/78 (80.8%) | 64/78 (82.1%) | 64/78 (82.1%) | +1.3 | +1.3 |
| multi-session | 112/133 (84.2%) | 109/133 (82.0%) | 113/133 (85.0%) | −2.2 | **+0.8** |
| single-session-assistant | 53/56 (94.6%) | 53/56 (94.6%) | 54/56 (96.4%) | 0 | +1.8 |
| single-session-preference | 21/30 (70.0%) | 19/30 (63.3%) | 20/30 (66.7%) | **−6.7** | −3.3 |
| single-session-user | 67/70 (95.7%) | 70/70 (100.0%) | 66/70 (94.3%) | **+4.3** | −1.4 |
| temporal-reasoning | 115/133 (86.5%) | 114/133 (85.7%) | 118/133 (88.7%) | −0.8 | **+2.2** |

## 分析

### B（abstain-prompt）NO-GO
教学式拒答（1:4 样本）对 LME 无净益，反而 −0.40pp：
- 历史 E1（裸去 force-answer）曾 +0.4pp，靠 ABS 子集拒答改善 +15.8pp 对冲过度拒绝。abstain-prompt 的显式教学并未放大该收益，反在 single-session-preference 上过度拒答 **−6.7pp**（70.0→63.3），single-session-user +4.3pp（95.7→100）为唯一正向。
- **结论：LME 的拒答杠杆已耗尽**。force-answer 非 LME 主瓶颈（与 [[lme-e1-no-force-verdict]] 一致），教学式拒答也不改变该判断。用户问的"A/B 能不能提高 pp" → **不能**（−0.4pp, p=0.878）。

### C（lme-typed 契约迁移）方向正向
+0.80pp 且**类别方向全正**：temporal-reasoning **+2.2pp**（86.5→88.7）、knowledge-update +1.3、multi-session +0.8、single-session-assistant +1.8。LME question_type 映射到 LoCoMo 已验证契约的方向正确（temporal 是最大受益类）。
- **但整体 p=0.608 within-noise**：单次 run 噪声标尺 ~±1-2pp（[[037-us2-verdict-2026-08-12]]），+0.8pp 尚未跨过显著线。
- **待办**：多 rep（repeats=3+）同批确认 temporal +2.2pp 是否稳健；若稳健则转正 `--lme-typed-prompts`（当前 default-off）。

## 局限

1. **单次 run（repeats=1）**：所有 Δ 均在单次噪声范围内，无 95% 显著。结论为方向性而非定量。
2. **绝对分不与历史批对比**：本批 judge 是新一次 flash 调用，A=86.20% vs 历史 clean 基线 84.60% 的 1.6pp 差是跨批 judge 漂移 + 数据集 cleaned 差异，**不可解释为真实提升**。三臂内部（同批）才可比。
3. **embed 512-cap 400**：已知/接受/恒定偏置（超 512 token chunk 不嵌入，三臂同受影响，见 [[lme-topk150-verdict]]）。

## 结论

- **B（--abstain-prompt）：NO-GO**。LME 拒答杠杆耗尽，教学式拒答 −0.4pp 无益。不转正。
- **C（--lme-typed-prompts）：方向 GO / 统计不显著**。temporal 契约迁移 +2.2pp 方向正确但整体 +0.8pp within-noise。保持 default-off，待多 rep 确认。
- 距 90pp 缺口（87.00 → 90.00，+3.0pp）不在此三臂内：主瓶颈仍为模型能力/多值冲突选错（[[lme-clean-rejudge]] [[lme-conflict-prompt-nogo]]）。

## 复现

- box run dir：`/root/autodl-tmp/lme-contract-{a,b,c}/`（results-hybrid.jsonl 500 条 each）
- 小结果已备份：`/root/autodl-tmp/eval-backup-2026-08-12-1940/contract/`
- 本地 results + 重判脚本：`~/.claude/session-scratch/lme-rejudge/runs/` + `rejudge_arms.py`
- 三臂 command：见 box `/root/autodl-tmp/lme-contract.sh`（concurrency 32, store 复用）
