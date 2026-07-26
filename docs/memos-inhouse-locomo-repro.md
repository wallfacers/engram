# MemOS 同栈 LoCoMo 复现(追评任务记录)

> 🧭 **状态**: **已出分(2026-07-26)** —— **MemOS @ engram 同栈 = 82.40%**(1269/1540),
> **低于** engram 同口径 85.71% **3.31pp**。原假设(MemOS 领先、待剥离伪影)**方向被推翻**:
> leaderboard 88.83 → 同栈 82.40 的 **−6.43pp 全部是 regime 伪影**。结果与口径诚实项见 **§6**。
> · **目标**: 用 MemOS **自家代码**在 **engram 同款模型栈**(答题模型跑在 AutoDL 租用 GPU box,
> 非本地)下跑一遍 LoCoMo,得到 apples-to-apples 的 MemOS 分。
> 竞品分正本 + 口径对齐见 [`competitive-benchmarks.md`](./competitive-benchmarks.md);
> 本文只记这一条复现线的动机、做法、约束、预期产出。

记录日期:2026-07-25。

---

## 1. 为什么要自己复现(动机)

`competitive-benchmarks.md §5④` 已用源码证实:engram 与 MemOS/OmniMemEval 的
**分母 / 类别(cat-5 排除)/ 拒答 / 聚合四轴相同**,可比;但仍有 **两个 regime 变量未固定**:

- **答题模型强度** —— MemOS leaderboard 的 `ANSWER_MODEL` 未 pin,极可能是强模型;
  engram 用本地/relay 模型。88.83 里有多少来自"答题模型更强"而非"记忆机制更好",未知。
- **judge 宽松度** —— OmniMemEval judge 默认 gpt-4o-mini + "沾同一主题即 CORRECT",
  比 engram judge 宽松(§6 逐条)。

单看 leaderboard 的 88.83 无法剥离这两块。**唯一干净的剥离办法**:让 MemOS 跑在
**和 engram 完全相同的答题模型 + 相同 judge** 上。此时若 MemOS 仍显著高于 engram,
差距才是**真机制差距**(抽取/检索/记忆组织);若差距大幅收窄,则原 23pp 里很大一块是
regime 伪影(答题模型 + judge 强度),不是 engram 的记忆能力短板。

这条线直接回答 §5④ 遗留问题,是"按瓶颈分兵"的前置诊断。

## 2. 做法

- **拉代码**:`MemTensor/MemOS`(https://github.com/MemTensor/MemOS)
  + 其 LoCoMo 评测框架 `MemTensor/OmniMemEval`(§4 已确认是同一套驱动代码)。
- **答题(answer)**:**AutoDL 租用 GPU box** 上 vllm 部署的 **qwen** 模型(OpenAI-compatible,**非本地**)。
  与 engram e2e 评测同一答题栈,即 [`remote-eval-box.md`](./remote-eval-box.md) 那台
  (SSH host/port/password 每次重启轮换,凭据只走 env/tunnel、绝不落库)——固定答题模型强度这一变量。
- **判题(judge)**:**deepseek-v4-flash**,与 engram 侧对齐(同一 judge → judge 宽松度可比)。
- **数据集 / 分母**:LoCoMo,**cat 1-4 = 1540 题,cat-5 排除**(与 engram 同口径)。
- **产出**:MemOS 在"engram 同款答题 + 同款 judge"下的复现分,分类别(single/multi/temporal/open)拆。

## 3. 约束(硬)

- **只能同 harness 才可比**:MemOS 复现分不得与其 leaderboard 88.83 直接混用宣称——
  前者是"engram 同栈下 MemOS 分",后者是"MemOS 自报栈分"。只有前者能与 engram 现状对齐比较。
- **诊断/对标线,不碰引擎**:本任务在 engram 仓外(拉 MemOS 到 scratchpad / 独立目录)跑,
  产物落 session scratchpad,不进 engram 引擎、不改 `memory/ embedding/ provider/ store/`。
- **死规则复核(禁付费云 rerank)**:若 MemOS 默认栈里带**云 reranker / 云 recall 模型**,
  必须显式标注——那部分不是"纯本地栈的赢",复现时应记录是否启用、启用会否污染"机制差距"结论。
  (engram 侧对标口径始终是纯客户端/离线可跑。)
- **省钱**:vllm 答题跑在 **AutoDL 租用 GPU box(计费,metered)** —— **空闲必停**(`remote-eval-box.md` 非议)。
  deepseek-v4-flash judge 是付费 token,按 LoCoMo 1540 题的判题量预估成本、过成本闸。
- **文献口径**:MemOS/OmniMemEval 方法学核对若需查论文,走 alphaXiv MCP,不用 WebSearch。

## 4. 预期产出与下一步(**已兑现,实测见 §6**)

| 产出 | 用途 | 实测(2026-07-26) |
|---|---|---|
| MemOS@同栈 LoCoMo 总分 + 分类别 | 与 engram 现状对比 | **82.40%**,engram 同口径 85.71% ⇒ **engram +3.31pp** |
| 复现分 vs leaderboard 88.83 的落差 | 量化 regime 伪影占多少 | **−6.43pp 全部是伪影**(答题模型 + judge 宽松度) |
| 剥离 regime 后的真机制差距 | 定位 engram 主战场 | 差距**是负的**;MemOS 唯一赢面 multi-hop +1.77pp |

结论已回填 `competitive-benchmarks.md §5②/§5④`。本文保留为该复现线的方法学与踩坑正本
(§5 环境适配 4 处 + 两个硬发现,任何人重跑 MemOS 都会撞上)。

---

## 5. 实施记录(2026-07-25,AutoDL box)

### 5.1 实际入口与数据集口径(已核实)

- **入口不是 OmniMemEval,而是 MemOS 自带 `evaluation/scripts/locomo/`**(6 步管线
  ingestion→search→responses→eval→metric,驱动脚本 `evaluation/scripts/run_locomo_eval.sh`,
  `LIB=memos-api`)。走 OmniMemEval 需要额外起 client 层,无必要。
- **数据集逐条同口径**:MemOS 自带 `evaluation/data/locomo/locomo10.json` =
  10 convs / **1986 qa** / cat1:282 · cat2:321 · cat3:96 · cat4:841 · **cat5:446** →
  cat1-4 = **1540**,与 engram 分母**完全相同**(不是"相似",是同一份文件同一切分)。
- **模型注入点**(全部 env,无需改代码):答题 `CHAT_MODEL`+`CHAT_MODEL_BASE_URL`;
  judge `EVAL_MODEL`+`OPENAI_API_BASE`;MemOS 抽取器 `MEMRADER_MODEL`+`MEMRADER_API_BASE`;
  embedder `MOS_EMBEDDER_*`。server 进程与 eval 进程分别给 env,故"抽取走本地 qwen、
  judge 走 deepseek"不冲突。`load_dotenv()` 默认不覆盖已有 env ⇒ **凭据可纯 export 注入,不落盘**。

### 5.2 复现栈(engram 同款)

| 组件 | 配置 | 与 engram 的关系 |
|---|---|---|
| 答题模型 | vllm `Qwen/Qwen3.6-35B-A3B-FP8` @:8000 | **同一台 box、同一模型** = 固定 answerer 变量 |
| MemOS 抽取器 | 同上(`MEMRADER_MODEL`/`MOS_CHAT_MODEL`) | 抽取模型强度也对齐 |
| embedder | vllm `BAAI/bge-large-en-v1.5` @:8010,**1024d** | **与 engram 009 店同款 embedder** |
| reranker | MemOS 默认 `http_bge` → 008 本地 sidecar `bge-reranker-v2-m3` @:8020 | 纯本地,非云;格式逐字段兼容 |
| graph db | **neo4j-community 5.26.28** @:7687 | MemOS 默认后端 |
| vec db | qdrant 1.18.3 @:6333(`neo4j_vec_db`,1024d,实测 13625 点) | neo4j-community 的向量侧 |
| judge | **deepseek-v4-flash × num_runs=3** | **与 engram judge 同模型** |
| 检索预算 | MemOS 自家默认 `top_k=20`(未调) | engram 侧是 top-k 30/chunk 12,各自默认 |

### 5.3 环境层适配(4 处,**MemOS 代码零改动**)

1. **gpt2 tokenizer**:MemOS 的 chunker 硬编码 `tokenizer="gpt2"`,chonkie 用 rust `tokie` 下载,
   而 box 只能走 AutoDL MITM 代理、rust 内置根证书不信其 CA(且 tokie 不认 `HF_ENDPOINT`)。
   → 本地下官方 `tokenizer.json` 传上去,`sitecustomize.py` 把 `from_pretrained("gpt2")` 转成
   `tokie.Tokenizer.from_json(<本地路径>)`。**同一份官方文件,分词行为等价**。
2. **pgvector 类型适配器**(仅 postgres 尝试期需要):psycopg2 未注册 → 向量读回是字符串。
   注册官方适配器并 `commit()`(残留事务会让 MemOS 连接池的 `set_session()` 报错)。
3. **usage 插桩**:hook `openai` 的 `Completions.create`/`AsyncCompletions.create` 累加
   `usage` 落 jsonl(MemOS eval 脚本自身不记 token)。规矩见 locomo-score-levers 成本一节。
4. 自动补依赖:MemOS 未在 pyproject 声明但运行时必需的 `chonkie`/`langchain_text_splitters`/
   `jieba`/`pika`/`psycopg2-binary`/`qdrant-client`。

### 5.4 两个硬发现(影响"什么才算 MemOS 的分")

- **① `PostgresGraphDB` 不能用于复现。** 它缺 `get_children_with_embeddings`/`get_edges`/
  `get_memory_count` 等 **tree_text 层级检索核心方法**(36 个方法 vs `neo4j.py` 47 个,
  另含 `detect_conflicts` → `return []`、`merge_nodes` → `NotImplementedError`)。
  实测:数据正常落库(user_name/向量/status 全对)但 **search 恒返回 0 条**。
  → 必须用 MemOS 默认主力 `neo4j-community`;**任何基于 postgres 后端的 MemOS 分数都无效**。
- **② `SEARCH_MODE=fast`(eval client 默认)在 neo4j-community 上恒返回 0 条**,
  换 MemOS 的 **`fine`** 模式后正好返回 `top_k=20` 条。fast 依赖的 fast-graph 路径在
  community 版不可用。→ 复现用 `fine`。**这个偏差对 MemOS 是保守/有利的**
  (fine 检索更完整),故若 MemOS 复现分偏低,**不能**归因于我们削弱了它。

### 5.5 顺带挖到的机制情报(与分数无关,已可用)

> ⚠️ **本节结论已被 §6 部分推翻(2026-07-26)**:下面"把真机制差距收窄到记忆组织形态"是在
> **假定 MemOS 领先**的前提下写的。同栈实测后 MemOS 总分反而低 3.31pp,**"engram 有个待补的
> 记忆组织短板"这个前提不成立**。仍然成立的是前半段的对照事实(同一 reranker、engram 端到端
> −0.06pp);它现在的正确读法是 **§6.2 那条**:MemOS 的 tree/graph + reranker 组合只在
> multi-hop 换来 +1.77pp,总分还输 —— **这条路的上限被外部证据关小了,不是打开了。**

**MemOS 默认栈自带本地 `bge-reranker-v2-m3`**(`MOS_RERANKER_BACKEND` 默认 `http_bge`)。
engram 在 **008 US1 试过同一个模型**:coverage +15.457pp,端到端 **−0.06pp(p=1.0)**,
且把 temporal 砸 −9 → NO-GO。**同一 reranker、一家赢一家不赢** ⇒ 增益不在 reranker 本身,
而在**它重排的对象是 tree/graph 组织的记忆**。这把"真机制差距"从笼统的
「抽取质量 vs 检索质量」收窄到 **记忆组织形态**上 —— 比原 §4 的三分法更可执行。

---

## 6. 结果(2026-07-26,全量 1540 出分)

### 6.1 主表

**MemOS @ engram-parity stack = 82.40%(1269/1540)**

| 类别 | n | **MemOS@同栈** | engram `009-full-A-base`(3-rep 多数) | Δ(engram − MemOS) |
|---|---:|---:|---:|---:|
| single-hop | 841 | 82.64% | 88.82% | **+6.18** |
| multi-hop | 282 | **89.36%** | 87.59% | **−1.77** |
| temporal | 321 | 82.55% | 81.93% | −0.62 |
| open-domain | 96 | 59.38% | 65.62% | **+6.24** |
| **OVERALL** | **1540** | **82.40%** | **85.71%** | **+3.31** |

**judge 成本(usage 全程插桩)**:deepseek-v4-flash × 3 reps = 4620 calls,
in 1,648,248 tok(cache 命中 1,187,456 = **72.0%**)、out 611,876 tok →
**$0.2392 ≈ ¥1.70**。答题侧跑在 AutoDL box 上的 vllm,不计 token 费。

### 6.2 结论:原假设方向被推翻

本任务的前提是"MemOS 88.83 高出 engram ~3~5pp,要剥离其中的 regime 伪影"。实测:

- **固定答题模型(同一台 box、同一 Qwen3.6-35B-A3B-FP8)+ 固定 embedder(bge-large)+
  同一 judge prompt 同一 judge 模型(deepseek-v4-flash)后,MemOS 反而低 3.31pp。**
- **88.83 → 82.40 = −6.43pp 全部记在 regime 头上**(答题模型强度 + judge 宽松度)。
  即 leaderboard 上那个"纯本地栈能上 88+"的数字,**换到 engram 的 answerer + judge 就不存在**。
- **真机制差距的方向**:MemOS 的 tree/graph 记忆组织**只在 multi-hop 赢 1.77pp**——
  正是它该赢的地方,与 §5.5 那条"reranker 增益来自被重排对象是 graph 记忆"的推断吻合;
  **single-hop / open-domain 各输 6pp+**,temporal 打平(−0.62,在噪声内)。

### 6.3 口径诚实项(**结论必须连同这几条一起引用**)

1. **聚合不对称**:engram 85.71% = **3 次答题** rep 多数投票(temp=1.0,同批 3-rep mean 85.4%、
   ci95 [84.9, 85.9]);MemOS = **1 次答题** × 3 次判题 rep 多数。MemOS 单点**无带宽**。
   82.40 落在 engram ci95 下界之外 2.5pp ⇒ **方向稳,量级 ±1pp 待 MemOS 补 answer reps**。
2. **答题上下文预算不等,且对 engram 有利**:MemOS 实测每题喂给 answerer 的记忆上下文
   mean **4236 字符 ≈ 1059 tok**(p50 3361 / p90 9127,2 个 speaker 块,记忆条数 mean 19.1 /
   p50 15 / max 40 = `top_k=20` 每 speaker 合并后);engram 侧同栈 prompt 实测 **3262 tok/次**。
   **engram 喂进去的上下文约是 MemOS 的 3 倍。**两边都是各自默认值,但这不是等预算比较——
   **+3.31pp 里有多少来自"上下文更多"而非"记忆更好",本轮未剥离**。这是本结论最大的残留混淆项。
3. **其余不对称项多数对 MemOS 有利**:MemOS 默认栈带本地 `bge-reranker-v2-m3`
   (engram A-base **无** reranker)、用了检索更完整的 `fine` 模式(§5.4②)。
   → 在这两轴上"engram 领先"是**保守**的。
4. **judge 确认真同款**:`judge_memos.py` 直接从 `cmd/locomo-bench/runner.go` 正则抽
   `judgeMem0AlignedSystemPrompt`,不是各用自家 grader prompt;模型与 `009-full-A-base`
   的 `regime.json`(`judge=mem0-aligned; judge_model=deepseek-v4-flash`)一致。
5. **未做配对检验**:engram 逐题正误在 HF(`009-eval-runs/009-full-A-base`),本地无副本,
   故本轮只有两个独立总分、**没有 1540 题的 McNemar**。要把 +3.31pp 从"两个点估计之差"
   升到"配对显著",需拉回 A-base 逐题 pred 做 join —— **零 token 成本,是最便宜的加固动作**。
6. **不得与 leaderboard 混用宣称**(§3 硬约束仍然有效):82.40 是"engram 同栈下的 MemOS",
   88.83 是"MemOS 自报栈下的 MemOS"。**只有前者能与 engram 比。**

### 6.4 产物

`.locomo-run/016-memos-parity/`(gitignored):
`memos_parity_score.json`(分类别 + 成本)· `memos_judged_detail.json`(1540 题逐题,
含 3 票 votes)· `memos_responses.json`(MemOS 原始答案 + `search_context`,7.1MB)·
`judge_memos.py` / `judge_memos.log` · `run_locomo.sh` / `chain4.sh`(6 步管线驱动)·
`sitecustomize.py`(§5.3 的 gpt2 tokenizer + usage 插桩)。**已核无凭据泄漏**
(box 的 SSH host/port/password 只走 env,未落任何文件)。⚠️ **尚未推 HF** —— 该目录 gitignored,
需归档到 HF 才跨机不失传。

### 6.5 下一步

| # | 动作 | 成本 | 价值 |
|---|---|---|---|
| 1 | 拉 HF `009-full-A-base` 逐题 pred,与 `memos_judged_detail.json` 做 1540 题 join + McNemar | **零 token** | 把 +3.31pp 从点估计升到配对显著 |
| 2 | MemOS 补 2 次 answer rep(共 3 rep) | box GPU 时间 + ¥3.4 judge | 给 82.40 一条误差带,消掉诚实项 ①  |
| 3 | 等上下文预算重跑(MemOS `top_k` 抬到喂满 ~3.2k tok,或 engram 降到 ~1k) | box GPU 时间 + judge | 剥离诚实项 ② 这个最大残留混淆 |
| 4 | 产物推 HF | 零 | 防失传 |
