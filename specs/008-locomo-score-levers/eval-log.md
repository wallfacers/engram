# Feature 008 Evaluation Log - LoCoMo Score Levers

本文件只记录实跑结果。按宪法 IV，评测结果与 mechanism 改动分开提交；负结果同样
保留，不能包装成涨点。

## US2: open-domain 五步推理 prompt 单变量 A/B

- **Date**: 2026-07-22 14:17-14:20 +08:00，旧版与新版连续单次运行。
- **Code**: 旧版 `0f06b78`；新版 mechanism `61b5311`。
- **Dataset**: `locomo10.json`，仅 category 3，共 96 题；两侧 question_id 96/96 对齐。
- **Store / retrieval**: `.locomo-run/007-us2/cov-store`，`hybrid`，`--chunks`，
  `--top-k 100`，`--chunk-quota 50`。
- **Answer**: 本地 `Qwen/Qwen3.6-35B-A3B-FP8`。runner 的 Temperature 为 Go
  零值，但 OpenAI wire 使用 `omitempty`，实际请求没有发送该字段；远端模型
  `generation_config.json` 实测为 `temperature=1.0, top_p=0.95, top_k=20,
  do_sample=true`。因此 feature 注记中的“默认 temp=0、确定性”假设在本次后端上
  **不成立**，两臂实际共享同一采样配置。**embedding**: 本地
  `bge-small-en-v1.5`；**judge**: `deepseek-v4-flash`，`--judge-mem0-aligned`。
- **Artifacts**: `.locomo-run/008-opendomain-{old,new}/results-hybrid.jsonl`；
  配对报告为 `.locomo-run/008-opendomain-old/compare.json`。
- **Single-variable audit**: 两侧命令参数、store、answer/embedding/judge 模型和
  regime 逐字一致，除必要的 `--run-dir` old/new 后缀外，唯一运行时机制变量是
  `openDomainAnswerPrompt` 文本。`forceOpenDomainAnswerPrompt` 与
  `answerPromptForRegime` 内容哈希均与基线一致；引擎目录 diff 为空。

### Result

| Arm | Correct | Accuracy | Delta |
|---|---:|---:|---:|
| old prompt | 61/96 | 63.5% | - |
| five-step prompt | 59/96 | 61.5% | **-2.1pp** |

配对 McNemar（任务定义 `b=旧对新错`、`c=旧错新对`）：**b=7，c=5，
p=0.774414**（双侧精确检验）。方向为净减少 2 题，且完全不显著。旧版相对 007
参考点 `60/96=62.5%` 多 1 题；结合上述服务端 generation config，应归因于采样
噪声，不能声称是 temperature=0 下的确定性微扰。

### Regression check against 007

`openDomainAnswerPrompt` 只由 category 3 非 force 分支选择，因此其余三类没有执行
路径变化，也无需重跑。007 原产物的冻结值为：multi-hop **248/282=87.9%**，
temporal **265/321=82.6%**，single-hop **733/841=87.2%**；三类合计
**1246/1444**，保持不变。

以这 1246 题与新版 cat-3 实跑结果外推全量：**1305/1540=84.74%**，相对 007
参考点 **1306/1540=84.81%** 为 **-0.065pp**，未超过噪声；相对同窗旧版外推
`1307/1540=84.87%` 为 `-0.130pp`，同样不是实质回归。

### Verdict

**SC-003 FAIL / NO-GO**：五步 open-domain prompt 没有实质上升，反而在本次同窗
采样配对中下降 2.1pp（McNemar p=0.774414）；这组 b/c/p 只描述本次实现结果，
不能冒充确定性复现。保守过闸纪律下，未证明实质上升即不得报告或采纳为涨点杠杆，
不翻其它默认。若未来重评，必须先显式固定服务端 temperature=0，或预先冻结双方
相同 repeats 的统计设计。
