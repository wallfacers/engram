# Feature 008 Evaluation Log - LoCoMo Score Levers

本文件只记录实跑结果。按宪法 IV，评测结果与 mechanism 改动分开提交；负结果同样
保留，不能包装成涨点。

## US2: open-domain 五步推理 prompt 单变量 A/B

- **Date**: 2026-07-22 14:17-14:20 +08:00，旧版与新版连续单次运行。
- **Code**: 旧版 `0f06b78`；新版 mechanism `61b5311`。
- **Dataset**: `locomo10.json`，仅 category 3，共 96 题；两侧 question_id 96/96 对齐。
- **Store / retrieval**: `.locomo-run/007-us2/cov-store`，`hybrid`，`--chunks`，
  `--top-k 100`，`--chunk-quota 50`。
- **Answer**: 本地 `Qwen/Qwen3.6-35B-A3B-FP8`。runner 的 Temperature 为 Go
  零值，但 OpenAI wire 使用 `omitempty`，实际请求没有发送该字段；远端模型
  `generation_config.json` 实测为 `temperature=1.0, top_p=0.95, top_k=20,
  do_sample=true`。因此 feature 注记中的“默认 temp=0、确定性”假设在本次后端上
  **不成立**，两臂实际共享同一采样配置。**embedding**: 本地
  `bge-small-en-v1.5`；**judge**: `deepseek-v4-flash`，`--judge-mem0-aligned`。
- **Artifacts**: `.locomo-run/008-opendomain-{old,new}/results-hybrid.jsonl`；
  配对报告为 `.locomo-run/008-opendomain-old/compare.json`。
- **Single-variable audit**: 两侧命令参数、store、answer/embedding/judge 模型和
  regime 逐字一致，除必要的 `--run-dir` old/new 后缀外，唯一运行时机制变量是
  `openDomainAnswerPrompt` 文本。`forceOpenDomainAnswerPrompt` 与
  `answerPromptForRegime` 内容哈希均与基线一致；引擎目录 diff 为空。

### Result

| Arm | Correct | Accuracy | Delta |
|---|---:|---:|---:|
| old prompt | 61/96 | 63.5% | - |
| five-step prompt | 59/96 | 61.5% | **-2.1pp** |

配对 McNemar（任务定义 `b=旧对新错`、`c=旧错新对`）：**b=7，c=5，
p=0.774414**（双侧精确检验）。方向为净减少 2 题，且完全不显著。旧版相对 007
参考点 `60/96=62.5%` 多 1 题；结合上述服务端 generation config，应归因于采样
噪声，不能声称是 temperature=0 下的确定性微扰。

### Regression check against 007

`openDomainAnswerPrompt` 只由 category 3 非 force 分支选择，因此其余三类没有执行
路径变化，也无需重跑。007 原产物的冻结值为：multi-hop **248/282=87.9%**，
temporal **265/321=82.6%**，single-hop **733/841=87.2%**；三类合计
**1246/1444**，保持不变。

以这 1246 题与新版 cat-3 实跑结果外推全量：**1305/1540=84.74%**，相对 007
参考点 **1306/1540=84.81%** 为 **-0.065pp**，未超过噪声；相对同窗旧版外推
`1307/1540=84.87%` 为 `-0.130pp`，同样不是实质回归。

### Verdict

**SC-003 FAIL / NO-GO**：五步 open-domain prompt 没有实质上升，反而在本次同窗
采样配对中下降 2.1pp（McNemar p=0.774414）；这组 b/c/p 只描述本次实现结果，
不能冒充确定性复现。保守过闸纪律下，未证明实质上升即不得报告或采纳为涨点杠杆，
不翻其它默认。若未来重评，必须先显式固定服务端 temperature=0，或预先冻结双方
相同 repeats 的统计设计。

## US3: bge-large 本地 embedder coverage A/B

- **Date**: 2026-07-22 14:18-14:42 +08:00，正式 large / small 各单次运行。
- **Dataset / denominator**: `locomo10.json` 全量 10 段；1540 题中有可解析 gold
  turn evidence 的 **1532** 题（multi-hop 281、temporal 321、open-domain 89、
  single-hop 841）。这是 coverage 分母，不是答题正确率分母。
- **Frozen retrieval config**: 两臂均为 `--coverage-only --chunks --top-k 30
  --chunk-quota 12 --retrieval hybrid`，未设置 reranker。
- **Small**: 原样复用 `.locomo-run/007-us2/cov-store`，本地
  `bge-small-en-v1.5`，384d；没有另建 small store。
- **Large**: fastembed 0.8.0 本地加载 `BAAI/bge-large-en-v1.5`，sidecar 仅列
  `bge-large-en-v1.5`，`/v1/embeddings` 批量自检为 1024d 且 index 顺序正确；
  用本地 `Qwen/Qwen3.6-35B-A3B-FP8` 重新抽取并整店写入
  `.locomo-run/008-embed-large-store`。10 店所有向量的 model/dims 均为
  `bge-large-en-v1.5/1024`，没有 384d 混入。
- **Zero answer/judge**: large 的 answer/judge model、small 的
  extract/answer/judge model 均设为故意不可用的 coverage-only sentinel；两次仍
  exit 0，日志明确打印 `retrieval-only ... no answer/judge`，产物中无 answer
  journal、judge 结果或 cost ledger。
- **Artifacts**: `.locomo-run/008-embed-{small,large}/coverage.json`；结构化差值为
  `.locomo-run/008-embed-large/comparison.json`；扫描测量为两目录的
  `cosine-scan.txt`。

### Coverage result (exact-turn recall, turn@30)

| Scope | Small 384d | Large 1024d | Large - small |
|---|---:|---:|---:|
| **OVERALL** | 77.012% | 80.804% | **+3.793pp** |
| multi-hop | 60.626% | 65.404% | **+4.778pp** |
| temporal | 79.465% | 82.529% | **+3.063pp** |
| open-domain | 52.672% | 58.112% | **+5.441pp** |
| single-hop | 84.126% | 87.693% | **+3.567pp** |

Overall session recall 为 95.420% -> 95.969%（**+0.549pp**）。五个 turn@k
scope 全为正，且 SC-002 指定的 open-domain / multi-hop 分别为 +5.441pp / +4.778pp。

### Honest-scale: pure-Go cosine scan

直接只读加载真实 10 店向量，逐店 warmup 后各重复 500 次仓库现有
`embedding.TopKCosine`。`chunk-quota` 路径的 `widePool=300` 经 Retriever 的
10x candidate multiplier 后实际 limit=3000；当前每店 238-491 条，所以是全量
cosine + sort。测量排除 SQLite load 和 HTTP query embedding：

| Store | Dims | Actual candidates (total / mean) | Mean | Median | p95 |
|---|---:|---:|---:|---:|---:|
| small | 384 | 3744 / 374.4 | 168.279 us | 168.881 us | 233.566 us |
| large | 1024 | 3888 / 388.8 | **399.027 us** | 406.356 us | **544.958 us** |

large 实店的纯 Go scan+sort mean 是 small 的 **2.37x**。large 正式运行 wall
`1271.80s` 包含 272 次 Qwen 抽取、整店 large embedding 回填和 coverage；small
`28.43s` 复用既有店，二者不是可比的端到端性能倍数，不能拿来声称 44.7x slowdown。

### Integrity notes and verdict

- 第一次 large 建店遇到一次 Qwen transport EOF；pipeline 会吞掉该 session 的
  抽取失败，所以该 attempt 被立即停止并完整排除（诊断产物保留为
  `.locomo-run/008-embed-large{-store}-failed-eof`）。正式重跑日志零 WARN/ERROR，
  且每店 `memory_entries == memory_embeddings`。
- 冻结命令要求 large 重新抽取：small 有 2688 extracted facts + 1056 chunks，large
  有 2832 extracted facts + 同一批 1056 chunks。总 entry 相差 144（+3.85%），说明
  同一 Qwen 重跑仍非逐条 bit-identical；因此这是**冻结重建契约下的 SC-002 A/B**，
  不能把全部差值强归因为 embedding 的纯因果效应。
- `git diff --name-only -- memory embedding provider store internal` 为空；sidecar 和
  测量器只在 scratchpad，Go 引擎零改。产物凭据扫描为空。

**SC-002 PASS / 值得**：large - small overall 为 **+3.793pp**，且 open-domain /
multi-hop 均有明显正增益，符合既定门。bge-large 保持默认关，但值得进入与 US1 的
联合 coverage 验证；本轮只证明两条杠杆可叠加测试，**未声称联合收益，也未声明或
替换任何 LoCoMo baseline**。

## US1: 本地 reranker coverage 免费闸（旗舰）

- **Date**: 2026-07-22 15:01-15:32 +08:00，`hybrid` vs `hybrid+rerank` 单次冻结运行。
- **Frozen retrieval config**: `--store-dir .locomo-run/007-us2/cov-store
  --coverage-only --chunks --top-k 30 --chunk-quota 12
  --retrieval 'hybrid,hybrid+rerank'`；两臂唯一变量 = 是否启用 reranker。
- **Dataset / denominator**: `locomo10.json` 全量，可解析 gold turn evidence 的
  **1532** 题（multi-hop 281、temporal 321、open-domain 89、single-hop 841）；两臂
  同分母。复用 007 固化 `cov-store`（bge-small 384d），未重建、无重抽取方差。
- **本地 reranker sidecar（死规则核心）**: 双端点纯本地服务，`/v1/embeddings`
  `bge-small-en-v1.5` 384d（与 store 同源）+ `/rerank`（裸）与 `/v1/rerank`
  cross-encoder `bge-reranker-v2-m3`，响应严格 `{"results":[{"index,relevance_score}]}`。
  源码零 `http/requests/urllib` 外网调用，经 `required_local_directory("RERANK_MODEL_PATH")`
  + `fastembed` **本地文件加载**（注释：missing file 必须失败而非走网络）。SSH 隧道
  访问，`EMBED_BASE_URL=http://127.0.0.1:<tunnel>/v1`，**无任何云 rerank**。
- **启动前三步自检**（`selfcheck.txt`）: `SELF_CHECK_OK models=2 rerank_results=2
  embedding_dim=384 bare_path=ok`；`/v1/models` 仅列 `bge-small-en-v1.5` +
  `bge-reranker-v2-m3`，内建 forbidden-list（`dashscope/cohere/gte-rerank/api.jina/openai.com`）
  拦截，无云型号。
- **Batch 延迟**（不含文本，`latency-smoke.txt`）: batch=50、top_n=12 五次
  median **122.158ms**（min 118.760 / max 153.487），本地 reranker 池化开销可接受。
- **Zero answer/judge**: `model/extract_model/judge_model=coverage-only-disabled`，
  `judge_base_url_host=127.0.0.1:9`（sentinel）；exit 0，产物无 answer journal /
  judge 结果 / cost ledger。**零花费**。

### Coverage result (exact-turn recall, turn@30)

| Scope | hybrid | hybrid+rerank | Δ (rerank − hybrid) | n |
|---|---:|---:|---:|---:|
| **OVERALL** | 77.012% | 92.468% | **+15.457pp** | 1532 |
| multi-hop | 60.626% | 81.369% | **+20.743pp** | 281 |
| temporal | 79.465% | 94.029% | **+14.564pp** | 321 |
| open-domain | 52.672% | 67.568% | **+14.896pp** | 89 |
| single-hop | 84.126% | 98.216% | **+14.090pp** | 841 |

Overall session recall 95.420% → 97.799%（**+2.378pp**）。

- `git diff --name-only -- memory embedding provider store internal` 为空；sidecar /
  自检 / 测量器只在 scratchpad，Go 引擎零改。产物凭据扫描为空。
- 因过闸幅度（+15.457pp）远超 +4pp 门约 3.9×，**不触发**第二本地模型（jina/mxbai）重试。

**SC-001 PASS（大幅）**：本地 reranker overall turn@k **+15.457pp**，每类 +14~20.7pp，
远超 +4pp 闸。这把 007 复盘中被死规则判负的**付费** gte-rerank 赢面（曾 +8.3pp）
转化为**纯本地、可移植、零云依赖**的合法赢，且幅度更大。

**必带限定**：此为 **coverage/turn@k 召回**增益，**非端到端答题正确率**——是充分性未证的必要条件；
真实答题增益需 US4 授权后端到端跑（默认预算 top-k 30，`hybrid` vs `hybrid+rerank`）才能声明。
reranker 保持**默认关 / opt-in**（宪法 V）；本轮**未声明或替换任何 LoCoMo baseline**。
