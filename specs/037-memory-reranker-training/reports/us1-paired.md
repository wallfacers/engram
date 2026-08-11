# US1 Paired Report: Qwen3-Reranker-0.6B Baseline

**Date**: 2026-08-12 | **Run**: 037-us1 | **Protocol**: 008 paired comparison

## Run Configuration

| Item | Value |
|---|---|
| Rerank model | `Qwen/Qwen3-Reranker-0.6B` (base, no training) |
| Serving | transformers + FastAPI `/v1/rerank` (no vLLM; cu128/CUDA 12.8 compat) |
| Embedding | `BAAI/bge-small-en-v1.5` (384-dim, via sentence-transformers) |
| Retrieval | hybrid (semantic + keyword + entity), top-k=30 |
| Answer model | `deepseek-v4-pro` (Anthropic endpoint, thinking disabled) |
| Judge model | `deepseek-v4-flash` (thinking disabled) |
| Extract model | `deepseek-v4-pro` |
| Dataset | LoCoMo 10 conversations, 1540 answerable questions |
| Machine | SeetaCloud RTX 4090 24GB, 224 vCPU, 754GB RAM |
| Commit | `afe1cd9` |

## Overall Results

| Arm | Correct | Total | Rate |
|---|---|---|---|
| hybrid (no rerank) | 1049 | 1540 | **68.1%** |
| hybrid+rerank | 1042 | 1540 | **67.7%** |
| **Delta** | **−7** | | **−0.4pp** |

## Category Breakdown

| Category | hybrid | hybrid+rerank | Δ | Questions |
|---|---|---|---|---|
| single-hop | 67.2% | 67.7% | +0.5pp | 841 |
| multi-hop | 64.2% | **59.9%** | **−4.3pp** 🔴 | 282 |
| temporal | 76.3% | 76.3% | 0.0pp | 321 |
| open-domain | 60.4% | 61.5% | +1.0pp | 96 |

## Flip Analysis

| | hybrid correct | hybrid wrong |
|---|---|---|
| hybrid+rerank correct | 987 | 69 (b→a) |
| hybrid+rerank wrong | 62 (a→b) | 422 |

Net: +7 flips favoring rerank, but category-level detail (see below) shows multi-hop −12 overwhelms the gains.

## Cost

| Role | Model | Calls | In (tok) | Out (tok) | Est. USD |
|---|---|---|---|---|---|
| Answer | v4-pro | 4,047 | 2,649,655 | 26,689 | ~$3.4 |
| Extract | v4-pro | 256 | 335,430 | 325,788 | ~$3.0 |
| Judge | v4-flash | 3,080 | 84,073 | 15,400 | ~$0.1 |
| Rewrite | v4-pro | 555 | 47,088 | 3,891 | ~$0.1 |
| Embed | local GPU | 8,464 | — | — | $0 |
| **Total** | | | | | **~$6.6** |

*Pricing: v4-pro $3/1M in-miss + $6/1M out; v4-flash $1/1M in-miss + $2/1M out. Cache-hit savings not measured (cost.json missing price table; no thinking tokens due to `ThinkingDisabled: true`).*

## Analysis

### Multi-hop is the primary victim (−12 questions, −4.3pp)

The general-purpose reranker's semantic similarity scoring breaks multi-hop reasoning chains. A two-step question ("what did X do after Y happened?") requires retrieving both steps; the reranker deprioritizes the bridging fact in favor of single-step semantic matches. This is the same failure mode diagnosed by xMemory (2602.02007): flat top-k similarity returns redundant highly-correlated surface matches, and rerank amplifies this bias.

### Temporal unchanged (0.0pp)

Unlike 008's bge-reranker-v2-m3 (−9 temporal), Qwen3-Reranker-0.6B did not harm temporal. The R7 audit (18.7% text-visible time signals) predicted temporal insensitivity—the reranker cannot learn time ordering from text it cannot see.

### Single-hop and open-domain marginally benefit

+0.5pp and +1.0pp respectively. Simple fact-lookup benefits from cross-encoder refinement of semantic similarity.

## Comparison with 008 (bge-reranker-v2-m3)

| | 008 (bge-v2-m3) | US1 (Qwen3-Reranker-0.6B) |
|---|---|---|
| Overall delta | −0.06pp | −0.4pp |
| Temporal | −9 questions | 0 questions |
| Multi-hop | not reported separately | **−12 questions** |
| Verdict | NO-GO | NO-GO |

Both general-purpose rerankers fail to convert retrieval improvement to end-to-end QA gain. The failure mode differs (008: temporal; US1: multi-hop) but the conclusion is identical.

## Verdict

**US1 confirms: general-purpose 0.6B rerankers do not convert to end-to-end QA improvement on LoCoMo.** This mirrors the 008 finding and strengthens the motivation for US2 (memory-specific training with LoCoMo ground-truth evidence). The trained model (bce-infonce checkpoint) will be tested next via the same paired protocol.
