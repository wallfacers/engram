# Quickstart: 确定性次模证据装填(045)

## 前置

- 本地 032-store(LoCoMo)与 `testdata/locomo/` 数据集(已 gitignored 在仓)。
- US1/AIC 门:**零模型端点、零网络**。box 阶段(US2+)才需要 vllm answerer + judge env。

## US1 · 离线装填保真门(本地,分钟级)

```bash
cd <engram>
CGO_ENABLED=0 go build ./...
go run ./cmd/locomo-bench \
  --data testdata/locomo/locomo.json \
  --store <032-store-dir> \
  --aic-gate .locomo-run/045-offline \
  --aic-gate-slice 0,1        # 先 304 题冒烟;去掉即 1540 全量
cat .locomo-run/045-offline/packing_gate.json   # gate.verdict = GO | NO-GO
```

判定:`packed.aic ≥ 0.95 × top150_full.aic` 且 `packed.tokens_mean ≤ 锚`。NO-GO → 写 verdict 关闭 feature,不上 box。

## US2 · 1-rep 同批配对 probe(box,组合批一次开机)

```bash
# box 上(env 全走进程环境,凭证不落盘)
setsid bash -c 'locomo-bench --data <locomo.json> --run-dir /root/autodl-tmp/045-runs/probe-ctl \
  --retrieval both --chunks --chunk-quota 12 --top-k 30 --unified-answer-contract \
  --concurrency 32 > ctl.log 2>&1; echo $? > ctl.exit' </dev/null >/dev/null 2>&1 & disown
# 对照臂完成后(收逐题 usage 锚),机制臂:
setsid bash -c 'locomo-bench ... 同配方 + --submodular-pack --pack-budget-anchor paired \
  --anchor-run /root/autodl-tmp/045-runs/probe-ctl > pack.log 2>&1; echo $? > pack.exit' & disown
# ride-along(同批顺序执行):
setsid bash -c 'locomo-bench --reverify-042 /root/autodl-tmp/045-runs/reverify \
  --reverify-labels <042-collect-dir> --concurrency 32 > rv.log 2>&1; echo $? > rv.exit' & disown
```

GO 判定:配对差 ≥0 且 McNemar 不显著为负 → 同一次开机接着 3-rep 正批 + LME(008 铁律);否则收尾关机。

## 验证清单

- [ ] 旗标关:`--help` 可见新旗标;默认配方 byte-parity golden 绿。
- [ ] 装填确定性:同输入两跑,`packing_audit.jsonl` 逐字节一致。
- [ ] US1 门报告:三口径 AIC + 审计(unmatchable 单列)。
- [ ] probe 工件:配对差、p 值、token parity(真实 usage 口径)。
- [ ] 重验工件:有效采集率 + AUC + flip + verdict 枚举。
- [ ] box 收尾:小文件备份 `/root/autodl-tmp/eval-backup-<ts>/` → `shutdown now`(必做)。
