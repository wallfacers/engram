# Specification Quality Checklist: Memory-Specific Reranker Training

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-11
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- 本 spec 是**方向探索线**（对标 MemOS memos-reranker 服务线），核心命题是"记忆专用重排训练能否把召回提升转化为端到端答题"。
- 历史约束已内化：008 铁律（e2e 配对唯一 GO 门 + temporal 单独报告）、028 教训（ground-truth 监督为主 + 弱教师蒸馏上限）、023 教训（训练必有测量）、死亡规则边界（本地 default-off / SaaS 分数单列 / 推理端禁付费云重排）。
- **Prior art 已核实（alphaXiv，2026-08-11）**：MemReranker (2605.06132) = MemOS 自家 memos-reranker 研究，0.6B 基于 Qwen3-Reranker 在 LOCOMO 检索级 MAP 0.7150 vs BGE 0.6708（+4.4pp）、temporal 类 0.7811 vs 0.7489，直接验证"记忆专用训练赢检索"，但未测端到端 QA 配对（008 铁律的转化仍是本项目要测的独特问题）；配套文献：2507.08336（KD vs CL）、2604.23734（Prism evidence 输出）、2607.18152（jina v3.5 域特化 0.6B 打平 4B，non-commercial）、2602.02007（xMemory 诊断共鸣）。
- 唯一技术性措辞：FR-001 提到 OpenAI 兼容 `/v1/rerank` 与 `embedding.Reranker` 接口、FR-003 提到 Qwen3-Reranker-0.6B 基座——前者是项目既有能力边界（008 已用同一协议），后者是文献验证过的基座（MemReranker 同款），作为可测试的验收条件保留；引擎内部不可被改动（宪法 II）。
- **Clarify 会话（2026-08-11，3 问）**：Q1 评测协议 = 全量配对 GO 门 + 留出对话诊断；Q2 GPU/预算 = RTX 4090 24GB 单卡 ~1-3h、上限 ¥100/8 GPU·时；Q3 训练数据 = LoCoMo 为主 + MSC 等记忆数据集补充（非跑分求通用）、LME 500 作验证集。方向约束 = 成本驱动小模型替代贵云 reranker。详见 spec `## Clarifications`。
- **外部 review 修订（2026-08-11，逐条独立核验后内化）**：P0 收口（SC-005 默认栈口子收紧为 opt-in sidecar；`EMBED_RERANK_BASE_URL`→`EMBED_BASE_URL`、`--arm`→`--retrieval`（代码核实 main.go:1639,3058）；score equation 冻结 + 三方 parity；US1/US2 跨 run `--compare` 配对 + non-inferiority 预注册）；P1 收口（multi-positive schema——实测 423/1986 多 evidence；temporal 文本可判别性审计 R7——retriever 只传 Entry.Content；真实 conv ID conv-26..50 split；预算机器门禁 + hash manifest + preflight INVALID）。外部 review 一处数字有误（282 为 single-hop 非 multi-hop），已甄别不采信该数字。
