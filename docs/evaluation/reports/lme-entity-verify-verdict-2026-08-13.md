---
title: LME entity-verify 实体替换门 — post-hoc 诊断（2026-08-13）
summary: 在同一 LME-S 500 测试集上做错题归因后设计 entity-verify prompt；旧实现的单次 clean 点估计为 89.80% vs C 臂 87.80%（+2.0pp，McNemar p=0.1102），但 prompt 直接使用了该测试集真实陷阱，属于 post-hoc in-sample tuning。该数据集专用实现现已放弃并由默认关闭的统一回答合同实验取代；旧分数不适用于新合同。
status: diagnostic
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
tags: [research, longmemeval, entity-verify, diagnostic, prompt-contract, data-leakage]
---

# LME entity-verify 实体替换门 — post-hoc 诊断（2026-08-13）

## 裁决

**诊断信号存在，但当前没有可用于出货或转正的有效提升证据。**

旧融合 prompt 在同一 LME-S 500 上的单次 clean 重判为 449/500（89.80%），C 臂为
439/500（87.80%），点估计 +2.0pp；配对 discordants 为 21/11，McNemar exact
`p=0.1102`，未达显著。该 prompt 是看过同一测试集错题后设计，并把真实测试题中的实体对直接写进
示例，因此这是 **post-hoc、in-sample benchmark prompt tuning**，不能解释成可泛化提升。

后续审计还发现，“换成合成示例”仍没有解决按数据集类别路由、固定 gold 拒答措辞和看过测试集错误后
调参的问题。**旧 89.80%/90.00% 等测量不绑定当前统一合同，当前也没有合格的 held-out
评测证据。** 因此 `--lme-entity-verify` 及其 prompt family 已从未发布工作树中移除，改由
`--unified-answer-contract` 做跨数据集、无类别标签、无示例的 default-off 实验。

## 旧实验观察值（仅诊断）

以下各臂使用相同 LME-S 500，但不是完整的配对多重复设计：

| 臂 | harness judge（单次） | 同批 clean 重判（单次） |
|---|---:|---:|
| A：force-answer | 86.00% | 86.80% |
| C：force-answer + LME typed contracts | 87.60% | 87.80% |
| entity-verify 早期单独版本 | 88.00% | 88.80% |
| 旧融合版本：typed contracts + entity-verify | 90.00% | 89.80% |

旧融合版本相对 C 的 clean 配对结果：

- 449/500 vs 439/500，点估计 **+2.0pp**；
- fusion 对 / C 错 = 21，fusion 错 / C 对 = 11；
- McNemar exact 双侧 `p=0.1102`，**正向点估计但统计未显著**。

旧融合版本另有三次自身运行：88.80%、88.00%、88.80%，均值 88.53%。没有对应的三次 C
对照，且这些分数不是与 C 同批的 paired clean repeats；因此不能用 88.53%−87.80% 声称
“真实 +0.7pp”。这三次只能说明该旧 treatment 的三次观测均未达到 90%。

## 机制假设

LME-S 中存在一类实体替换题：问题问实体 X，记忆只包含相似但不同的 Y，gold 是信息不足。
force-answer 的“必须猜测”规则容易让模型把 Y 的属性错误地迁移到 X。

实验门在事实类问题上要求先核对问题实体：只有相似但不同的主题时输出
`The information provided is not enough.`；multi-session 与 temporal-reasoning 类别分别保留聚合和
时间推理合同。这个机制假设值得做 held-out 验证，但本次测试集不能再承担验证集角色。

## 测试集泄漏

旧 prompt 的示例逐字使用了本次 LME-S 500 中的真实题型，包括：

- `vintage films` 与 `vintage cameras`；
- `Dr. Johnson` 与 `Dr. Smith`；
- `Italian restaurants` 与 `Korean restaurants`。

报告归因时列出的 table tennis/tennis、football/baseball、uncle/niece 也来自同一批测试题。
这会直接教模型识别被计分的陷阱模板，违反独立验证要求。一次中间修订曾改用 `Project Atlas`/
`Project Beacon`、`Dr. Rowan`/`Dr. Vale` 两组合成例，但这仍是为同一测试模板写的特殊提示，未解决
根因，现已一并删除。统一合同不含 few-shot 示例，静态测试禁止已知测试短语、benchmark 名、类别和
固定拒答文本进入 prompt。

## 当前实现边界

- LME-only entity flag、entity-verify 的类别 6–10 路由、合成例和固定拒答文本均已移除；历史 LME typed prompt 仍只作 control；
- 替代实验 `--unified-answer-contract` 同一文本覆盖 LoCoMo/LongMemEval 所有类别，不读取类别号；
- 普通、B0、formal B1/compiler、frozen replay validation 与 fixed-gold 路径统一走同一个 prompt hook；
- 新 run-dir 把实际 prompt 字节写入普通 journal fingerprint 与 formal frozen prompt digest；旧 run-dir
  一律不得续跑；
- 实验期与旧 prompt selector、`--counter-refine` 和类别预算互斥，只为保证单变量归因；
- engine 未修改；新合同尚未跑分，当前 verdict 是 `BLOCKED`，不是产品能力。详见
  [统一回答合同审计](unified-answer-contract-verdict-2026-08-13.md)。

仓库中现有的 17 个行为案例只是与统一合同同期编写的开发 smoke
fixtures，不是 held-out 替代品，也不能建立 2% false-abstention 上限。

## 其他探索性观察

旧融合版本叠加 `trace-mediation + relation-context` 的两次运行是 86.4%/87.2%，低于旧融合版本
自身三次的 88.8%/88.0%/88.8%。该结果只覆盖组合臂、仅两次、也没有完整配对显著性，不能分别
断言 trace 或 relation 为负；最多说明不应在没有隔离实验前继续叠加这组配方。

旧单次 90.0% 高点的 50 道错题分析可用于提出计数、时间推算、偏好和多值选择等后续假设，不能用来
证明“Qwen 能力天花板”“纯客户端杠杆已穷尽”或“90% 不可达”。该 run 恰是正噪声高点，用它估计
剩余错误上限还带有选择偏差。

## 合格复验要求

下一次验证必须：

1. 预先冻结无示例的统一合同与评测方案；
2. 使用从未参与错题分析、按 conversation/template 隔离的 held-out；
3. 历史 control 与统一合同同时跑至少 3 repeats，并在同批 judge 下配对；
4. 报告每 rep 配对差、逐题 majority McNemar、数据集/代码/model revision 与 artifact SHA256；
5. held-out 通过前不转正、不写入默认 benchmark 结果。

## 可复现性缺口

旧 entity/fusion run 的脚本、manifest 与 artifact hashes 未入仓；“entity-verify 早期单独版本”也不是
当前 CLI 可重建的 prompt 状态。因此旧表只保留为历史诊断记录，不能作为独立复现或 promotion 证据。

相关背景：[counter-refine 组合臂报告](counter-refine-verdict-2026-08-13.md)、
[LME 三臂合同实验](lme-contract-arms-2026-08-12.md)。
