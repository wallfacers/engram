# Feature 007 Evaluation Log — Judge 口径对齐(mem0-aligned)

本文件记录维护者实跑的评测。**宪法 IV 归因纪律**:eval 结果(本文件)与 mechanism
代码分开提交;judge 口径是评分口径变更,新口径分**明标「口径对齐非涨点,不得与其它
杠杆收益叠加」**,且**不得**与旧口径基线静默并列。

---

## US2 Strike 1: mem0-aligned 全量参考点(新栈)

- **Date**: 2026-07-22 (10:50–11:34 +08:00, 单次连续运行)
- **Dataset / repeats**: locomo10.json 全量 10 段 × 1540 题(cat 1-4,cat-5 排除) × repeats=1
- **Retrieval**: `hybrid`,`hybrid+assoc`,`hybrid+temporal` 三臂;**top-k 100 / chunk-quota 50**(统一慷慨预算)
- **Answer + Extract model**: 本地 `Qwen/Qwen3.6-35B-A3B-FP8`(vllm,`enable_thinking:false`)
- **Embedding**: 本地 `bge-small-en-v1.5`(fastembed sidecar,384d)
- **Judge**: `deepseek-v4-flash`,**mem0-aligned 口径**(`--judge-mem0-aligned`);anti-放水 golden 门 26/26 已守
- **Store**: 复用持久化抽取(`--store-dir`,facts 175–363/段);抽取零重算
- **Cost**: ledger `actual_usd=0`(价表无 flash 计价);真实 ≈ 3×1540 判题 × ~550 tok ≈ **¥1–4**。答题/抽取/embedding 全本地零成本。
- **Fingerprint**: `judge=mem0-aligned;judge_model=deepseek-v4-flash;retrieval=hybrid,hybrid+assoc,hybrid+temporal`(无 rerank)

### 结果(mem0-aligned judge,1540 分母)

| 臂 | multi-hop | single-hop | temporal | open-domain | **OVERALL** |
|---|---|---|---|---|---|
| **hybrid**(默认检索) | 87.9 | 87.2 | 82.6 | 62.5 | **84.8%** (1306/1540) |
| hybrid+assoc | 89.4 | 86.1 | 81.3 | 57.3 | 83.9% (−0.9) |
| hybrid+temporal | 87.6 | 88.5 | 83.8 | 63.5 | **85.8%** (+1.0) |

### 裁决

1. **抽取不是瓶颈,判题口径是差距大头** —— 与竞品同 1540 分母下:MemOS 88.83(本地)、
   Mem0 92.5。本栈 mem0-aligned hybrid **84.8%**,差距收敛到 ~4–8pp,支持 007 假设
   「差距主要是口径不是架构」。**注意**:此为跨实现对比,非受控实验(抽取/答题模型不同),
   只作方向锚,不作精确名次。
2. **`+assoc` 判死** —— 端到端 net **−0.9pp**(open-domain −5.2pp);与零成本 coverage
   (turn@k 全类别全平)双证一致。**assoc 不是涨点杠杆,保持默认关。**
3. **`+temporal` 微正** —— net **+1.0pp**(单跳/时间/开放域齐升,多跳略降);真实但小。
   **是否翻默认需单独权衡**(小增益 vs 复杂度),本轮不翻。

### 诚实警告(与数字绑定,不可剥离)

- **⚠️ 本栈抽取 = Qwen3.6,非冻结 luna** → **84.8% 与旧严判 65.4% 不是干净对比**
  (栈不同:Qwen3.6 vs luna、top-k 100 vs 30、口径不同)。**不得**叙述为「算法涨了 ~19pp」。
- **⚠️ 判题伪影的干净隔离只在 dev-slice 同栈成立**:2 段切片同栈严判 65.4% → mem0 85.8%
  (**+20pp**,同 transcript 唯一变量=judge)。**全量层未跑同栈严判臂**,故全量「伪影」
  只是含栈变化的粗估,**不作严格隔离上报**。要严格隔离需补一条「同栈严判全量 hybrid 臂」
  (近免费,可选后续)。
- **⚠️ top-k 100 / chunk-quota 50 是慷慨统一预算,非交付默认**(003 默认 top-k 30)。
  84.8% 是「宽预算 + mem0 口径」下的参考点,非默认配置分。
- **⚠️ 口径对齐非涨点**:此数**不得与 assoc/temporal 或任何其它杠杆收益叠加**。

### Decision

- **声明本栈 mem0-aligned 参考点**:hybrid = **84.8%**(1540,Qwen3.6 抽取,top-k 100,
  deepseek-v4-flash mem0 判)。作为**新栈新参考点**记录,**不替换** 003 冻结-luna 基线
  (51.0% ± 10.3pp,不同 regime)。
- **默认 judge 口径保持严格**(FR-010):本次不翻默认;翻默认是单独决策,需先补同栈严判
  全量臂做干净隔离。
- **assoc 保持默认关**;temporal 微正记录在案,不本轮翻默认。
- 干净隔离的完整落地(同栈严判全量臂 + false→true flip 抽查)列为 US2 可选后续,近免费。
