# Quickstart: 读侧证据关联装配（Evidence Relation Assembly）

**Phase 1 输出** | 2026-08-06 | 验证场景（离线单测 + 配对评测命令）。

## 前置

- Go 1.25（`CGO_ENABLED=0` 硬门）
- 配对评测需：store（`--store-dir`，含 memory_entities 与 event_date 的 030 冻结 store）、embedding sidecar（bge-large-en-v1.5）、answerer（Qwen3.6 本地 vLLM）、judge（DeepSeek mem0-aligned）——完全复用 030 已冻结栈
- 配对命令模式参考 030：`specs/030-evidence-mediation/reproduction.md`

## 1. 离线单测（零模型，本机跑）

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'Relation|EvidenceAssembly'
```

断言（对应 US1/US2 离线 Independent Test）：
- 确定性：同候选证据集两次 `computeRelationContext` → 逐字节一致
- 三类关系：共享实体 → related_to；日期邻近 → temporal_next；因果词+共享实体 → caused_by
- 空关系 fail-soft：无共享实体/日期/因果词 → 返回 `nil`（不产出块）
- Parity：`--relation-context` 关闭时装配输出与 030 逐字节一致（`TestRelationContextParity`）
- 类别映射：multi-hop 出 related_to+caused_by 链；temporal 出 temporal_next 链；其余类别 nil

## 2. 子集配对评测（008 铁律，arm-to-arm）

84 题 = 029 实际子集 `specs/030-evidence-mediation/diagnosis/phase0-ids-029-84.txt`
（同 store/answerer/judge/cap，唯一变量 = 关系上下文 arm）。

**检索档用 `--retrieval fts`（keyword-only 弱检索）**——依据 [flash 探针](diagnosis/flash-keyword-probe.md)：结构精炼在弱检索下有展示空间（trace +2.8pp p=0.006），强检索 hybrid 下 within-noise（+0.3pp p=0.78）。实测命令（flash 直连，本地 fts，无 embedding 端点）：

```bash
# 实测配方（2026-08-07 verdict，84 题 x 3 reps majority）
bash run031.sh   # 内含 keep / relation / (可选) relation+trace 三臂 + --compare

# 等价手工命令
BIN=.../locomo-bench
F="--chunks --retrieval fts --top-k 30 --chunk-quota 12 --force-answer --judge-mem0-aligned --evidence-assembly --only-questions specs/030-evidence-mediation/diagnosis/phase0-ids-029-84.txt --repeats 3"
$BIN --store-dir flash-84/store --run-dir run-031/keep     $F --trace-mediation=false
$BIN --store-dir flash-84/store --run-dir run-031/relation $F --trace-mediation=false --relation-context
./locomo-bench --compare run-031/keep run-031/relation   # flips + McNemar p + verdict
```

**实测结果**（verdict 收口）：子集 relation +2.4pp（29.8% vs 27.4%，p=0.75）；全量 1540 +1.04pp（48.70% vs 47.66%，+16 题，p=0.253），均 within-noise；生效类别（temporal/multi-hop）全量一致 +3.2pp。

**GO 门**：arm-to-arm 同口径 relation ≥ keep 且生效类别不回归；**不以 030 的 hybrid/Qwen 绝对分作比较**（检索档与模型栈不同，绝对分不可比）。

## 3. 全量复跑（子集后有信号再跑）

```bash
bash run031.sh RUN_DIR --retrieval fts --evidence-assembly --relation-context --repeats 1   # 全量 1540
./locomo-bench --compare DIR_KEEP_FULL DIR_RELATION_FULL
```

**期望**：全量增量方向与子集一致；生效类别一致正向；不生效类别不变（波动为噪声）。叠加臂（relation+trace）未实测——基于独立臂 within-noise + 030/探针推断叠加无显著增量，不混报。

## 4. 诚实边界（写报告时必附）

- MemCog 消融 delta（↓6.79 / ↓6.53）是「MemCog 完整系统内移除组件」的差异，不是 engram 栈上的叠加差异；其 Graph Overlay 是**跨维度页面**建链（LLM 抽取事实 → 语义聚类 → 页面），031 是**单对话候选集内临时共现**——`related_to` 在 LoCoMo 单对话里被说话人主导、信息量受限；`temporal_next`（EventDate）才是可靠主增量。
- 配对以 engram 同栈 majority 为准；单次差值不算数。
- 结构上下文只在 multi-hop / temporal 类别生效（single-hop/generic 不注入）。
- 实体提取是确定性启发式（说话人排除 + 停用词 + 单 token 专名 + 全局边 cap 24）；`since` 因 LoCoMo 时间义 false positive 已从因果词典移除。
