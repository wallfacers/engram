# 错题本（failbook）— engram skill 触发飞轮

每圈失败逐条归因,是下一圈 skill 修订的唯一输入。纪律:**不全量补跑**(限流/超时类失败单独补),修复后失败例回填数据集(只增不减,`source: flywheel-round-N`)。root cause 词表:`contract-too-narrow`(契约没覆盖)/`contract-wording`(措辞歧义)/`dataset-semantics`(数据集语义边界,非 skill 缺陷)/`rate-limit-suspected`(订阅限流/超时,先单个补跑)/`host-memory-competition`(宿主自带记忆竞争)/`eval-env-retrieval`(评测环境检索能力,非 skill 缺陷)/`host-behavior`(宿主特有)。

## Round 1 · 2026-08-29 · v0.1.0 → v0.2.0

环境:claude 2.1.251(GLM glm_w)/ codex 0.150.1(tf --yolo)/ opencode2 beta(Console 免费档);concurrency 6,timeout 200s。

### 基线(v0.1.0 显式契约)成绩

| 模块 | claude | codex |
|---|---|---|
| implicit-write-pos | **0/26** | 13/26 |
| implicit-write-neg | 25/26 | 26/26 |
| implicit-read-pos | 6/24 | 10/24 |
| implicit-read-neg | 23/24 | 24/24 |
| regression(020) | 23/32 | 25/32 |

opencode:9 例后全败于 **provider rate-limit**(免费 Console 限流)——`provider.rate-limit` 错误字样,runner 判 RUNNER-UNAVAILABLE。**限流≠能力失败**,已单独串行补跑,不计入通过率分母的证据链以补跑结果为准。

### 基线失败根因(→ 驱动 v0.2.0 修订)

| 现象 | root cause | 处置 |
|---|---|---|
| write-pos claude 0/26、codex 13/26 | `contract-too-narrow`:description 只锚显式措辞 | v0.2.0 三通道契约 |
| claude 对 force-push 禁令等场景"已记下"但写进**宿主 auto-memory** | `host-memory-competition`:模型用宿主记忆兜底,用户以为记住了 | v0.2.0 隐式写契约;评测侧 per-run cwd 隔离 |
| opencode 把虚构 M2/macOS 写进 repo AGENTS.md/constitution.md(已回滚备份) | 同上,写错位置的极端形态(直接改项目文档) | 同上 + 跑后必查 `git status` |

### v0.2.0 修订后成绩与剩余失败

| 模块 | claude | codex |
|---|---|---|
| implicit-write-pos | 5/26 | **24/26** |
| implicit-write-neg | 23/26 | 25/26 |
| implicit-read-pos | 7/24 | 7/24 |
| implicit-read-neg | 21/24 | 24/24 |
| regression | 24/32 | 25/32 |

(claude 数字受 auto-memory 污染,见下;补跑后以补跑为准)

| 剩余失败 | 例数 | root cause | Round-1 处置 | 状态 |
|---|---|---|---|---|
| claude write-pos false-negative | 21 | `host-memory-competition`:基线跑把事实写进了 claude 按 cwd 持久的 auto-memory,修订版跑时模型"发现自己记忆已有,无需重复保存"→ 不写 engram | runner 改 per-run 独立 cwd + 清残留;`--only` 补跑 46 例 | 补跑中 |
| codex read-pos wrong-report(检索空但如实) | 9 | `eval-env-retrieval`:中文 query 搜不到英文 seed(离线 keyword+entity 无 embedding,跨语言 miss)→ 空结果如实报,判分要求"查到并用"判 fail | seed 双语化(dataset v2);补跑 | 待补跑 |
| codex/claude read-pos false-negative | 8+14 | 同上(检索空后部分模型不调或调了空) + 部分 `contract-wording`(read 触发句不够强) | seed v2;必要时 round-2 强化 §0 read 措辞 | 待补跑 |
| codex read-pos 编答案(snake_case 猜成 config_parser.py 等) | ~2 | `contract-wording`:§5"如实报空"未被稳定遵守 | round-2 观察 | 开放 |
| regression false-negative(显式正例无调用) | 5-6/工具 | `dataset-semantics`:"after I confirm"类措辞模型正确等待确认但判分要求立即调;间接持久化措辞("keep this available later")触发弱;CLI-only intents(stats/export)模型只说不做 | 020 冻结集不改;round-2 在 SKILL.md §4 强化"CLI-only 直接执行" | 开放 |
| neg false-positive | codex 2、claude ~5 | v0.2.0 扩契约的代价,均 <10% 门内;逐例见 failures.jsonl | 观察 | 开放 |
| timeout/failed | claude 12、codex 1 | `rate-limit-suspected`(GLM 偶发慢/限流) | 单个补跑 | 补跑中 |
| opencode 全部 | — | `rate-limit-suspected`(Console 免费档限流,6 并发打爆) | 串行 concurrency 1 补跑;后续圈 opencode 默认并发 1-2 | 补跑中 |

### Round-1 器材/方法学修复(非 skill 文本)

1. opencode2 v2 MCP 项目配置 schema = `mcp.servers.<name>`(v1 `mcp.<name>` 静默失效)。
2. opencode 后台 service 是串行化单点,timeout kill 客户端不杀 service → worker 池卡死;改 `--standalone`。
3. opencode Console 免费档并发 >2 即限流 → 该工具并发 1。
4. claude auto-memory 按 cwd 持久,跨 run 泄漏 → per-run cwd。
5. 评测 agent 会逃逸写盘(repo 文档/根目录二进制) → 每 run 后 `git status` 巡检+回滚。
6. runner 新增 `--only <ids>` 单例补跑 + `--sample N` 等距抽样防回归(替代全量,省 token)。

### Round-2 候选(skill 文本层)

- §0 read 触发句强化:"depends on remembered facts"具体化(问"我的/我们之前的/老规矩"等指代前情的问法)。
- §4 CLI-only intents"直接执行,不要只描述"。
- 观察 neg 误触发逐例,必要时在 description 排除项再收口。

## Round 2 · 2026-08-29 · v0.2.0 read 侧修复(补跑圈)

修订:SKILL.md §0 read 触发句具体化 + **短关键词 query 指导**(2-6 词,不用整句;空结果先用最独特单词重试一次);mcp.md/cli.md 同步。触发根因:引擎 keyword 检索对长混合 query(48 code points 中英混串)返回 0(CLI 复现坐实,`search "pnpm"` 命中而整句空)——引擎行为按宪法 II 不在 skill feature 内修,skill 层用 query 构造指导绕过;**上报维护者:长 query 检索空是引擎候选增量**。

器材修复(本轮踩坑三连,全入 runner/手册):
1. **两个 run 并发共享同一 `--scratch` 会互删 store**(round-2/2b 秒灭根因)——手册铁律:同 scratch 串行,并发用独立 scratch。
2. **并发 seed add 撞 SQLite 单写者**(read-pos 集全带 seed,4 并发必撞)——seed 重试 4 次×递增退避。
3. unavailable 时 verdicts 被丢弃(诊断黑箱)——保留 partial verdicts。
4. per-run 空 cwd 弄坏"装依赖"类 case 语义——cwd 放最小工作区模板(README/package.json/main.go)。

结果(read-pos 失败例 --only 补跑 + 6 抽样防回归):

| | claude | codex |
|---|---|---|
| read-pos 补跑 | 11/20 | 10/20 |
| read-pos 合并(v0.2.0 全量+补跑) | **15/24 ≈ 62%**(基线 25%) | **12/24 = 50%**(基线 42%) |
| 抽样回归(neg/write/regression) | 4/4 ✅ | 4/4 ✅ |

剩余失败 20 = 14 false-negative(仍未检索) + 6 wrong-report(判分词表边界:如川菜推荐合理避开花生但未说"花生"字样)。**write 侧终态**:codex 96%、claude 73%(auto-memory 竞争修复后);**neg 误触发**:claude ~4%、codex ~2%。

opencode:免费 Console 档限流窗口内串行也 3 连败;替代 provider 全堵(Alibaba stored key 401、DASHSCOPE key 401、GitHub Copilot 400)——本轮以 13 例有效数据标注"限流受限"如实入档,全量留待维护者提供有效 provider 或限流窗口外慢跑。

### Round-3 候选

- read false-negative 14 例:§0 read 触发句再强化(枚举"哪个/什么/什么时候 + 我的/我们的"问式);或 description 的 implicit-read 句加"任务涉及用户偏好/约定时先查"。
- wrong-report 6 例:判分词表按答案实际表述扩(如推荐类答案"避开花生类菜品"变体);部分或为真失败(未把约束应用于输出),逐例看 raw。
- claude write 73%→90%:GLM flash 对"吐槽式透露"(iw-pos-002 类)最弱,§0 write 类目词再加"complaints about repeated mistakes"。
- regression 层 dataset-semantics 5-6 例:020 冻结集不动;SKILL.md §4 加"CLI-only intents 直接执行"。

## Round 3 · 2026-08-29 · 模型矩阵 + query 构造修复(v0.2.0 → v0.2.1)

### 器材升级(runner,判分口径变化需注明)

1. **per-case 独立 store**(`<scratch>/data/<label>/<case-id>`):消灭共享 store 跨 case seed 污染——iw-pos-024 曾因 read case 的相同 seed 被误判 false-negative(模型 list 后正确去重);同时消灭 SQLite 单写者争用。旧共享 store 数字与 per-case 数字不完全可比(wrong-op 类在共享 store 下会被别 case 的写入"救活")。
2. **runner 自动生成 opencode.json**(修 wipe-吃配置 bug)+ **claude 每 case 独立 cwd**(auto-memory 隔离从 per-run 收紧到 per-case)。
3. **`tool@variant` 语法**:`codex@ds`/`codex@qwen`(Profile V2 → 阿里 MaaS 实例,`wire_api="responses"`,vLLM Responses API 实测可用;codex 0.150 已砍 `wire_api="chat"`)、`claude@opus`(variant=claude 模型槽位,经 settings 映射到 GLM 非 flash)。
4. 双模型 MaaS 探测:deepseek-v4-flash-0731 / qwen3.8-flash 默认开思考,`enable_thinking:false` 可关;矩阵跑保持默认开(测集成者开箱行为)。

### 矩阵圈 A2 · claude host 模型档位(GLM flash vs GLM 非 flash)· v0.2.0 冻结 skill + per-case store

| (v0.2.0, 50 隐式正例+8 抽样) | glm-5.3-flash | glm-5.3(opus 槽) |
|---|---|---|
| implicit-write-pos | 17/26 = 65% | 18/26 = 69% |
| implicit-read-pos | 15/24 = 63% | 16/24 = 67% |
| neg 误触发(抽样) | 0/8 | 0/8 |

**结论(回答"claude 为什么双低"):换一整档模型只值 +4pp(噪声级)——claude 双低不是 flash 容量问题,是 skill 触发文本对 GLM 系不够显式**。gpt-5.6(codex 同 skill 写入 96%)证明模型家族间确有遵循差异,但 GLM 档内升级无效;提分杠杆在 skill 文本与 query 指导,不在换档。模型槽位经 raw 内 `"model"` 字段验证确已生效。

### wrong-report 4 例逐 raw 定性(推翻 Round-2"判分词表边界"假设)

| case | raw 真相 | root cause |
|---|---|---|
| ir-pos-011 花生 | 检索 query 用话题词"川菜 外卖 口味偏好"而非约束词(过敏/忌口)→ 未命中 seed → 推荐未避花生 | `query-wording`:缺约束词指导 |
| ir-pos-012 CET/Berlin | query"会议时间偏好 日程"缺 时区/Berlin → 未命中 → 给了无时区锚的候选时段 | 同上 |
| ir-pos-014 snake_case | query 7 词超长 → 引擎长混合 query 返回 0;且空结果后未按指导做单词重试 | `query-length` + 未重试 |
| ir-pos-009 M2 | 检索后转头扫真实环境,用 WSL2 覆盖了记忆答案 | `env-over-memory` |

判分器**未改**——4 例皆真失败(非词表缺口),避免为过门放水。

### v0.2.1 修订(据此)

SKILL.md:description 加问式枚举/约束计划/动作触发 + complaint 类目(压到 1006 码点,<1024 门);§0 write 加"吐槽/纠正透露的常设规则";§0 read 重写为 (a)问式 (b)约束计划 (c)动作前三查 + **query 用约束词不用话题词、2–4 短词、空结果必须单词重试、记忆值优先于现场环境**;§4 加 CLI-only 直接执行;§5 加记忆值不被现场覆盖。contract.json 0.2.1(triggers 同步);mcp.md/cli.md query 指导同步 2–4+约束词。数据集 v3:write/read 各 28/28(+2 complaint pos、+2 pos 各类、+4 neg 各类,source: flywheel-round-3,合计 144)。


## 模板(每圈复制)

```
## Round N · 日期 · vX.Y.Z → vA.B.C
### 成绩(基线/修订 × 模块 × 工具)
### 失败逐条:case_id | 工具 | 类型 | root cause | 处置 | 状态
### 器材修复 / Round-N+1 候选
```
