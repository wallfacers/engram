# 执行简报：feature 003 批次 1（T001–T013，测量地基 MVP）

> 本文件是发给执行 Agent 的任务简报存档。设计已冻结并经跨工件一致性分析
> （含 I1 single-pass 口径裁决），执行方只实现、不重开设计。评审收口由规划方负责。

## 批次划分

| 批次 | 任务 | 性质 | 状态 |
|------|------|------|------|
| 1 | T001–T013（Setup + Foundational + US1 + US2 代码/脚本） | 纯离线，零 API 花费 | **本批** |
| — | T014（Strike 0 评测） | 维护者付费执行 | 批次 1 review 通过后 |
| 2 | T015–T021（US3 联想检索开发） | 纯离线 | 后续简报 |
| 3+ | US4/US5 开发、各枪判定、LME_S 终态 | 依次 | 后续简报 |

## 完成定义（每任务）

`go build ./... && go vet ./... && go test ./...` 全绿 + `CGO_ENABLED=0 go build ./...`
通过 + `TestRetrievalParity` 保持绿 + tasks.md 勾选 `[x]` + 本地 commit。

## 汇报与阻塞协议

- 完成 T013 或被阻塞 → 停下输出汇报（逐任务状态/新增文件/测试输出尾部/遗留问题）。
- 设计文档矛盾或无法按契约实现 → 不改 specs/ 设计文档，记录到
  `specs/003-bio-retrieval-locomo/impl-notes.md`，跳过该任务继续无依赖任务。

---

# 执行简报：批次 2.5（Amendment 001 测量协议修正，纯离线零花费）

> 依据：Strike 0 方差诊断（specs/003-bio-retrieval-locomo/eval-log.md）。中转站
> 后端随时间窗漂移，跨目录 --compare 判定失效。契约见 contracts/bench-cli.md
> §2b/§2c。批次 2（US3）完成后立即做本批；完成后 Strike 1 才可开跑。

## 任务

- [ ] A1 `cmd/locomo-bench/main.go` `--retrieval` 支持逗号分隔多臂 + `+assoc` 等
      后缀（armsFor 扩展）；臂后缀覆盖全局机制开关；各臂共库、同窗交错答题判分
- [ ] A2 多臂运行落盘同窗 `paired.json`（契约同 compare.json + `"paired_in_process": true`）；
      stats.json 分臂输出保持现命名 results-<arm>.jsonl
- [ ] A3 `--force-answer` flag：answer/multi-hop/open-domain 三个 prompt 去掉
      "I don't know" 出口、要求必给最佳猜测；与 --abstain-prompt 互斥（启动时报错）；
      **口径改动独立 commit**（宪法 IV）
- [ ] A4 测试：armsFor 解析、后缀覆盖、paired.json 产出、互斥报错；既有测试保持绿

## 完成定义 / 汇报协议

同批次 1（go build/vet/test 全绿 + CGO_ENABLED=0 + TestRetrievalParity 绿 +
逐任务 commit）。阻塞或契约矛盾 → 记 impl-notes.md，不改 specs/ 设计文档。
