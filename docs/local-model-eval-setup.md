# 本地开源模型评测栈(摆脱中转站)

**目的**:LoCoMo/locomo-bench 评测的三个 LLM 角色(抽取 / 答题 / 判官)长期依赖第三方"中转站"relay,极不稳定(整段 502 "Upstream access forbidden"、并发限流、reasoning 模型隐藏 token)。本文档记录如何用一台 **80G GPU** 跑量化开源模型 + 本地 embedding,做到**离线、可复现、可移植**的评测栈。这与宪法 I(local-first)一致,且**不触碰死规则**(死规则只禁"付费云 rerank 涨点";自托管开源模型是纯客户端、可移植,反而更对)。

> 状态:计划稿(2026-07-21)。embedding 侧(fastembed sidecar)已在用;LLM 侧待部署。

## 各角色的模型要求(由低到高)

| 角色 | 难度 | 每题 token(实测,LoCoMo hybrid top-k=30) | 说明 |
|---|---|---|---|
| 判官(judge) | 低 | ~450 in / ~10 out(+reasoning 暗账) | 判 predicted 是否命中 gold;无检索上下文,最便宜 |
| 抽取(extract) | 中 | 一次性/对话,`--store-dir` 复用后免 | 从对话轮次抽事实,质量影响检索 |
| 答题(answer) | 中高 | ~3,639 in / ~45 out | **input 重**(塞了 ~3.6K 检索上下文);量大(1540 题),真正的成本+稳定性瓶颈 |

结论:**判官是便宜的那个(约占答题的 1/8),瓶颈在答题**。本地化的最大收益是答题+抽取的成本与稳定性;判官顺带解决,还免掉 reasoning 模型判 true/false 的隐藏 token。

## 硬件与模型选型(80G GPU:A100 80G / H100)

用 **vLLM** 起 OpenAI 兼容端点(`/v1/chat/completions`),即插即用,无需改 engram 代码。

- **判官**:32B 级 instruct 就够,70B 更稳。推荐 `Qwen2.5-72B-Instruct-AWQ`(4-bit ~40GB,留足 KV cache)或 `Llama-3.3-70B-Instruct-AWQ`;要快用 `Qwen2.5-32B-Instruct`。**判分建议用非推理模型或关闭思考**,省 token 又飞快。
- **答题 / 抽取**:同样 70B 级(`Qwen2.5-72B-Instruct-AWQ` / `Llama-3.3-70B-Instruct-AWQ`)。答题质量直接决定基线分,选强的。
- 三个角色可共用同一个 vLLM 实例(一个 80G 卡跑一个 4-bit 70B),串行/低并发即可。

### 起 vLLM

```bash
pip install vllm
# 一个 4-bit 70B,OpenAI 兼容端点
vllm serve Qwen/Qwen2.5-72B-Instruct-AWQ \
  --port 8000 --api-key local-eval \
  --max-model-len 16384 --gpu-memory-utilization 0.92
# 端点:http://<host>:8000/v1  (模型名即 Qwen/Qwen2.5-72B-Instruct-AWQ)
```

### 接入 locomo-bench(只换 env,零代码改动)

```bash
export LOCOMO_PROVIDER=openai
export LOCOMO_BASE_URL=http://<host>:8000/v1
export LOCOMO_MODEL=Qwen/Qwen2.5-72B-Instruct-AWQ
export LOCOMO_API_KEY=local-eval
export EXTRACT_MODEL=Qwen/Qwen2.5-72B-Instruct-AWQ   # 或换更快的抽取模型
# embedding 走本地 fastembed sidecar(见下)
export EMBED_BASE_URL=http://127.0.0.1:7999/v1
export EMBED_MODEL=bge-small-en-v1.5
```

## Embedding 侧(已在用):fastembed sidecar

零依赖(Python stdlib http.server)包 `fastembed` 的 `BAAI/bge-small-en-v1.5`(384 维)为 OpenAI 兼容 `/v1/embeddings`。要点:
- 必须 `setsid` + 绝对路径启动(WSL2 gotcha:login shell 会 `cd $HOME`,相对路径静默失败),`python3 -u`,首启下载模型后打印 `READY`。
- 模型 ID 一旦选定不能变(存储向量 vs 查询向量必须同模型,否则检索失真)。
- 详见 memory `offline-coverage-bakeoff-setup`。

## 换模型后必做:重验 + 声明新基线

1. **anti-放水 golden 门(判官可移植性硬检验)**:换判官后跑
   `LOCOMO_JUDGE_GOLDEN=1 ... go test -run 'TestJudgeMem0AlignedMatchesGolden/(anti-lure|emotion-opposite|list-zero-hit|boundary-14d)' ./cmd/locomo-bench/`
   —— 确认新判官也把所有陷阱判 WRONG(SC-001)。这正是那个门存在的意义。
2. **口径隔离(宪法 IV)**:换判官/答题模型 = 换基线。新分**单独声明、单独提交**,**不与旧 65.4%(luna)或竞品 88.83(GPT-4o 级判官)直接叠加**。judge 口径的 **delta(同一模型 strict vs mem0-aligned)对模型稳健**,始终有效;绝对分随模型变。
3. 要和竞品绝对分对齐,需用他们那档判官(GPT-4o 级);要纯离线可移植,本地 70B 判官更符合 engram 定位——两者按目的取舍,别混。

## 成本/稳定性对比(为什么值得)

- 中转站:免费/低价但**极不稳**(整段 502、并发限流、key 级风控),reasoning 模型判分慢(5–40s/次)且有隐藏 token。
- 本地 80G:租用约 $1–2/hr,**稳定、可控、可复现、离线**;判分近免费;答题/抽取成本转为固定 GPU 时长。

相关:memory `offline-coverage-bakeoff-setup`、`locomo-relay-key`、`judge-mem0-alignment-finding`;宪法 [.specify/memory/constitution.md](../.specify/memory/constitution.md)(I local-first、IV eval 门、V 诚实降级)。
