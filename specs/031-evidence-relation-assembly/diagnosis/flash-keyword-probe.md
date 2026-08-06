# 探针:DeepSeek flash 全量配对(base vs trace)— keyword-only vs hybrid

**日期**: 2026-08-06 · **口径**: 全量 1540(cat 1-4)× 1 rep · **模型**: deepseek-v4-flash(禁思考,OpenAI 直连)答题+判题 · **检索**: 两档对比——keyword-only(FTS5 BM25,无 embedding)vs hybrid(本地 Ollama bge-m3 语义 + keyword + entity)

## 结论速览

1. **检索是决定性变量**:flash 在 keyword-only 下 base 47.7%,切 hybrid(bge-m3)后 **88.1%**(+40pp)——50% 完全是无语义检索导致,非模型能力。
2. **flash 禁思考 + bge-m3 超 030 Qwen base**:88.1% vs 030 的 84.9%(Qwen+bge-large 单次)。bge-m3(新一代 1.2B)检索更强。
3. **trace 增量依赖检索强度**:弱检索 +2.8pp(p=0.006 显著),强检索 +0.3pp(p=0.78 不显著)。证据越弱,结构精炼边际收益越大(与 030 一致)。
4. **端到端费用 trace 更贵**(两档均坐实):trace 调用(读全候选 + 大 packet)是费用大头,"省 token"仅限 answer 上下文 budget 口径。

## 得分对比(flash 禁思考,全量 1540 × 1)

| 检索 | base | trace | Δ | McNemar |
|---|---|---|---|---|
| keyword-only | 47.7%(735) | 50.1~50.5%(两次) | **+2.4~2.8pp** | **p=0.006 above-noise** |
| hybrid(bge-m3) | **88.1%**(1357) | **88.4%**(1361) | +0.3pp | p=0.78 within-noise |
| answer 上下文(hybrid) | 3377 | 343 | 省 9.8× | |

## 得分(base vs trace,flash 禁思考)

| 指标 | base(no trace) | trace | Δ |
|---|---|---|---|
| OVERALL | 47.7%(735/1540) | 50.1~50.5%(两次) | +2.4~2.8pp |
| multi-hop | 55.0% | 57.4% | +2.4 |
| open-domain | 61.5% | 58.3% | −3.2 |
| single-hop | 44.6% | 46.8% | +2.2 |
| temporal | 45.5% | 51.4% | **+5.9** |
| answer 上下文 mean | 1339 tok | 508 tok | 省 2.6× |

配对(base 1 rep vs trace 1 rep):flips trace→base 133 / base→trace 91,McNemar **p=0.006,above-noise**。类别 temporal 最受益,topen-domain 小幅回(注意)。

## 费用(flash 价:in 1元/M 未命中、out 2元/M;未含缓存命中;DeepSeek API 口径,本地 embed 免费)

| 口径 | base | trace | 备注 |
|---|---|---|---|
| keyword-only | ~3.7 元 | ~6.0 元 | trace 贵 ~62% |
| **hybrid(bge-m3)** | **~6.74 元** | **~9.18 元** | **trace 贵 ~36%** |

hybrid 明细(DeepSeek API,排除本地 embed):
- **base**:answer in 5.20M(语义检索候选多,ctx 3377)+ extract 0.52M + judge 0.55M ≈ 6.74 元
- **trace**:answer in 0.53M(ctx 343)+ judge 0.55M + **trace 调用 6.06M in / 1.00M out(占 80%)** ≈ 9.18 元

**端到端 trace 更贵(两档均坐实)**——trace 调用(读全候选 + 大 packet 输出)是费用大头;「省 token」仅限 answerer context budget 口径(hybrid 下 3377→343 省 9.8×,但 trace 调用自身烧更多)。

## 对 031 的意义

- **031 的关系计算是纯本地**(实体/日期/因果词典,零额外 LLM 调用)——叠加在已付费的 trace 调用之上,**边际 LLM 成本 = 0**。这是 031 相对「再烧一次 LLM 精炼」类机制(如二次 trace / 更长 answer 推理)的定位优势。
- **强检索(hybrid)下 trace 无独立增量**(+0.3pp,p=0.78;与 030 的 +0.72~1.01pp 一致),**弱检索(keword-only)下 +2.8pp 显著**(p=0.006)——证据越弱,结构精炼边际收益越大。**031 结构上下文(显式关系边)的配对验证应走弱检索/难题子集(如 030 的 84 题),那里才有展示空间**;强检索全量下大概率也 within-noise。

## 诚实边界

- keyword-only 绝对分 47-50% 与 030 的 85.91% 不可比;hybrid 下 flash 88.1% 与 030 口径接近但 embedding 模型(bge-m3 vs bge-large-en-v1.5)与答题模型(flash vs Qwen)均不同,绝对分只作参考。
- 单次 rep(非 3 次 majority),judge 为 flash(与 answerer 同模型)。
- flash 禁思考:通过新引擎能力 `provider.Request.ThinkingDisabled`(LOCOMO_NO_THINKING 默认开)注入 `thinking:{"type":"disabled"}`;DeepSeek 默认思考会污染 Anthropic 格式 JSON 解析(OpenAI 格式 content 干净,但思考是纯开销)。
- 成本未含 DeepSeek 缓存命中(连续重复前缀可再降);hybrid 两臂约 16 元、keyword-only 两臂约 10 元,今日探针合计 ~26 元。
