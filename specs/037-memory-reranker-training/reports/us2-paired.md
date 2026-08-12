# US2 Paired Report: Trained Reranker (bce-infonce merged) — NO-GO

**Date**: 2026-08-12 | **Run**: 037-us2 | **Protocol**: 008 paired comparison（与 US1 同协议）

## Run Configuration

| Item | Value |
|---|---|
| Rerank model | **bce-infonce merged**（LoRA r16/α32，9450 样本×3ep，T017） |
| Serving | `tools/server.py`（transformers，score equation 冻结），`HF_HUB_OFFLINE=1` |
| Embedding | `BAAI/bge-small-en-v1.5`（384-dim, sentence-transformers, 无前缀） |
| Retrieval | hybrid（semantic+keyword+entity），top-k=30，**无 --chunks**（与 US1 同） |
| Answer / Extract / Rewrite | `deepseek-v4-pro`（api.deepseek.com/anthropic） |
| Judge | `deepseek-v4-flash` |
| Dataset | LoCoMo 10 convs, 1540 answerable（与 US1 同一 locomo.json） |
| Machine | AutoDL westc RTX 4090 24GB |
| Commit | `afe1cd9`（US1 同款 binary） |

## Overall Results（run 内配对，同 store 自洽）

| Arm | US1 correct | US2 correct | US1 rate | US2 rate |
|---|---|---|---|---|
| hybrid（no rerank） | 1049 | 916 | 68.1% | **59.5%** ⚠️ |
| hybrid+rerank | 1043 | 899 | 67.7% | **58.4%** |
| **Delta（rerank−hybrid）** | **−6** | **−17** | **−0.4pp** | **−1.1pp** |

## Category Breakdown（run 内，hybrid → hybrid+rerank）

| Category | US1 hybrid | US1 rerank | US1 Δ | US2 hybrid | US2 rerank | US2 Δ |
|---|---|---|---|---|---|---|
| single-hop | 67.2% | 67.7% | +0.5pp | 56.7% | 55.1% | **−1.6pp** |
| multi-hop | 64.2% | 59.9% | −4.3pp | 58.9% | 58.5% | −0.4pp |
| temporal | 76.3% | 76.3% | 0.0pp | 70.7% | 72.3% | **+1.6pp** ⭐ |
| open-domain | 60.4% | 61.5% | +1.0pp | 47.9% | 40.6% | **−7.3pp** |

## Flip Analysis（run 内 paired.json，a=hybrid, b=rerank）

| | US1 flips | US2 flips |
|---|---|---|
| a→b（rerank 害） | 62 | 69 |
| b→a（rerank 救） | 69 | 86 |
| **净** | +7（rerank 略害） | +17（rerank 害） |
| temporal 净 | 11/11 对称（0） | **15/10（+5 救）** |

## 基线漂移（关键发现）⚠️

US2 的 **hybrid 基线 59.5% 比 US1 的 68.1% 低 8.6pp**，而两 run 协议逐项一致
（同 binary / 同数据 / 同 retrieval flags / 同 regime / trace 均 fallback / answer
context tokens 654 vs 657 几乎相同）。逐题对比（1540 题 100% 交集）：439 题答案不同，
**US1 独对 286 vs US2 独对 153（不对称 1.87:1）**。

**根因**：两次 run 各自独立执行 extraction（v4-pro LLM 随机，US2 store 3077 vs US1
2944 entries）+ answer（temp=1.0）。**单次 run 的系统噪声尺度 ≈ 8pp**，远大于被测的
rerank 增量（~1pp）。含义：

1. **跨 run 对比（US2 58.4 vs US1 67.7）不可靠**——不能归因于 merged vs base。
2. **run 内配对（同 store、同 extraction 下 rerank vs no-rerank）自洽有效**——是
   008 判定的可靠依据。
3. **008 铁律的单次配对结论存在噪声下限**：检测 ~1pp 量级的 rerank 增益，需要
   repeats 均值 + `--store-dir` 复用（消除 extraction 随机性）才能支撑。

## Verdict: **NO-GO**（008 铁律，run 内配对为准）

- **US2 merged rerank 未转化为端到端增益**（run 内 −1.1pp，与 US1 base −0.4pp
  方向一致）——记忆专用训练（bce-infonce）**第 3 次证伪 rerank e2e 转化**
  （008 bge-v2-m3 → US1 Qwen3-base → US2 trained-merged）。
- **temporal +1.6pp（+5 净翻转）是唯一正向信号**：temporal-hard negatives 训练
  让 merged rerank 在时间域改善（US1 base rerank 为 0.0pp）。但被 single-hop
  （−1.6pp）/ open-domain（−7.3pp）拖累，总体仍负。**局部价值不足以 e2e 转化**。
- **泛化否决门（T021）不适用**：GO 门已不通过，无"泛化"宣称需要否决。
- **分数塌缩佐证**：merged 的 yes_no logit 被 BCE 压入负区（分数 0.14–0.46 vs base
  0.08–0.72），绝对区分度恶化——这可能解释了 open-domain 的 −7.3pp（低分样本间
  排序被噪声主导）。temporal 的 +1.6pp 说明排序在目标类别上仍有效。

## Cost

| Role | Calls | In (tok) | Out (tok) | Est. USD（US1 同定价） |
|---|---|---|---|---|
| Answer (v4-pro) | 4,744 | 3,117,480 | 29,550 | ~$9.5 |
| Extract (v4-pro) | 272 | 348,187 | 335,601 | ~$3.1 |
| Rewrite (v4-pro) | 915 | 77,388 | 6,242 | ~$0.3 |
| Judge (v4-flash) | 3,080 | 83,979 | 15,400 | ~$0.1 |
| Embed (local GPU) | 7,632 | — | — | $0 |
| **Total** | | | | **~$13** |

*US2 answer 调用（4744）与 in-tokens（3.12M）高于 US1（4047 / 2.65M），可能含
retry（embed retries exhausted 4 次）与 rewrite 更多（915 vs 555）。cache-hit 未计入。*

## 与 US1 对比（跨 run 仅作参考，因基线漂移不可直接比）

| | US1（base） | US2（merged） |
|---|---|---|
| rerank 增量（run 内） | −0.4pp | −1.1pp |
| temporal 增量 | 0.0pp | **+1.6pp** |
| 训练后增量（跨 run rerank 臂） | — | 58.4 vs 67.7（**受基线漂移污染**） |
| Verdict | NO-GO | **NO-GO** |

## 建议（方法论）

1. **确认 temporal 信号**：若继续，用 `--store-dir` 复用 US1 store + repeats ≥3，
   消除 extraction 随机性后再判定 temporal +1.6pp 是否真实。
2. **008 配对噪声下限**：所有 ~1pp 级杠杆判定需预注册 repeats 数；单次 run 结论
   一律标注"噪声尺度 ~8pp"。
3. **037 收口**：记忆专用重排训练（0.6B LoRA）在 LoCoMo 1540 上**不转化 e2e**。
   剩余价值在 temporal 局部改善，若要继续需针对性配方（temporal-hard 数据已备，
   R7 审计曾警示 hard 池信号率仅 18.7%）。
