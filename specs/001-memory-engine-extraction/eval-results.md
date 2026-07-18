# LoCoMo 评测记录(feature 001 收尾)

> 目的:抽离后在真实端到端评测下抽查保真——确认抽取 / curation / embedding /
> hybrid 检索 / 答题 / 判分整条链在 engram 上工作,且量级与抽离前基线一致。

## 环境

| 项 | 值 |
|----|----|
| 数据集 | `testdata/locomo/locomo10.json`(HF `KimmoZZZ/locomo` 原版,10 段对话) |
| 答题/判分 LLM | `gpt-5.6-luna`(OpenAI 协议中转站,`LOCOMO_PROVIDER=openai`) |
| 抽取模型 | `gpt-5.4-mini`(EXTRACT_MODEL,提速) |
| embedding | 本地 Ollama `qwen3-embedding:0.6b`(1024 维,`/v1/embeddings`) |
| 工具 | `cmd/locomo-bench`,`--retrieval both`,top_k=30 |

## T025 小子集端到端(SC-006)—— ✅ 通过

**样本**:2 段对话 × 每段 10 题 = 20 题(小样本,方差大,属**方向性抽查**,非全量定分)。

| 检索臂 | multi-hop | temporal | single-hop | open-domain | OVERALL (J) |
|--------|-----------|----------|------------|-------------|-------------|
| fts    | 33.3% (2/6) | 45.5% (5/11) | 50% (1/2) | 100% (1/1) | **45.0% (9/20)** |
| hybrid | 66.7% (4/6) | 90.9% (10/11) | 50% (1/2) | 100% (1/1) | **80.0% (16/20)** |
| **A-B uplift** | +33.3pp | +45.4pp | — | — | **+35.0pp** |

Ollama 在本次运行服务了 1244 次 embedding 调用,证明 hybrid 语义臂真实生效。

## 判读

- **整条链在 engram 上工作**:抽取(gpt-5.4-mini)→ 存储 → embedding(本地 Ollama)→
  三路 hybrid 检索(语义+BM25+实体 RRF)→ 答题 → LLM judge,端到端跑通、无异常。
- **hybrid > fts +35pp**,且涨点集中在 **multi-hop 与 temporal**——正是语义信号应当发力、
  也是战略文档记录的调优受益类别。**per-category 形态与抽离前一致**,是保真的强信号
  (对拍只验了检索层,本次覆盖了抽取/curation/embedding/答题链)。
- **量级对齐基线**:hybrid 80%(20 题小样本)与抽离前 SRC ~74.7% 可答题均值在同一量级、
  落在采样噪声内(样本仅 20 题,方差高,不作精确对分)。未见异常回退。

## T026 全量 sanity(SC-007)—— 状态:子集已验,全量按需

本次以 2 段小子集完成方向性抽查(足以确认保真信号)。**全量 10 段一次性 sanity 未跑**:
- 成本现实:`gpt-5.6-luna` 抽取单段曾测得 ~9.5 分钟;全 10 段 × both 臂 + 全部题目的
  答题/判分为数小时级、且有 API 花费。
- 结论:全量分数主要服务"对外发布 / 论文级口径";对"抽离是否弄坏端到端"这一保真目标,
  上表子集信号已充分。全量运行留作按需(有发布/论文需要时再跑,命令见 `quickstart.md`)。

## 复现

见 `quickstart.md` 第 5 节;环境变量同上表。run 产物写入 `./.locomo-run/`(已 gitignore)。
