# LoCoMo Single-hop / Multi-hop 错题诊断

## 摘要

本报告诊断 `.locomo-run/008-us4-e2e/results-hybrid.jsonl` 中全部
single-hop 和 multi-hop 错题：

- single-hop：112/841 错，正确率 86.68%。
- multi-hop：40/282 错，正确率 85.82%。
- 两类约 45% 的错题主因都在检索或入库侧。
- single-hop 的第二大问题是 IDK；multi-hop 的第二大问题是 gold 口径和
  IDK。
- multi-hop 中，真正属于“证据已经齐全但推理仍错”的题很少。

## 数据与方法

### 数据源

- 端到端结果：`.locomo-run/008-us4-e2e/results-hybrid.jsonl`
- 同题 rerank 结果：`.locomo-run/008-us4-e2e/results-hybrid+rerank.jsonl`
- 同题 force-answer 结果：`.locomo-run/008-force-answer/results-hybrid.jsonl`
- 原始对话及 gold evidence：`testdata/locomo/locomo10.json`
- 持久抽取库：`.locomo-run/008-embed-large-store/conv*.db`
- 运行日志：`.locomo-run/008-us4-e2e/run.log`

### 分类口径

每道错题只归入一个主因，优先级如下：

1. `(d)`：`predicted` 字面包含 `I don't know`。
2. `(c)`：预测实际正确，或问题、gold、evidence 存在实体、粒度、时间或计数口径问题。
3. `(e)`：预测事实在完整原对话中无依据，或发生明确的说话人/状态幻觉。
4. `(a)`：gold 事实没有进入可用记忆，或已在 store 中但被竞争事实压出有效上下文。
5. `(b)`：相关 evidence 已明显进入回答上下文，但模型选错槽位、条件或计数。

`(a)` 进一步拆成：

- **排序/覆盖问题**：gold 事实已在持久库中，但回答使用其他竞争事实；部分题有
  rerank `false -> true` 的直接 A/B 证据。
- **模态/抽取丢失**：答案只由图片或图像指代承载，fact 和 verbatim chunk
  均未保留所需细节。

### 证据边界

`results-hybrid.jsonl` 没有保存逐题 top-30 memory 内容。因此，“排序未排上”是由
以下证据交叉判定，而不是声称直接看到了检索列表：

- gold 事实是否存在于持久抽取库；
- predicted 是否来自其他真实但不相关的竞争事实；
- 同题 rerank 是否从错误翻转为正确；
- force-answer 是否能利用相同持久抽取恢复答案；
- gold evidence 和完整原对话是否支持预测。

端到端日志另有 270 次 `semantic signal degraded`，其中 260 次 connection reset、
10 次 timeout。由于日志并发输出且没有 question ID，不能把这些退化精确归因到某道题，
但它是本轮检索排序结果的重要运行级干扰因素。

> ✅ **已修复(2026-07-23,bench 侧)**:根因 = sidecar 并发下瞬断 + 引擎按宪法 V
> 静默降级(`retriever.go:754`,生产正确)+ `embedding.HTTPClient` 无重试,一次抖动
> 即整题丢 semantic 路。修法为 adapter-only:`cmd/locomo-bench/embed_retry.go` 的
> `retryingEmbedder` 包装器(3 次尝试、200ms 倍增退避、context 取消即停、
> retried/exhausted 计数落日志),接线于 `buildBenchEmbeddingClient`,引擎零改。
> 后续端到端跑应确认 `embed retries exhausted` 为 0;sidecar 服务端并发配置是
> 运维侧另一半。

## Single-hop

### 失败类型分布

| 类型 | 数量 | 错题占比 | 子类型/说明 |
|---|---:|---:|---|
| (a) 检索未召回 | 51 | 45.5% | 排序 37；模态/抽取丢失 14 |
| (b) 已召回但读错 | 14 | 12.5% | 槽位、主语、时间或粒度选择错误 |
| (c) gold/判分口径 | 20 | 17.9% | 实体绑定、等价答案或粒度问题 |
| (d) IDK | 26 | 23.2% | 字面包含 `I don't know` |
| (e) 事实性幻觉 | 1 | 0.9% | 严格无原文依据 |
| **合计** | **112** | **100%** | |

### (a) 检索未召回

1. `conv-0-q-110`
   - Gold：`a cup with a dog face on it`
   - Predicted：`pots`
   - 诊断：具体答案只在图像 caption；fact 和 verbatim chunk 都没有写入该细节。
2. `conv-0-q-85`
   - Gold：`researching adoption agencies`
   - Predicted：`Caroline and Melanie planned to go on a special family outing or trip together during the summer of 2023.`
   - 诊断：adoption fact 明确存在于 store，回答却使用另一个 summer plan，属于排序问题。
3. `conv-2-q-111`
   - Gold：`kundalini yoga`
   - Predicted：`aerial yoga`
   - 诊断：两项事实都在 store；rerank 臂改为正确，说明旧活动压过了目标活动。
4. `conv-4-q-76`
   - Gold：`MinaLima's creation from the Harry Potter films`
   - Predicted：`map of Middle-earth from LOTR`
   - 诊断：原句和抽取事实都明确；rerank 臂答对，属于竞争事实排序问题。
5. `conv-8-q-104`
   - Gold：`bowl of spinach, avocado, and strawberries`
   - Predicted：`dessert from his cousin's wedding`
   - 诊断：具体食物只存在于图像，文本记忆未保留，回答命中另一张食物图片的相关事实。

**最高 ROI 修法：** 单看确定性收益，抽取/入库侧先补图像 caption；更大的上限在
检索侧处理竞争事实和 embedding 运行退化。

### (b) 已召回但答题读错

1. `conv-0-q-148`
   - Gold：`She was happy and thankful`
   - Predicted：`They enjoyed it a lot`
   - 诊断：两个信息都在 `D18:5`，模型把孩子的体验当成 Melanie 的反应。
2. `conv-1-q-77`
   - Gold：`Hoodies`
   - Predicted：`her own collection`
   - 诊断：同一句已经出现 hoodie，回答却返回所有权关系而非物品类型。
3. `conv-3-q-136`
   - Gold：`Four`
   - Predicted：`1`
   - 诊断：原文明确是 `won my fourth video game tournament`，模型把序数读成单次事件。
4. `conv-4-q-124`
   - Gold：`J.K. Rowling`
   - Predicted：`Harry Potter`
   - 诊断：作者与作品同句出现，回答选错了实体槽位。
5. `conv-7-q-115`
   - Gold：`spending time with loved ones`
   - Predicted：`jogging`
   - 诊断：同一 turn 中同时有晨间 jog 和晚间活动，模型没有遵守问题中的顺序限定。

**最高 ROI 修法：** 答题侧增加实体槽位、主语和时间限定约束；无需扩大召回。

### (c) Gold 或判分口径问题

1. `conv-4-q-136`
   - Gold：`The doctor said it's not too serious`
   - Predicted：`Tim did not have an injury on that date; he mentioned that John had a tough week with an injury.`
   - 诊断：问题问 Tim 的伤，但 evidence 的说话人和伤者都是 John；预测纠错合理。
2. `conv-4-q-166`
   - Gold：`Harry Potter`
   - Predicted：`Lord of the Rings`
   - 诊断：gold evidence 是 Tim 的喜好，问题却问 John；John 原文确实说自己喜欢 LOTR。
3. `conv-7-q-101`
   - Gold：`A year ago`
   - Predicted：`2022`
   - 诊断：按 2023 会话时间，两种时间表达等价。
4. `conv-9-q-151`
   - Gold：`Photography`
   - Predicted：`fixing cars`
   - 诊断：摄影原句 `D30:1` 的说话人是 Dave，但问题问 Calvin；gold 发生实体错绑。
5. `conv-6-q-91`
   - Gold：`tracking inventory, resources, and donations`
   - Predicted：`streamline their operations and make them run more smoothly`
   - 诊断：预测是原句效果的正确概括，只是粒度比 gold 更粗。

**最高 ROI 修法：** 评测侧修正 gold 的实体绑定，并让 judge 接受等价时间、正确概括和
对错误问题前提的纠正；这不是检索或抽取问题。

### (d) IDK 弃答

1. `conv-1-q-63`
   - Gold：`fashion internship`
   - Predicted：`I don't know`
   - 诊断：事实存在于 store，rerank 臂直接答对。
2. `conv-2-q-69`
   - Gold：`old car`
   - Predicted：`I don't know`
   - 诊断：force-answer 臂答对 `Her old car`。
3. `conv-8-q-96`
   - Gold：`strength and resilience`
   - Predicted：`I don't know`
   - 诊断：事实存在于 store，rerank 臂答对。
4. `conv-0-q-140`
   - Gold：`"Trans Lives Matter"`
   - Predicted：`I don't know`
   - 诊断：文字只在海报图像中，属于合理弃答背后的模态缺口。
5. `conv-7-q-93`
   - Gold：`developing renewable energy finding ways to supply clean water to those with limited access`
   - Predicted：`I don't know`
   - 诊断：rerank 和 force-answer 均完整答对。

现有 force-answer 结果救回 8/26 道 single-hop IDK，但全局强答也制造了新的错误。

**最高 ROI 修法：** 答题侧只对 IDK 做有证据的定向 retry，不宜无条件开启全局强答。

### (e) 事实性幻觉

该类按严格定义只有一题：

1. `conv-0-q-147`
   - Gold：`Grateful and thankful for her family`
   - Predicted：`wrecked`
   - 诊断：原文只说 scared、珍惜和感谢家人，没有“她感到 wrecked”的事实。

**最高 ROI 修法：** 答题侧要求情绪词必须能回指原句，禁止把事故结果迁移成人物感受。

## Multi-hop

### 失败类型分布

| 类型 | 数量 | 错题占比 | 子类型/说明 |
|---|---:|---:|---|
| (a) 检索未召回 | 18 | 45.0% | 排序 17；模态/抽取丢失 1 |
| (b) 已召回但推理错 | 1 | 2.5% | 条件过滤失败 |
| (c) gold/判分口径 | 9 | 22.5% | 实体、计数去重或等价答案问题 |
| (d) IDK | 9 | 22.5% | 字面包含 `I don't know` |
| (e) 事实性幻觉 | 3 | 7.5% | 说话人或事实状态错绑 |
| **合计** | **40** | **100%** | |

### (a) 检索未召回

1. `conv-3-q-61`
   - Gold：`Gamecube, PC, Playstation.`
   - Predicted：`video games, CS:GO`
   - 诊断：设备类型由图像承载，库中只保留 PC/游戏文本，属于模态丢失。
2. `conv-3-q-78`
   - Gold：`nine`
   - Predicted：`6`
   - 诊断：九次参赛跨多个 session，回答只聚合到部分事件。
3. `conv-4-q-17`
   - Gold：`6`
   - Predicted：`1`
   - 诊断：完整对话中六场胜利存在；gold evidence 列还漏了一条，但当前回答的召回明显不全。
4. `conv-4-q-30`
   - Gold：`Seattle, Chicago, New York, and Paris.`
   - Predicted：`Edinburgh, Italy`
   - 诊断：回答返回另一人物的城市；rerank 臂答对。
5. `conv-9-q-63`
   - Gold：`custom-made yellow guitar with an octopus on it, shiny purple guitar`
   - Predicted：`custom guitar made by his Japanese artist friend`
   - 诊断：只聚合到其中一把吉他；rerank 臂答对。

**最高 ROI 修法：** 检索侧做跨 session 的事件多样性召回；17/18 都是排序或覆盖问题，
抽取不是 multi-hop 的主矛盾。

### (b) 已召回但推理错误

该类只有一题：

1. `conv-6-q-20`
   - Gold：`two`
   - Predicted：`3`
   - 诊断：只有 `D10`、`D29` 是 charity tournament；回答把 `D27` 的普通 online
     competition 也计入。

**最高 ROI 修法：** 答题侧先枚举候选事件，再按 charity 条件过滤和去重。

### (c) Gold 或判分口径问题

1. `conv-2-q-18`
   - Gold：`Rob`
   - Predicted：`a colleague`
   - 诊断：Rob 原文就是该 colleague，预测正确但不够具体。
2. `conv-3-q-52`
   - Gold：`two`
   - Predicted：`3`
   - 诊断：完整原文明确说 `this is the third time`，预测实际上正确。
3. `conv-5-q-17`
   - Gold：`three times`
   - Predicted：`4`
   - 诊断：同一个未来 hike 在多轮被重复确认，“计划几次”的事件去重口径不明确。
4. `conv-8-q-11`
   - Gold：`Weight problem`
   - Predicted：`gastritis scare`
   - 诊断：两者都被明确描述为促使 Sam 改变生活方式的 wake-up call。
5. `conv-9-q-47`
   - Gold：`two`
   - Predicted：`3`
   - 诊断：除两次近期车展外，Dave 还明确提到 10 岁第一次车展，预测正确。

**最高 ROI 修法：** 评测侧修复完整对话与 evidence/gold 的一致性，并明确“事件次数”的
去重规则。

### (d) IDK 弃答

1. `conv-1-q-9`
   - Gold：`Rome`
   - Predicted：`I don't know`
   - 诊断：rerank 和 force-answer 都答对。
2. `conv-4-q-38`
   - Gold：`UK`
   - Predicted：`I don't know`
   - 诊断：rerank 和 force-answer 都答对。
3. `conv-5-q-23`
   - Gold：`Finding the right dog and pet-friendly apartments close to open spaces`
   - Predicted：`I don't know`
   - 诊断：rerank 能回答住房主干。
4. `conv-5-q-56`
   - Gold：`No`
   - Predicted：`I don't know`
   - 诊断：rerank 和 force-answer 都答对。
5. `conv-7-q-77`
   - Gold：`Phuket`
   - Predicted：`I don't know`
   - 诊断：地点与潜水事件分布在两个 session，未完成桥接。

现有 force-answer 结果救回 3/9 道 multi-hop IDK。

**最高 ROI 修法：** 答题侧只对 IDK 做一次有约束的跨 session 强制归纳。

### (e) 事实性幻觉

1. `conv-3-q-1`
   - Gold：`Watching movies, making desserts`
   - Predicted：`writing`
   - 诊断：Nate 后文明确说 `I'm no writer like you`，写作不是共同兴趣。
2. `conv-8-q-82`
   - Gold：`Unhealthy snacks, sweets, yoga, places with beautiful views`
   - Predicted：`painting`
   - 诊断：把 Evan 的稳定减压方式及给 Sam 的建议升级成 Sam 已有事实。
3. `conv-9-q-54`
   - Gold：`Calvin stays connected to the creative process by always staying up-to-date on world events and watching documentaries about artists.`
   - Predicted：`writing lyrics and notes`
   - 诊断：该动作原文属于 Dave，却被错误归给 Calvin。

**最高 ROI 修法：** 答题侧强化 speaker/entity 绑定，并区分“建议/考虑”与“已经发生”。

## 跨类别 ROI 排序

### 1. 检索侧：先消除运行退化，再处理排序

当前 rerank 在目标两类中：

- 救回 single-hop 36 题、multi-hop 15 题；
- 同时弄错原本正确的 single-hop 32 题、multi-hop 13 题；
- 目标两类净增仅 6 题，而且 overall 从 83.70% 降到 83.64%。

这说明剩余错误对上下文排序高度敏感，但当前 rerank 是高翻转、低净收益，不能直接作为
修法采纳。应先稳定本地 embedding 查询，并把逐题 retrieval trace 持久化，之后再针对
竞争事实、跨 session 多样性和事件覆盖做可归因优化。
（embedding 查询稳定化已落地：bench 侧 retry 包装器，见「证据边界」修复注；
逐题 retrieval trace 由 009 US1 归因 trace 承接。）

### 2. 抽取侧：补齐图像 caption

single-hop 中至少 14 道主因错误来自图像细节完全没有进入 memory；另有多道 IDK 也只
缺图像粒度。杯子、海报、食物、画作、照片内容等题形成了高度确定、可批量处理的失败簇。

> 🔧 **机制已落地,收益未声明(2026-07-23)**:`--image-captions` flag(默认关,
> adapter-only,引擎零改)——解析层把每 turn 的 `blip_caption` 折进文本
> (`[shares a photo: …]`),抽取与 verbatim chunk 两条摄入路自动受益;关闭时逐字节
> 等旧行为。locomo10 实测 1226/5882 turn(20.8%)带 caption。**生效需重建抽取店 +
> 端到端 A/B(宪法 IV)才可声明分数**;排序约束:须在 009 归因(原始 008 store)
> 之后再建 caption 店,避免污染同源基线。

### 3. 评测侧：修复 gold、evidence 与 judge

两类共有 29/152 道错题属于 gold、实体绑定、等价答案或计数去重口径问题，占目标错题
的 19.1%。这部分不应通过检索或生成算法“迎合错误 gold”。

### 4. 答题侧：只做定向 IDK retry

force-answer 从 35 道目标类 IDK 中救回 11 道，但也会在其他题引入长篇猜测和新错误。
更合适的方式是只对 IDK 触发一次短答案、证据约束的 retry，而不是全局强制回答。

## 最终结论

- **single-hop：** 最大实际杠杆是检索排序；最确定的批量修复是图像 caption 入库；
  IDK 应做定向 retry。
- **multi-hop：** 主矛盾是跨 session 事件覆盖，不是复杂推理能力；只有 1/40 是明确的
  “证据齐全但条件推理仍错”。
- **评测可靠性：** 29 道 gold/口径题足以显著影响与 MemOS 的 pp 差距，必须与产品能力
  错误分开统计。
- **当前 rerank：** 能证明排序敏感性，但净收益太低，不能直接采纳。
