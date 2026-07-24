# LoCoMo 端到端答题评测 — 可复现 runbook（含踩坑史）

> **为什么有这份文档**：一次 e2e 答题 run 涉及 answer-LLM / embedder / judge 三个可替换后端 + 一串
> 非可选 flag，任一错配就得到**看似合理实则错 15–25pp 的伪影**。2026-07-23 我为验证「bge-large 是否端到端
> 涨点」绕了一大圈（59% → 70% → 85%），全程是配置伪影而非算法差异。这份文档把**可复现 recipe + 每个坑的
> 根因/修复**钉死，避免下次重蹈。逐特性 verdict 在 [`locomo-score-levers.md`](./locomo-score-levers.md)；
> 远端机手册在 [`remote-eval-box.md`](./remote-eval-box.md)；本文件是**「怎么把一次 e2e run 跑对」的正本**。

## 0. 一句话结论（canonical recipe）

固化 store 已建好时，复现 84% 级基线的**完整命令**：

```bash
source ~/.config/engram/locomo-vllm.env      # answer/extract → box vllm 8000
source ~/.config/engram/judge.env            # judge → deepseek（见 §2，务必单独文件）
export EMBED_BASE_URL=http://127.0.0.1:8001/v1 EMBED_MODEL=bge-large-en-v1.5 EMBED_API_KEY=local-eval
locomo-bench \
  --data testdata/locomo/locomo10.json \
  --store-dir <固化store> \
  --chunks --top-k 30 --chunk-quota 12 \
  --retrieval hybrid --force-answer --judge-mem0-aligned \
  --concurrency 40 --run-dir <run-dir>
```

**四个非可选 flag，缺一个就错**：`--chunks` · `--chunk-quota 12` · `--force-answer` · `--judge-mem0-aligned`。
跑完第一件事：`cat <run-dir>/regime.json`，必须同时含
`force_answer=true`、`judge=mem0-aligned`、`judge_model=deepseek-v4-flash`。缺任一字段 = recipe 错，数字作废。

## 1. 三后端栈

| 角色 | 后端 | 端点 / 配置 | 备注 |
|---|---|---|---|
| **answer + extract** | box vllm `Qwen/Qwen3.6-35B-A3B-FP8` | `:8000`，`--max-model-len 32768 --default-chat-template-kwargs '{"enable_thinking":false}'` | 计费机，空闲必停；隧道 `-L 8000`。solo 用 `--gpu-memory-utilization 0.92`，与 embed 同卡共存降到 `0.82` |
| **embedder（bge-large 1024d）** | box vllm `BAAI/bge-large-en-v1.5` | `:8001`，`--runner pooling --gpu-memory-utilization 0.08`，**`HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1`** | 隧道 `-L 8001`；离线 env 见 §踩坑#4 |
| **embedder（bge-small 384d，仅复现旧基线）** | **本地** `fastembed` sidecar | `:7999`，`scratchpad/embed_sidecar.py`（OpenAI 兼容 `/v1/embeddings`） | `.locomo-run/007-us2/cov-store` 是用 fastembed bge-small 建的，查询嵌入必须同源才对齐 |
| **judge** | `deepseek-v4-flash` @ `api.deepseek.com/anthropic` | `JUDGE_PROVIDER=anthropic`，key 走 `~/.config/engram/judge.env` | 微付费；`--judge-mem0-aligned` 决定宽/严口径 |

隧道（answer + bge-large 同一条）：

```bash
ssh -N -L 8000:127.0.0.1:8000 -L 8001:127.0.0.1:8001 -p <port> root@<host>
# 就绪校验：curl -s -H 'Authorization: Bearer local-eval' http://127.0.0.1:8000/v1/models
#           curl -s -H 'Authorization: Bearer local-eval' http://127.0.0.1:8001/v1/models
```

## 2. env 文件（凭据只走 env，永不进仓库）

`~/.config/engram/locomo-vllm.env`（answer/extract）、`~/.config/engram/judge.env`（judge）——两个都 `chmod 600`、
未跟踪。judge.env 内容形如：

```bash
export JUDGE_PROVIDER=anthropic
export JUDGE_BASE_URL=https://api.deepseek.com/anthropic
export JUDGE_MODEL=deepseek-v4-flash
export JUDGE_API_KEY=<deepseek key，只此一处，勿写死进任何 tracked 文件/脚本/日志>
```

> **为什么单独一个 judge.env 而不是塞进 `~/.bashrc`**：见 §踩坑#1。judge 覆盖键遵循
> `JUDGE_* → 回退 LOCOMO_*`（`cmd/locomo-bench/judge_config.go`）——JUDGE_ 没设上就**静默回退成用 answer 模型自判**。

## 3. 复现旧基线（管线自检，必做）

换环境后先做这一步确认「管线本身」没漂，再谈任何新杠杆：

```bash
# 本地起 bge-small sidecar（首次自动下 ~130MB）
setsid python3 scratchpad/embed_sidecar.py >sidecar.log 2>&1 & disown   # 打印 READY 后就绪
# 用 cov-store（bge-small 固化店）跑 canonical recipe，但 EMBED 指向 7999
EMBED_BASE_URL=http://127.0.0.1:7999/v1 EMBED_MODEL=bge-small-en-v1.5 ... \
  --store-dir .locomo-run/007-us2/cov-store --chunks --top-k 30 --chunk-quota 12 \
  --retrieval hybrid --force-answer --judge-mem0-aligned
```

**期望**：overall ≈ **84.0%**（记录基线 84.22%，±0.2pp 属 temp=1.0 采样噪声）。若不是 84% 级，别往下做——
先按 §踩坑逐条排除，管线没自检过的任何「新杠杆结论」都不可信。

## 4. 踩坑史（复现前必读，每条都真实踩过）

| # | 症状 | 根因 | 修复 |
|---|---|---|---|
| 1 | judge 变成 Qwen 自判：`cost=0`、log `judge_base_url_host=127.0.0.1:8000`、regime 缺 `judge=mem0-aligned` | `source ~/.bashrc` 在非交互脚本里命中开头 `case $- in *i*) ;; *) return`，在到达追加的 `export JUDGE_*` 前就 return | judge 环境放**独立文件** `~/.config/engram/judge.env`，脚本 `source` 它，别靠 ~/.bashrc |
| 2 | **overall ~59–70%（应 ~84%）** | 漏 `--judge-mem0-aligned`，默认严判把「同义但措辞不同」的正确答案判错 | 永远带 `--judge-mem0-aligned`；跑完查 `regime.json` 含 `judge=mem0-aligned` |
| 3 | context 均值 ~1500 tok（应 ~3550），分数塌（single-hop 尤甚） | `--chunk-quota 0`（默认）→ verbatim chunks 在等权 RRF 融合里输给 facts，chunk 召回塌到 ~2% | 带 `--chunk-quota 12`（30 槽里给 chunks 留 12），且必须同时 `--chunks` |
| 4 | 离线 box 上 embed vllm 启动挂住，日志停在 `Retrying ... huggingface.co ... Network is unreachable` | vllm 启动做在线 HF HEAD 校验 config.json，box 无外网，重试 5 次后卡住 | 启动前 `export HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1`（权重已在本地缓存） |
| 5 | answer vllm 突发 `transport error <- ... 8000: EOF`（成串失败题被判错） | `--concurrency 96` 压垮同卡共存的 answer vllm，瞬时丢连 | 并发 **40–64** 稳定（实测 0 失败）；共存时别顶到 96 |
| 6 | `cost: actual_usd=0` 但明明用了 deepseek judge | deepseek 不在 `LOCOMO_PRICE_TABLE`（见 `cost.json` 的 `unpriced_models`）→ 记 0，**不代表没调用** | 别拿 cost=0 当「judge 没跑」的证据；看 regime.json + log 里 `judge_base_url_host` |
| 7 | 用 `--store-dir` 复用店，但 context 仍低 / 分数不对 | 复用了**错的店**：facts-only（无 verbatim chunk）或维度不匹配的店 | 复用前核店：`sqlite3 conv0.db` 看 `memory_entries` 是否含长 chunk、`memory_embeddings` 维度是否与当前 EMBED 一致 |
| 8 | 用 heredoc 在一条命令里同时「建脚本 + 启动」，命令中途被 tool 超时 SIGTERM（exit 144），脚本没写全，后续 setsid 静默失败（无 log） | 长命令被打断留下半截/缺失的脚本文件；`setsid ... >/dev/null` 把错误也吞了 | **run 脚本用 Write 工具单独落盘**，`bash -n` 校验存在后再 `setsid` 启动；WSL2 长任务一律 detach + 文件轮询（[CLAUDE.md 长任务规则](../CLAUDE.md)） |
| 9 | SC-004 逐字节复现失败 | vllm-GPU 查询嵌入非确定（`embed_probe` unstable，bit_identical≈0.875）→ rrf_score 尾数抖 | 要 bit-identical 换 **fastembed CPU** 嵌入；GPU 嵌入不影响分数结论（象限分布两跑一致） |
| 10 | **冷启动首臂系统性偏低 ~2pp**（多臂配对时首个 arm 被压低，险酿假 GO） | vllm 刚 launch 后立刻跑的第一个 arm，KV cache 冷/共卡 embed 竞争/服务未热 → 同配置比之后复跑低 ~2.25pp（实测 base 82.92% vs 同配置 base2 85.17%，2026-07-24 assoc 014 评测）。若拿冷首臂当基线,任何 treatment 都会凭空 +2pp 显著 | **box 冷启后第一个 arm 作 warm-up 丢弃,或必复跑一次基线 arm 做锚**；多臂配对信「同会话复跑的干净基线」,不信首臂;paired McNemar 也要对干净基线,不对冷首臂 |

## 5. 「基线到底用的什么」怎么反查

旧 run 目录里没记全 flag 时：
- **embedder**：`cat <run-dir>/cost.json` 的 `unpriced_models`（会列出实际用过的 answer/embed/judge 模型名）。据此发现 84.22% 基线用的是 **bge-small-en-v1.5**，不是 bge-large。
- **是否重建店**：`cost.json` 的 `by_role.extract.calls`——`0` = 复用了固化店（未重抽）。
- **是否开 chunks / chunk-quota**：run.log 里 `verbatim chunks ingested ... chunks=N`（line 938）证明 `--chunks` 开了；context 均值（`answer_context_tokens_mean` ~3550 vs facts-only ~1550）反推 chunk 是否真进了 top-K。
- **判题口径**：`regime.json` 是否含 `judge=mem0-aligned`。

## 6. 本轮结果（2026-07-23，canonical recipe，deepseek-v4-flash mem0-aligned judge，box Qwen 答题）

| store / embedder | overall | 判定 |
|---|---|---|
| bge-small @ `007-us2/cov-store`（**控制/自检**） | **84.03%**（1294/1540） | 复现记录基线 84.22%（±0.2pp）→ **管线验证通过** |
| **bge-large @ 全新 q12 店** | **85.45%**（1316/1540） | **+1.42pp / 净 +22 题**；single +1.7 / multi +2.1 / temporal −0.6 / open-domain +4.2 |

verdict + caveat 详见 [`locomo-score-levers.md` Feature 009 §bge-large 端到端](./locomo-score-levers.md)。

## 7. 复现清单（TL;DR）

1. box 起 answer(8000) + bge-large(8001, 离线 env)；本地起 bge-small sidecar(7999) 仅当要复现旧基线。
2. 建隧道，`/v1/models` 双端点校验就绪。
3. `source locomo-vllm.env` + `source judge.env` + `export EMBED_*`。
4. **控制自检**：cov-store + bge-small + canonical recipe → 必须 ≈84%。
5. 跑目标 run（同 recipe，换 store/embedder）；**并发 40–64**；WSL2 用 setsid detach + 文件轮询。
6. 跑完 `cat regime.json` 核四要素；`cost.json` 不可信 usd 但可读 `unpriced_models`。
7. box 计费——**空闲即停**。
