# 042 交接：AutoDL unified×k150 LoCoMo 配对 run（2026-08-14 晚）

> 场景：本机即将关机，维护者换机接手。box 上有一个 18:01 启动的全量 run 正在跑，
> 预计 **~3 小时（约 21:00–21:30）完成**。本文给出：进度检查命令、健康/病理判据、
> 完成后的出分与关机步骤、以及本次 042 评审+推演的结论存档。

## 1. Box 访问

- SSH：`ssh -p 38389 root@connect.westd.seetacloud.com`（AutoDL，密码由维护者持有，
  **重启会轮换**；按仓库规则走 paramiko/env，不入库不入日志）
- 两个 vllm 均在跑：answer=Qwen3.6-35B-A3B-FP8 `:8000`（max-model-len 32768）、
  embed=bge-large `:8010`（**`--max-num-seqs 1` 确定性在位**）
- judge=api.deepseek.com deepseek-v4-flash（env 在 `/root/autodl-tmp/032-run.env`，0o600）
- GPU：RTX PRO 6000 Blackwell 96G；磁盘：/ 23%、数据盘 38%，宽裕

## 2. 正在跑的 run

- 启动：**2026-08-14 18:26:50**（box 时间，第三次也是最终一次启动），脚本
  `/root/autodl-tmp/038-runs/run-locomo-paired-150.sh`（setsid 已脱离；自带 rm -rf 全新目录）
- 内容：LoCoMo 1540 × 3 reps × 2 臂（`hybrid`=control 即 legacy 同批、`hybrid+unified`=treatment）
  top-k **150**、chunk-quota 12、max-tokens 16000、judge-mem0-aligned、context parity fail-closed
- run-dir：`/root/autodl-tmp/038-runs/locomo-paired-150/`
- ETA：每 repeat ≈ 60 分钟（见下方车队效应），3 repeats ≈ 3-3.5 小时 → **预计 21:30–22:00 出 run.exit**

### ⚠️ 先读：records 冻结是预期行为，不要杀 run（重要更正）

18:18 的 goroutine dump 已定案（`/root/autodl-tmp/042-scratch/goroutine-dump-18q.log`）：
10 个 conversation 并行启动 → ~3040 个答案 goroutine 在单一 32 槽 FIFO 信号量上排队，
judge 请求排在全部原始 answer 之后 → **每个 repeat 的前 ~50-60 分钟 records 会冻结在
~40 条左右（启动头 2 分钟挤过去的），vllm 请求速率却保持 ~60-65/min**。这不是病。
约 55-65 分钟处 judges 集中排空，records 一口气 +3000、10 个 `conversation done` 连发，
然后进入下一 repeat。判健康看**请求速率**，不要看记录数。

（前两次启动 16:35 / 18:01 均为健康 run，因误判"记录冻结=病理"被杀——详见
[autodl-slow-run-troubleshooting.md](autodl-slow-run-troubleshooting.md) 的误诊教训。）

### 进度检查（复制即用，paramiko 或 ssh 均可）

```bash
date +%H:%M:%S
grep -c 'conversation done' /root/autodl-tmp/038-runs/locomo-paired-150/run.log
for d in /root/autodl-tmp/038-runs/locomo-paired-150/run-*; do echo -n "$(basename $d)=$(cat $d/results-*.jsonl 2>/dev/null | wc -l) "; done
curl -s -m 5 http://127.0.0.1:8000/metrics | grep num_requests_running | grep -v '#'
cat /root/autodl-tmp/038-runs/locomo-paired-150/run.exit 2>/dev/null
```

### 时间线预期（以 18:26 启动计）

- 18:26–19:25：repeat-1 answer 相。records 冻结 ~40 条、convdone=0、请求 ~60/min —— **全部正常**
- ~19:25–19:35：repeat-1 judge 排空，records 跳到 ~3000，10 个 conversation done 连发
- ~19:35–20:35：repeat-2 同样节奏；~20:35–21:35：repeat-3；随后配对校验+统计 → run.exit
- **唯一该担心的信号**：请求速率掉到 ~0 且 running=0 且长时间无推进 → 真停滞，按手册查

## 3. 完成后（run.exit=0）

harness 自己会出分并写：`stats-hybrid.json`、`stats-hybrid+unified.json`、`paired.json`、
`unified-pair-validation.json`。核对三件事：

1. `paired.json`/stats：两臂 3-rep majority 正确数与逐题 flips（unified 修回/害）、McNemar；
2. 对照系：**control(hybrid)@k150 = legacy 同批复测**（跨批 90.13% 仅参考）；
   unified@k30=87.92%（`locomo-paired-classify`）；零成本推演估计 unified@k150≈**89.1–89.4%**
   （可迁移收益池 37 题 / 可迁移危害池 14 题，净 +23 ≈ legacy 的 +25，见 §5）；
3. 结论口径：unified×k150 若 ≈89.4 且 < control@k150 → 印证"unified 修 prompt 病、k150 修上下文量，
   两机制正交；LoCoMo 高分栈仍是 legacy k150"，042 的路由前提在两个栈下都成立。

然后按规矩收尾（已授权）：**备份小结果文件 → 关机停止计费**：

```bash
TS=$(date +%Y%m%d-%H%M%S); mkdir -p /root/autodl-tmp/eval-backup-$TS
cp -r /root/autodl-tmp/038-runs/locomo-paired-150/*.json /root/autodl-tmp/038-runs/locomo-paired-150/*.log \
      /root/autodl-tmp/038-runs/locomo-paired-150/run-* /root/autodl-tmp/eval-backup-$TS/ 2>/dev/null
shutdown now
```

## 4. 若进程死了 / 需要重跑

- 重跑=执行原脚本（rm -rf 语义，全新目录）：`cd /root/autodl-tmp/038-runs && setsid nohup bash run-locomo-paired-150.sh </dev/null >/dev/null 2>&1 &`
- 注意：该协议 **fail-closed 拒绝 resume**（"refuses journal resume ... use a fresh --run-dir"），
  半截 run 只能整跑，不能续。
- 维护者此前已决定"不跑 3 次"——若 3 reps 太久，可改 `--repeats 1`（脚本内一处），
  1-rep ≈ 1 小时，口径标注 `1-rep interim`、不进 result-matrix 坐实行。

## 5. 本次已完成的 042 工作（换机后从这接）

### 5a. 042 spec 链对抗式评审：verdict = **NEEDS CHANGES**（spec 在 worktree，未 commit）

worktree：`.claude/worktrees/042-counterfactual-evidence-utility`（分支基线 54d022c，早于 cleanup
commits 3cff168/ac5c66c，实现前需先合入 master）。要点（完整清单在会话记录，需回写 spec）：

- CRITICAL：spec SC-003 把诊断门钉在"历史 040 效用集"上，与 plan/research（fresh collect
  cross-fit）矛盾且按字面不可实现（历史无概率信号）；
- HIGH：research"沿用 temperature=0"是事实错误（现 recipe 不发送 temperature，wire.go omitempty
  → 服务端默认非零；nav/trace 先例硬编码 0，照抄即 recipe 漂移）；EMBED_* 未冻结（默认
  Ollama qwen3-embedding，会错查 bge-large 的 store）+ manifest 无 embedder 字段；
  `--judge-mem0-aligned` 缺席 contract 冻结输入（quickstart 有、contract 无，默认 false 会整批返工）；
  +25 净增常数 vs 新批次 deep-net 的交互（建议 min(25, D)）；单次 attempt+FAILED→INVALID 在
  ~2 万调用规模上运维脆弱（建议预注册有界 retry）；
- 可行性结论：预算算术成立（ratio=S/D+f+u，f+u≤0.10–0.18 vs BENEFIT 率 3.6%），路由精度要求
  56c−31h≥25（c=70%→h≤46%），诊断存活估 25–40%、最终 GO 估 15–25%；建议全量 collect 前先做
  2-conv 信号存在性 pilot（AUC<0.65 直接 NO-GO，省 8/10 采集）。

### 5b. 零成本推演（已在 box 上算完，脚本在 `/root/autodl-tmp/042-scratch/`）

逐题 join（032-think3 / tk150-full3 / locomo-paired-classify，156/31/+25 校验通过）：
- unified@k30 只顺带修了 legacy 56 个深检索收益中的 19 个（34%）→ **unified 与 k150 正交**；
- unified@k150 点估计 **89.1–89.4%**（对迁移率不敏感），大概率 < legacy k150 90.13%；
- 041 的 1-conv "U-k30=U-k150" 全量不成立，unified 栈 top-k 税仍在（完美路由净空间 +23 题）；
- 跨批 join 噪声 ±2pp（C30 vs L30 同 prompt 分歧 72/1540），推演不能替代本次同批配对。

### 5c. box 上的遗留物

`/root/autodl-tmp/042-scratch/`：probe/p2/p3 有界验证 run、sse_test.py、
analyze_unified_tax.py + refine.py（推演脚本）、sick-run-forensic/（16:35 病历现场，供根因追查）。
均可删；删前按惯例先备小文件。

## 6. 战略结论（数据已齐，等本次 run 收口）

unified 在 LoCoMo 的角色=契约正确性/可移植（涨点在 LME：90.2%@k30 已坐实）；LoCoMo 最高分栈
仍是 legacy thinking+k150（91.10 锚）。**042 的 logprob 路由前提在两个栈下都成立**——
run 结束后即可按 §5a 修订清单进 tasks，不必再等其它数据。
