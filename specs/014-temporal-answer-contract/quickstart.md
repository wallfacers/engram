# Quickstart: 验证答题侧时序推理契约

## 前置

- Go 1.25,`CGO_ENABLED=0`。
- US2 e2e 需 box 全本地栈:answer/extract vllm(:8000)+ embedder bge-large vllm(:8001)+ judge deepseek(`JUDGE_*` env)。隧道 `-L 8000 -L 8001`。见 `docs/locomo-e2e-eval-reproduction.md`。

## US1 验证(离线,零 box/token)—— 必做,先绿

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run TemporalContract
```

**期望**:
- 四锚断言通过(枚举 / 相对→绝对 / 精确匹配反±1 / 时长相减 在契约文本内)。
- 三不变量通过(category≠2 / 开关关 / abstain 优先 逐字节不变)。
- 引擎零改:`git diff --name-only -- memory embedding provider store internal` 输出为空。

## US2 验证(box 三臂 e2e 门)—— GO/NO-GO 决策

一次 run 三臂,canonical recipe(`--top-k 30`,**无 cat-top-k**)+ `--repeats 3`。**WSL2 长任务用 setsid detach + 文件轮询**(隧道打包进脚本内,见 013 gotcha)。

```bash
# 每臂公共:
#   source ~/.config/engram/locomo-vllm.env ; source ~/.config/engram/judge.env
#   export EMBED_BASE_URL=http://127.0.0.1:8001/v1 EMBED_MODEL=BAAI/bge-large-en-v1.5 EMBED_API_KEY=local-eval
#   STORE=.locomo-run/009-bge-chunks-store ; DATA=testdata/locomo/locomo10.json
# 公共 flag:--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned
#           --retrieval hybrid --repeats 3 --concurrency 48   (无 cat-top-k:默认 30)

# base     (冷首臂 warm-up 丢弃后复跑做干净锚)：<公共>                        (无 --temporal-answer-prompt)
# old-tplan：<公共> --temporal-answer-prompt   (旧契约常量：git stash 算法改 / 临时旧常量)
# new-tplan：<公共> --temporal-answer-prompt   (新强化契约)
```

**判定**:
1. 每臂取 category-2(n=321)逐题正误(3 rep 多数投票)+ overall。
2. `new-tplan` vs `base` 配对 McNemar → **GO 需显著抬 + overall 不回退**(SC-001)。
3. `new-tplan` vs `old-tplan` 差分 → 归因(SC-004:涨点是否归于强化本身)。
4. 跑完 `cat <run-dir>/regime.json` 核四要素(`force_answer=true` / `judge=mem0-aligned` / `judge_model=deepseek-v4-flash`)。
5. **冷启动纪律**:配对只对干净复跑基线,不对冷首臂(踩坑#10)。

## US3(条件于 GO)

- GO:把契约全文 + 三模式动机 + 复用前置条件(记忆行须带绝对 `[event:]` 锚)写入 `docs/temporal-answer-contract.md`(可移植 pattern)。
- NO-GO:落带归因的 verdict 于 `docs/locomo-score-levers.md`,记录升级路径(确定性日期脚手架 Option B),不产出移植文档。

## 提交纪律(Constitution IV)

算法改(契约常量 + 单测)与评测配置/结论**分开提交**(attribution)。verdict 落 tracked docs 后再更新本地 memory 指针。
