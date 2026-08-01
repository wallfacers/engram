# Compile Arm Contract

**Version**: 026.v1
**Supersedes/Builds-on**: 022 `compiler-contract.md`(Evidence Need / Action 闭集 / SourceResolver / Bundle / Trace 全部继承,不改)

**Package**: `cmd/locomo-bench`(adapter 层 arm 实现)+ `memory/evidencecompiler`(引擎契约,只读复用)

## Boundary

Compile arm 位于 retrieval 之后、answerer 之前。每个 arm:

- 接收**同一组冻结 `[]Candidate`**(候选 ID/TextDigest/rank/source IDs 是 run 内冻结输入,arm 不得修改);
- 沿候选已声明 lineage 精确读取 source(批量 `Resolve(ids)`,禁 Search/query);
- 生成 `EvidenceNeed` + `GroundedTrace` + `EvidenceBundle`;
- 用实际 answerer 的 tokenizer 对完整最终 input 重计;
- 不调用 answerer,不知道 benchmark/category。

## Arm 集合

| Arm ID | 策略 | 实现状态 |
|---|---|---|
| `legacy_count` | 按条数装填(现状基线,control) | 022 legacy runner;026 用其作为配对对照 |
| `exact_token` | exact query-token relevance 排序 | **022 已实现**(`compileExactTokenArm`) |
| `extractive` | verbatim-first 双态(原文优先 KEEP,装不下才 EXTRACT/MERGE) | **022 引擎已实现**(`internal/extract`);026 验证 + 配对 |

## verbatim-first 双态 gate(022 引擎已实现)

```
原文能装入 cap?
  ├─ YES ──> KEEP / FETCH_SOURCE(保留原始 turn/span,按 relevance 顺序)
  └─ NO ───> EXTRACT(span,按 relevance 排序)
              └─ EXTRACT 满足 Need?──> 完成
                  └─ 仍不够 ──> MERGE(逐句验证 source IDs)
```

- 由 022 `internal/extract/extract.go` 实现:`BuildExtractionPlan`(raw vs EXTRACT 双态)、`SelectPackingItems(rawFits)`(原文优先)、`MergePermitted`(over-cap && EXTRACT 不充分)、`ExtractiveSatisfiesNeed`(充分性)。**026 不重写。**
- 无来源 `ADD` MUST 被拒绝(022 已实现)。
- MERGE 仅当原文装不下 **且** EXTRACT 仍不满足 Need 时允许(Retain-or-Consolidate 实证)。

## 输入/输出形状

输入(继承 022):`[]evidencecompiler.Candidate` + 已冻结 `Need` 上下文。

输出(继承 022):

```go
type ArmResult struct {
    Arm     string
    Need    evidencecompiler.EvidenceNeed
    Actions []evidencecompiler.Action   // 已应用
    Trace   evidencecompiler.GroundedTrace
    Bundle  evidencecompiler.EvidenceBundle
}
```

## 校验(继承 022 fail-closed)

- 无来源 `ADD` → 拒绝 + 退回 extractive。
- 无效 citation / 无来源 span / 超 cap → 丢弃该 action,不调 answerer。
- 关闭 legacy IDK retry,最终 Bundle 通过后只答一次。
- 任一 arm 的 retrieval call MUST 为零。

## 配对契约(026 新增)

- 两臂 MUST 同一 store、候选逐字节一致,只差 arm(编译策略)。
- 报告 MUST 含 candidate oracle(gold 是否已在池)以区分 compiler miss 与 candidate miss。
- LoCoMo 答案键噪声(6.4%,multi-hop 9.9%)记录在案;小 delta 不单独作 promotion 依据。
