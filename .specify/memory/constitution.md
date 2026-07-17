<!--
Sync Impact Report
==================
Version change: (template, unversioned) → 1.0.0
Bump rationale: MAJOR — initial ratification of the engram constitution; first
concrete principles adopted in place of template placeholders.

Modified principles: (none — first adoption)
Added sections:
  - Core Principles I–V
  - 技术约束 (Technology Constraints)
  - 开发工作流与质量门禁 (Development Workflow & Quality Gates)
  - Governance
Removed sections: (none)

Templates & runtime guidance reviewed:
  - .specify/templates/plan-template.md ...... ✅ compatible (Constitution Check
    gate is placeholder-driven; no change required)
  - .specify/templates/spec-template.md ...... ✅ compatible (no mandatory
    sections added/removed by this constitution)
  - .specify/templates/tasks-template.md ..... ✅ compatible (bench-gate &
    TDD map onto existing Polish / test phases)
  - .claude/skills/speckit-*/SKILL.md ........ ✅ no principle references to sync
  - README.md / docs/*.md .................... ✅ consistent with principles

Deferred TODOs: (none)
-->

# engram Constitution

engram 是可嵌入任意智能体的本地优先记忆层:一套记忆引擎,三个薄集成面
(MCP server / CLI·skill / API SDK)。本宪法定义所有 spec、plan、tasks、
实现与评审都必须遵守的不可协商原则。

## Core Principles

### I. 本地优先,默认离线 (Local-First, Offline by Default)

engram 的核心功能路径 MUST 在无外网、无云依赖的环境下完整运行。存储 MUST 为
本地文件(SQLite),embedding 与 LLM 调用 MUST 走可替换的本地 sidecar
(如 Ollama / fastembed),不得把任一托管服务设为运行必需项。任何要求联网或
私有云才能工作的能力 MUST 是显式可选、默认关闭的增强项,而非默认路径。

*理由*:合规定位(金融/医疗/政企私有化)与"错位竞争、不抢宿主"的产品叙事
都建立在离线可运行之上;一旦默认路径隐含云依赖,该定位即失效。

### II. 引擎与适配层分离 (Engine/Adapter Separation)

记忆引擎(存储、混合检索、抽取、curation、embedding 客户端、评测 harness)
MUST 是独立、可单测的库,不得依赖任何宿主(workhorse-agent、Codex、Claude
Code、Cursor 等)的类型或运行时。宿主特定逻辑 MUST 只存在于薄适配层
(MCP / CLI / SDK);适配层之间不得共享私有状态,只通过引擎的公开 API 交互。
新增一个集成面 MUST NOT 要求修改引擎内部契约。

*理由*:"单点投入、多处复用"是抽离的全部战略价值;引擎一旦渗入宿主耦合,
三个集成面就退化成三份分叉,抽离白做。

### III. 契约优先与命名空间隔离 (Contract-First & Namespace Isolation)

每个能力 MUST 先在 spec/plan 中定稳定的对外契约(API 形状、错误语义、数据
schema),再写实现;契约的破坏性变更 MUST 走版本号 MAJOR 提升并附迁移说明。
所有记忆读写 MUST 以 namespace 隔离不同宿主/用户,跨 namespace 访问 MUST 是
显式、经授权的操作,默认不可见。

*理由*:三个集成面共享同一存储,缺乏隔离即数据串味;契约不先定,适配层与
引擎会并行漂移,回归无从谈起。

### IV. 评测回归门禁 (Evaluation Regression Gate) — NON-NEGOTIABLE

`cmd/locomo-bench` 既是产品回归测试也是论文实验基础设施。任何触及检索、抽取、
curation、存储或 embedding 的变更,在合并前 MUST 跑可比口径的评测,并 MUST NOT
使既定基准指标(LoCoMo 可答题均值等)相对当前基线显著回退。若变更有意改变
指标,PR MUST 明确声明新基线与理由。评测口径(数据集、prompt、融合参数)
的改动 MUST 与算法改动分开提交,以保证可归因。

*理由*:记忆质量是本项目唯一的差异化护城河(已经五轮消融换来),没有守门的
评测,任何"顺手优化"都可能悄悄抹掉调优成果。

### V. 优雅降级与规模诚实 (Graceful Degradation & Honest Scale)

多信号能力(语义 + BM25 + 实体 RRF 等)MUST 在任一信号缺失/失败时独立优雅
降级,而非整体报错。系统的规模与性能边界 MUST 在文档中如实声明(当前:单用户
~10 万条级记忆,不承诺千万 token 语料),MUST NOT 用未验证的规模宣称掩盖已知
限制;超边界的能力(ANN/量化等)MUST 作为明确的后续工作而非隐性承诺。

*理由*:可信度即产品资产;一次夸大的规模宣称或一个不降级的硬失败,足以摧毁
本地记忆层在生产宿主中的信任。

## 技术约束 (Technology Constraints)

- **无 CGO**:引擎 MUST 保持纯 Go 可交叉编译,SQLite 使用 `modernc.org/sqlite`,
  不得引入需要 C 工具链的依赖到核心路径。
- **依赖最小化**:新增第三方依赖 MUST 有明确理由并优先选择可离线、可审计者;
  能用标准库解决的不引依赖。
- **可替换的模型侧**:embedding / LLM 提供方 MUST 通过接口隔离,支持替换本地
  实现,不得把单一 provider 硬编码进引擎。
- **单一存储真相**:三个集成面 MUST 共享同一 SQLite 存储与 schema,不得各自
  维护平行数据副本。

## 开发工作流与质量门禁 (Development Workflow & Quality Gates)

- **spec-kit 驱动**:每个能力 MUST 走 `constitution → specify → plan → tasks →
  implement` 流程;跳过 spec/plan 直接实现的改动不予合并(除文档与琐碎修复)。
- **测试先行**:涉及引擎行为的实现 MUST 先有可失败的测试(契约测试/集成测试),
  再写实现;引擎库 MUST 可在无宿主、无外网下单测。
- **评审合规检查**:每个 PR 的评审 MUST 显式核对本宪法五项原则;违背原则的
  复杂度 MUST 在 plan 的 Complexity Tracking 中记录理由,否则采用更简方案。
- **门禁不可绕过**:评测回归门禁(原则 IV)与离线可运行(原则 I)是硬门禁,
  CI 或评审发现违背时 MUST 阻断合并。

## Governance

本宪法 MUST 优先于其它一切开发实践与惯例;冲突时以本宪法为准。

- **修订流程**:修订 MUST 以 PR 形式提出,写明变更内容、版本升降理由与受影响的
  模板/文档,经维护者批准后合并。
- **版本策略**:采用语义化版本。MAJOR = 移除/重定义原则或做出向后不兼容的治理
  变更;MINOR = 新增原则/章节或实质性扩充指引;PATCH = 措辞澄清、错别字、
  非语义精修。
- **合规审查**:所有 PR 与评审 MUST 验证与本宪法的一致性;发现漂移 MUST 在合并
  前修正或在 Complexity Tracking 中记录并证成。
- **运行时指引**:日常开发的具体指引以 `README.md` 与 `docs/` 为准,二者
  MUST 与本宪法保持一致,冲突以本宪法为准。

**Version**: 1.0.0 | **Ratified**: 2026-07-18 | **Last Amended**: 2026-07-18
