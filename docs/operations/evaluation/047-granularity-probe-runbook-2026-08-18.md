---
title: 047 US2 粒度 probe — box runbook(维护者开机器后照此执行)
date: 2026-08-18
tags: [operations, eval, spec-047, chunk-granularity]
audience: [maintainers, agents]
---

# 047 US2 box runbook

前置:US1 GO verdict [047-granularity-sweep-2026-08-18](../../evaluation/reports/047-granularity-sweep-2026-08-18.md)。
目标:450 档 vs 900 锚单变量配对。**分段门(spec FR-004)**:先 2 臂 × 1-rep probe
(方向筛,~¥25-40),GO 再补 2-rep + clean 重判(~¥60-90)。判题尽量落空闲时段
(高峰 = 北京 9:00-12:00 / 14:00-18:00;晚上开机器正合适)。

## 0. 环境起来后(每次新实例都要)

```bash
# vllm answerer: Qwen3.8-27B, 必须 32768(16384 长 thinking 会 SSE 卡死)
# vllm embed: bge-large, 必须 --max-num-seqs 1(并发≥4 嵌入非确定→检索漂移)
# 凭证/端口以维护者 live 提供为准,只走 env,不进任何文件
source /root/autodl-tmp/032-run.env   # 若在;否则按 032 口径重建 env
```

binary:本地 WSL2 编译 `CGO_ENABLED=0 GOOS=linux go build -o /tmp/qwen38-eval/locomo-bench-047 ./cmd/locomo-bench`
(当前 master 已含 `--per-call-timeout`/`--wide-dump`,零新代码需求——US2 机制臂全部用现有 flag),scp 上 box。

## 1. 建 450 店(一次性,~10-30 min)

```bash
cp -r /root/autodl-tmp/032-store /root/autodl-tmp/047-store-450   # fact/嵌入全复用
cd /root/autodl-tmp && LOCOMO_API_KEY=dummy-coverage-only-makes-no-llm-call \
EMBED_BASE_URL=$EMBED_BASE_URL EMBED_MODEL=$EMBED_MODEL ./locomo-bench-047 \
  --dataset-format locomo --data /root/autodl-tmp/locomo.json \
  --store-dir /root/autodl-tmp/047-store-450 --run-dir /root/autodl-tmp/047-probe/cov450 \
  --coverage-only --retrieval hybrid --chunks \
  --chunk-target-chars 450 --chunk-max-chars 550 \
  --top-k 30 --chunk-quota 12
# 验证: log 里 chunk 数应约翻倍(~4-6k/conv 级), embedding backfill 完成
```

## 2. Probe(2-3 臂 × 1-rep,同批顺序执行)

```bash
# 机制臂 A(涨点优先): 450 店 k75 q45 ≈7107 tok
./locomo-bench-047 --dataset-format locomo --data /root/autodl-tmp/locomo.json \
  --store-dir /root/autodl-tmp/047-store-450 --run-dir /root/autodl-tmp/047-probe/grA-k75q45 \
  --chunks --retrieval hybrid+unified --top-k 75 --chunk-quota 45 \
  --per-call-timeout 15m --judge-mem0-aligned --no-idk-retry \
  --concurrency 32 --repeats 1 --trace-mediation=false

# 机制臂 B(省 token 优先,时间允许可选): 450 店 k60 q36 ≈5693 tok
#   同上, --top-k 60 --chunk-quota 36, run-dir grB-k60q36

# 对照臂(锚): 900 店 k30 q28(与 quota28 verdict 配方唯一差异=同批重跑)
./locomo-bench-047 --dataset-format locomo --data /root/autodl-tmp/locomo.json \
  --store-dir /root/autodl-tmp/032-store --run-dir /root/autodl-tmp/047-probe/ctl-k30q28 \
  --chunks --retrieval hybrid+unified --top-k 30 --chunk-quota 28 \
  --per-call-timeout 15m --judge-mem0-aligned --no-idk-retry \
  --concurrency 32 --repeats 1 --trace-mediation=false
```

注意:`--trace-mediation=false` 这个 flag 当前 master 已不存在(044 删除)——若 binary
是 master 新编的,**去掉该 flag**;只有老 042-bin 才需要。本地新编的 locomo-bench-047 不带。

## 3. Probe 判定(在线 correct 字段直接配对)

```bash
# 拉回 results-*.jsonl 后本地对比 per-question correct(同 qid 配对)
# 门: 机制臂 - 对照臂 ≥0(或 B 臂允许 ≥-0.3pp 换 token -18%)→ GO 补全
#     显著负 → NO-GO, 当天关机止损
```

## 4. GO 后补全

- GO 臂补 2-rep(`--repeats 3` 全新 run-dir 重跑 3-rep,或 run-dir 续跑 2 rep——
  regime.json 一致即可续);对照臂同样 3-rep 同批。
- clean 重判拉回本地凌晨跑(judge 空闲价省一半)。
- 结果入 result-matrix + verdict 文档。

## 5. 硬纪律

- run-dir 全部在 `/root/autodl-tmp/`,不碰系统盘;`df -h /` 开跑前查。
- 请求速率看 vllm 指标非 results 行数(车队效应手册);慢跑先查
  [autodl-slow-run-troubleshooting](autodl-slow-run-troubleshooting.md)。
- **跑完即关机**(空闲必停);小结果文件先备份 `/root/autodl-tmp/eval-backup-<ts>/`。
- 预算参考:机器 ¥6/h(probe 段 ~2.5-3h;judge 空闲 ~¥6-12);全部 flag 无 paid
  rerank(DEATH RULE 合规)。
