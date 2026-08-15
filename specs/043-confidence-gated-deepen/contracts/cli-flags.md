# Contract: CLI Flags(043)

**Date**: 2026-08-15 | **冻结于实现之前**(宪法 III:契约先行)

## 新增 flags(cmd/locomo-bench)

| flag | 默认 | 语义 |
|---|---|---|
| `--confidence-deepen` | `false` | 机制总开关。false = 主路径逐字节不变(验收 golden) |
| `--deepen-pilot` | `""`(空) | stage 型旗标(照抄 `--utility-stage` 模式,main.go 早期分派)。取值:`signal`(2-conv 双信号 AUC pilot)。空 = 普通路径 |
| `--deepen-threshold` | `0`(未定稿) | 机制臂只读 pilot seal 定稿值;命令行显式传入非定稿值时**必须报错拒绝**(防偷偷调参,R5) |
| `--deepen-signal-feature` | `""` | 同上,只读 pilot seal(featureName) |
| `--deepen-k` | `30` | 补检单轮条数(round-0 配额之外追加,不参与 N-r 拆分) |
| `--deepen-max-gaps` | `3` | 每题缺口上限(schema 侧同时校验) |

## 互斥与组合

- `--confidence-deepen` 与 `--gap-refetch`、`--agentic-nav`、`--iris`、multi-query 臂、`--lme-*` 特调族:互斥(进 `validateUnifiedPromptPairExperiment` 冲突表;`--deepen-pilot` 除外)。
- `--confidence-deepen` 与 `--unified-answer-contract`:**必须同开**(机制定义在 unified 配方上;契约 digest 必须等于现行 `1d8a8d0f`)。
- 与 `--repeats`/`--store-dir`/`--chunks`/`--chunk-quota`:照常组合(round-0 配方冻结)。

## 机制臂命名

`supportedArmMechanisms` 新增 `"deepen"`;arm 全名 `hybrid+unified+deepen`;对照臂 `hybrid+unified`。

## 输出格式契约(answerer 侧,唯一新增 prompt 接触面)

机制臂 answerer 的输出 = 现行 unified 契约要求的 final answer **之后**,追加一个缺口块:

```
<DEEPEN_META>
{"gaps":[{"category":"bridge_entity","target":"...","slot":"...","description":"..."}]}
</DEEPEN_META>
```

- 分隔符固定为 `<DEEPEN_META>`/`</DEEPEN_META>`;judge 输入**剥离该块**(判分只看 final answer,clean 口径不受污染)。
- 该块是输出格式契约,不是答题 prompt 措辞;unified 契约常量(`runner.go` `unifiedAnswerContractPrompt`)**字节不动**,机制说明以追加 system 段之外的形式注入(harness 拼接层,契约 digest 校验范围之外的字段单列,不得混入 `formalAnswerPromptDigest`)。
- 解析失败(无块/坏 JSON/超条数)= 信号按自信处理,不加深,记 `failure_kind=gap_parse_failed`。

## 确定性映射契约

```
query(gaps[0]) =
  nonempty(target) && nonempty(slot) ? target + " " + slot
: nonempty(description)                            ? description
: nonempty(target)                                 ? target
: 原问题
```

只取第一条存活 gap;纯字符串操作;同输入必同输出(单测锁定)。
