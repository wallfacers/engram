# T19 交接文档 ② — B/D 最小对照 run · 收数与后续（2026-09-28 晚）

> 写于 2026-09-28 ~18:10 CST，by 本会话 agent。接手者（回家续跑）请通读。前序完整背景见
> `t19-handoff-20260928.md`（两 BUG 诊断）与 `tasks.md` 尾部记档（修复+转向全史）。

## 1. 一句话现状

**B/D 最小对照 run 正在盒上跑**（维护者令：停五臂省资源，先看 k150+jev 效果）：
`$D9 = /root/autodl-tmp/051-jev-arms-runs/bd-pilot-20260928T163756`，runner PID **24790**，
16:38:17 点火，ETA **~19:07**（3,080 journal 行 @~20 行/分）。18:06 实测 60%、degraded 13/1842=0.71%。

## 2. 本轮改动（全部已 commit，分支 `codex/051-jev-relevance-filter`）

| commit | 内容 |
|---|---|
| `d605170` | **T19 两 BUG 修复**：jev-native validity receipts（BUG ② exit-1 缝：verdict 自证回执，`jevCoreValidityArtifacts` 改 jev 真实产物集，022 四文件/formal_calls 是 Evidence Compiler 产物、arms 路径不走、造假=红线）+ embClient 穿透 + `validateJevEmbeddingWiring` fail-closed（BUG ① 语义信号 nil）。全门绿、引擎 diff 空。 |
| `93b5566` | **`--jev-answer-arms`**：全五臂照测（检索/打包/filter journal 3,080 行不变），仅声明臂花 answer+judge；子集封进注册 digest（空=全臂字节不变）。 |
| `d554e40` | freeze 路径同 parse answer-arms（漏 parse 会让 freeze 无子集→run binding 拒跑；首次 freeze 的 bd-pilot-20260928T163431 已标 SUPERSEDED）。 |
| `d0f3155`/`3065391` | docs 记档（$D8 五臂点火后被维护者令 STOP，28min/298 行，ABORTED.note 标记）。 |

思考口径已核验对齐 91.10 栈：thinking 开（serve-051.sh 无禁参数）、max-tokens 8000（=历史 022 配方）、
max-model-len 32768、judge 拿剥离思考后 clean 终答。上轮 D7 实证 answer_out p50=1064/max=8000。
上下文预算：B 输入 5,811 + 输出 8,000 < 32,768 ✓。

## 3. 收数步骤（ETA ~19:07 后任意时刻）

```bash
source ~/.engram-eval-secrets.env   # ENGRAM_EVAL_SSH_HOST/PORT/USER/PW
ssh -o ConnectTimeout=15 -o BatchMode=yes -p "$ENGRAM_EVAL_SSH_PORT" "$ENGRAM_EVAL_SSH_USER@$ENGRAM_EVAL_SSH_HOST"
D=/root/autodl-tmp/051-jev-arms-runs/bd-pilot-20260928T163756
cat $D/gated.exit $D/chain.done     # 预期 0 / 0（T19 修复生效的判据；$D7 时代恒 1）
ls $D/                              # 应有 summary.json + jev_arms.jsonl + jev_arm_verdict.json
```

然后：
1. **verdict 核验**：`jev_arm_verdict.json` 的 `promotion_verdict` 应非 INVALID；`summary.json` 的
   `validity` 应 complete（questions 1540、rows 7,700、answer_arms=[B,D] 的 compliance 分母=3,080）。
   verdict/contrast 只会包含 B→D（C/A 未答题 → 相关 SC HOLD/未测，预期内）。
2. **算分**（盒上无 python3，用 awk 或拉回本地算）：从 `jev_arms.jsonl` 按 (conv,q) 配对 B/D 臂
   `correct_measured`+`correct`，出：B 绝对正确率、D 绝对正确率、B→D 差距（配对翻转数）。
3. **判读框架**（对照数）：
   - B 绝对水位：上轮（语义信号死的）60.65%；历史 91.10（tk150+thinking+3-rep+chunk-verbatim）。
     语义修复后 B 回升多少 = BUG ① 影响的量化。若仍 ~60 → 证据形态主嫌疑坐实（收数文档 §5.1：
     短 facts ~48tok/条 vs chunk-verbatim，gold-recall 0.92% 矛盾）。
   - B→D 差距：上轮 −3.77pp（p=3.4e-05，SC-001 HOLD）。语义上线后池子 gold 密度升，看 D 能否
     贴进 −0.5pp 门。SC-002 压缩数据（D shown/token vs B）照读。
4. **降级行**（当前 13 行）：**绝不全量重跑**（维护者令 11:3x）。真补齐=逐行 filter 重调（shim
   :8020）→ 用过滤后证据重答（1 次 vllm）→ 重判（1 次 judge）→ 修补记录 → 原版/补齐版 verdict 并列
   报告，标注「pilot + degraded-row backfill patch」。成本 ~13×3 次调用（几毛钱）。
5. 小结果拉回 `specs/051-jev-relevance-filter/pilot-artifacts/`（bd- 前缀）：jev_arm_verdict.json、
   jev_arms_report.json、gated.log、protocol.json、summary.json、jev_filter_calls.jsonl 的 digest（大
   journal 本地放 .scratch）。写收数报告（对齐 t19-p3-pilot-collect2.md 格式）。

## 4. 盒上资产（AutoDL，开着、计费中，维护者知情）

- 凭据：`~/.engram-eval-secrets.env`（SSH paramiko 或 key 均可）；真 Jev key 同文件的
  `AI_GATEWAY_API_KEY`（网关已充值，分离冒烟 1/0/0/0 实证有效）。
- 服务：answerer :8000（Qwen3.6-35B-A3B-FP8，32768）、embed :8010（bge-large，max-num-seqs 1）、
  shim :8020（typesafe，91d02cc9）。
- 部署：`bin/locomo-bench` = `793eec63` @ `d554e40`（bin/REV 同步）；链脚本 `bin/run-p3-bd.sh`
  （sha cadfc992，D 行=$D9 + `--jev-answer-arms B,D`）；clone `/root/autodl-tmp/engram` HEAD=d554e40
  porcelain=0。
- store：`/root/autodl-tmp/051-stores/stores`（10 店复用，extraction 零花费）。
- **关机时机问维护者，勿自作主张**（当前令=跑完不急关机，留着补齐/拉数据）。

## 5. 硬规则（违反=事故）

1. 引擎不可碰：`git diff --name-only -- memory embedding provider store internal` 必须空。
2. 秘密只经 env；不入仓/日志/工具输出/argv。ssh 带 `-o ConnectTimeout=15 -o BatchMode=yes`。
3. 杀进程按 PID（禁 pkill -f）；长命令 setsid detach；禁本地 sleep 循环轮询。
4. 降级行绝不全量重跑；盒子空闲必停（当前开着=维护者明示）。
5. verdict/gotcha 落 tracked docs（tasks.md + 收数报告），不能只留在会话里。
6. 模型池禁令：relay/cmd-deepseek-v4.1-flash 维护者禁用；glm 配额禁至 2026-09-24 17:21 之外正常。

## 6. 收数后的决策点（顺序）

1. **B 水位判读** → 若 ~60：证据形态诊断成为下一片（上轮已派审计员方向：gold↔memory linkage、
   facts vs chunk-verbatim 打包差异；其产物 `t19-evidence-shape-diagnostic.md` 可能已落，先查）。
2. **D vs B 判读** → 贴门（≥−0.5pp）→ 与维护者商 3-rep 全量；仍差 → θ 调优（θ=0.5 偏狠：上轮
   留 3.8/150、空注入 8%；θ 降到 ~0.35 或调 KShowMax 是首批旋钮）。
3. **k200 排除项**（用户提过）：等盒空闲可跑 `--jev-answer-arms B,D` + 池 200 子集实验（需改
   jevArmPoolSize=评估配置、单独 commit）——但证据已判死（池外召回 0.9%、topk 非单调、harm 31 题），
   预期 0~0.3pp，只做排除不做指望。
4. 3-rep 正式链前必须先过回执缝修复验证（本轮 gated.exit=0 即是证明）。

*End of handoff ②.*
