# Data Model: 查询期 verbatim 证据编译(026 增量)

**Feature**: 026-verbatim-evidence-compile

**Date**: 2026-08-01

## 设计规则

1. **026 不新增持久实体**。复用一个经 022 验证的不可变链条:Evidence Ledger → Frozen Candidate → Compiler Action → Grounded Trace → Evidence Bundle → 一次 Answerer 调用。
2. 026 的增量是**编译策略(arm)**——同一组 frozen candidate + Evidence 之上不同的 action 选择与组装规则,不新增存储。
3. 若 verbatim-first 需要新的确定性编译策略(如 source 内子跨度定位),作为显式 contract increment 评估(宪法 II),不预建。
4. 所有时间 Unix microseconds;span 用 Unicode code-point `[start_char,end_char)`(022 已冻结)。

## 关系总览(继承 022)

```text
Evidence (immutable content)   ← append-only Ledger,只读
  │
Frozen Candidate ──> Candidate lineage ──> Evidence
       │
       └── [026] Compile Arm 选择 action
             ├── legacy-count        (现状:按条数装填)
             ├── exact-token         (022 已实现)
             ├── deterministic extractive (026 补齐)
             └── verbatim-first      (026 核心新臂:原文优先双态)
                    │
                    ├── KEEP / FETCH_SOURCE ──> 原始 span(装得下)
                    └── EXTRACT / MERGE ──> 有来源压缩(装不下)
                    │
                    └── Grounded Trace ──> Evidence Bundle ──> 一次 Answerer
```

## 实体(026 全部复用 022 类型,见 `memory/evidencecompiler/internal/contracts/types.go`)

| 实体 | 来源 | 026 的用途 |
|---|---|---|
| `Candidate` | 022 | 冻结输入;verbatim-first 沿其 `SourceIDs` lineage 回收原文 |
| `Source` | 022 | 批量 Resolve 的原始证据;verbatim-first 的核心内容来源 |
| `SourceSpan` | 022 | EXTRACT 的输出 span(Unicode codepoint) |
| `EvidenceNeed` | 022 | 声明作答缺什么(compiler 不猜) |
| `Action`(KEEP/EXTRACT/DROP/MERGE/FETCH_SOURCE) | 022 | verbatim-first 的 action 闭集;无来源 ADD 拒绝 |
| `GroundedTrace` | 022 | 审计记录(Need/actions/span/source IDs) |
| `EvidenceBundle` | 022 | answerer 唯一输入;token cap 内、逐项绑 source |

## 026 增量(仅编译策略,非持久实体)

| 概念 | 类型 | 语义 |
|---|---|---|
| `CompileArm` | enum(string) | `legacy_count \| exact_token \| extractive \| verbatim_first`;arm 是同一 frozen candidate 上的确定性编译策略 |
| verbatim-first 双态 gate | 逻辑 | 原文装得下 → KEEP/FETCH_SOURCE;装不下 → EXTRACT(按 relevance 排序)仍不够 → MERGE(逐句验证);复用 022 extract.go 的 raw-fit/MERGE gate |

**不新增**:无新表、无新迁移。026 的"原始 span 回收"沿候选 lineage 走 022 的批量 `Resolve(ids)`,禁 Search/query。
