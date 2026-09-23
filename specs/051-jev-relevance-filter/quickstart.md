# Quickstart & Validation: 051-jev-relevance-filter

验证手册（不放实现代码；契约见 [contracts/](contracts/)，任务分解见 tasks.md）。全程默认**离线可完成**——真 key 步骤均为可选。

## 1. 构建与测试（硬门）

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./filter/... ./memory/... ./mcpserver/...
```

预期：零错误；无任何 `ENGRAM_JEV_*` 环境变量时全部测试照绿（SC-006）。

## 2. 关键不变式测试（新增，必须断言非自比较）

- **parity 不变式**：`memory_search` 不传新参数 → 响应与基线逐字节一致（对照既有 golden/contract 测试扩展）。
- **Select 策略**：0.5 阈值留条 / 全灭放宽 0.35≤3 条 / 仍空返回空 / pinned+trigger 旁路 / KShowMax=12 硬顶。
- **降级**：httptest 强制 500/超时 → `degraded=true` + 结果 = RRF 前 limit 条。
- **写门**：一次性 → skip；密钥未要求 → skip；密钥+明确要求 → write（不回显内容）；门失败 → 无门照写。
- **分片**：>64 候选分片并发合并，任一片失败 → 整体降级（不部分采用）。**离线零网络断言**（互审补）：`filter="jev"` 且无 key → 不发起任何网络请求、结果恰为 RRF 前 limit 条、`degraded=true`。

## 3. Jev 协议冒烟（httptest 桩，离线）

stub server 校验请求形状（pointer 模式：共享 state + 每记忆一道条件化 noul 题）并回固定概率数组；断言两段式结果与 `filter` 遥测字段。

## 4. 真 key 冒烟（可选，密钥只经环境变量）

```bash
export ENGRAM_JEV_BASE_URL=... ENGRAM_JEV_MODEL=<pinned-revision> ENGRAM_JEV_API_KEY=...  # 现场提供，不入任何文件
go build ./cmd/engram-mcp && ./engram-mcp --data-dir /tmp/engram-051-smoke
# MCP 调 memory_search {query:"...", limit:8, candidate_pool:150, filter:"jev"}
```

预期：短名单 ≤12、`filter:{backend:"jev", degraded:false}`、无相关记忆时 `results:[]`（字段名以 memory-search.md 为准）；无 key 时同调用 → `degraded:true` + 8 条 RRF。

## 5. 四臂评测（合入硬门，Constitution IV）

远程 GPU 评测箱（runbook: docs/operations/evaluation/remote-gpu-runbook.md；**run-dir 必须 `/root/autodl-tmp/`**；WSL2/远程长任务 setsid detach；模型侧阶段 worker pool 尊重 `--concurrency`）：

| 臂 | 检索 | 展示 |
|----|------|------|
| A | RRF limit=8 | 8 |
| B | RRF k=150 全塞 | 150 |
| C | RRF k=150 截断 | 门限 12（对照 8） |
| D | RRF k=150 → Jev θ | 过线条目（记条数） |
| D-noRelax | 同 D，`ENGRAM_JEV_RELAX=0` | 过线条目（SC-004 测量） |

口径：unified answer contract、clean judge（`extractFinalAnswer`）、3-rep 多数、同 store 同种子。产出指标：准确率、pool recall@150、recall@shown、短名单精确率、空注率、分段延迟/USD/token、B↔D 翻错题双向清单。判定：SC-001..007（D ≥ B−0.5pp 且无显著回退、平均 ≤12 条且 token ≤ B 的 1/3、recall@shown ≥ C、空注率 ≥ C、降级 ≥ A、无 key 测试绿）；任一不达标 **HOLD**。

互审修订补充：跑前先 `--estimate` 成本预估（4 臂 × 3-rep × 1540 量级）；category-5 对抗题以 declared block 增跑 C/D（SC-004，在 D-noRelax 变体上测）；SC-003 等预算 C@12（C@8 为参考）；显著性 = run 内配对 + 精确 McNemar + 配对 CI；空注操作定义 = **`results == []`（展示集为空）**（单一判据）；SC-004 判定 = D-noRelax 空注率 ≥ C 且绝对下限 ≥ 50%（预注册）。四臂统一冻结 `--answer-input-cap=32768`（B 无截断硬断言：bundle ≤ cap 且 packer 未触顶，触顶则 run 无效）；SC-002 token 比以 packer 实际注入 token 计。038 合法性工件齐备（formal call journal / `evalArtifactValidity.isComplete` 回执 / B0 receipts / pilot+warm-up / 3-rep 同窗）；Jev 调用以独立 `filter` 类进逐行 audit 并记 usage/cost；Jev 钉版进 manifest（`filter_model`，机制键 `filter.jev.v1`）。

结果落 `docs/evaluation/results.md`（正本，措辞限「LoCoMo 口径」）+ `experiment-verdicts.md`；eval 配置与算法改动**分开提交**。

冻结与运行的分工（Slice-6 修订）：过滤器注册表在**冻结那一步**封进 manifest（`--jev-arms --eval-freeze-protocol`，环境里已带 `ENGRAM_JEV_MODEL`/`ENGRAM_JEV_THETA` 等），digest 因此覆盖它；运行（`--jev-arms --eval-protocol <manifest>`）只校验不修改，注册表不一致即拒跑。四条声明缺一不可：`--jev-pilot-gate-confirmed`、`--jev-warmup-disposed`、`--jev-same-window-reps`、`--jev-b0-continuity-declared`（后者为真时合法性门强制要求 B0 continuity summary 工件存在）。四臂配方钉死 `--chunk-quota=0` 且不用 `--cat-chunk-quota`：配额分片会改变池的成员，C 臂截断与过滤臂的对比就不再是单变量。

## 6. 生产旋钮验收

`skills/engram/SKILL.md` 更新后：无 `ENGRAM_JEV_*` 时 skill 行为与今天完全一致；设 `ENGRAM_SEARCH_POOL=150 ENGRAM_FILTER=jev` 后走宽池+过滤。
