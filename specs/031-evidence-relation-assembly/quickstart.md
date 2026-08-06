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

## 2. 子集配对评测（008 铁律，云端）

84 题 = 029 实际子集 `specs/030-evidence-mediation/diagnosis/phase0-ids-029-84.txt`
（同 store/answerer/judge/cap，唯一变量 = 关系上下文 arm）。基线 flag 复用 030：
`--chunks --retrieval hybrid --top-k 30 --chunk-quota 12 --force-answer --judge-mem0-aligned`。

```bash
# 031 装配 arm（相对 030 keep 基线）
bash run031.sh RUN_DIR --only-questions phase0-ids-029-84.txt --repeats 3 \
  --evidence-assembly --relation-context
# 031 + trace 叠加（单独报告叠加效应，不混报）
bash run031.sh RUN_DIR --only-questions phase0-ids-029-84.txt --repeats 3 \
  --evidence-assembly --relation-context --trace-mediation
# 配对分析
./locomo-bench --compare DIR_KEEP DIR_RELATION    # flips + McNemar p + verdict
```

**GO 门**：3 次多数 `--relation-context` ≥ 030 keep 基线（008 铁律），且 multi-hop/temporal 类别不回归（L0-3）。

## 3. 全量复跑（子集 GO 后）

```bash
bash run031.sh RUN_DIR --evidence-assembly --relation-context --repeats 3   # 全量 1540
bash run031.sh RUN_DIR --evidence-assembly --relation-context --trace-mediation --repeats 3  # 叠加
./locomo-bench --compare DIR_KEEP_FULL DIR_RELATION_FULL
```

**期望**：majority 相对 030 全量基线（85.91% trace / 84.9% base）不回归；叠加时单独报告增量。

## 4. 诚实边界（写报告时必附）

- MemCog 消融 delta（↓6.79 / ↓6.53）是「MemCog 完整系统内移除组件」的差异，不是 engram 栈上的叠加差异；预期是「试一把」而非「必然涨点」（029 模拟高估教训）。
- 配对以 engram 同栈 majority 为准；单次差值不算数。
- 结构上下文只在 multi-hop / temporal 类别生效（single-hop/generic 不注入）。
