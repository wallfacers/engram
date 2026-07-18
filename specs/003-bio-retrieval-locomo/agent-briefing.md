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
