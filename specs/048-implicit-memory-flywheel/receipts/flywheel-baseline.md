# T054 dev-comparison baseline receipt（flywheel-baseline）

- 日期：2026-09-04（UTC，WSL2 本机）
- 数据集：core172（dev-regression，sealed copy 于 series root `datasets/core172/`）
- 系列：`t054-dev-baseline`，purpose `dev-comparison`
- Series root：`~/.engram-eval-048/t054/series/`

## Frozen identities

| 项 | 值（短 digest） |
|---|---|
| series manifest | `025f4fcae01a765311bdb5a6…` |
| skill snapshot（T018 pre-revision） | `f993c9780bc928f0b653db42…`（`snap-910fb87800415a2f5f7`） |
| core execution plan | `d98744959c506ae6a608f994…` |
| runner | `6f43f507822fdf27…`（rev `runner-6f43f507822f`） |
| judge rule | `6e1252a7a3ae936d52560943…` |
| case_set_digest（全腿一致） | `79cbf0c6069f133f…` |
| timeout / concurrency | 240 s / 8 workers（bounded pool，overlap 观测为真） |

## Legs（binary per-case，零 runner-error）

| host × ordinal | run_digest | seal_digest | case_order_digest | 总分 | 状态 |
|---|---|---|---|---|---|
| claude o1 | `c248f0df0a841dae…` | `3276bb6b2b264cc0…` | `3d8eeac7e22001af…` | 90.7%（156/172） | complete |
| codex o1 | `ae1273c31c4a88df…` | `6b7d78c2ccfa3501…` | `3d8eeac7e22001af…` | 83.7%（144/172） | complete |
| codex o2 | `168a39ff76ad7711…` | `f57afa56aae4d6ec…` | `b940824cf9d699c8…` | 82.6%（142/172） | complete |
| codex o3 | `c08638182a682681…` | `1cdda235c89e33b3…` | `6f167225fe868bdd…` | 85.4%（147/172） | complete |
| opencode o1 | `1ecb5bbfc7882016…` | `a839dc1cacf01d22…` | `3d8eeac7e22001af…` | — | 作废（CLI 自动升级窗口，维护者裁定） |

opencode o2/o3：**未跑**（维护者指示"opencode不要跑了"）。

## Per-module（binary，%）

| 模块 | claude o1 | codex o1 | codex o2 | codex o3 | codex median |
|---|---|---|---|---|---|
| ir-pos | 82.1 | 100.0 | 96.4 | 92.9 | 96.4 |
| ir-neg | 96.4 | 82.1 | 82.1 | 89.3 | 82.1 |
| iw-pos | 89.3 | **39.3** | **35.7** | **46.4** | **39.3** |
| iw-neg | 100.0 | 100.0 | 100.0 | 100.0 | 100.0 |
| reg | **81.2** | 84.4 | 87.5 | 90.3 | 87.5 |
| tr-pos | 94.4 | 100.0 | 88.9 | 88.9 | 88.9 |
| tr-rneg | 100.0 | 75.0 | 100.0 | 100.0 | 100.0 |
| tr-wneg | 100.0 | 100.0 | 100.0 | 100.0 | 100.0 |
| **总分** | **90.7** | 83.7 | 82.6 | 85.4 | **83.7** |

## 失败归档（分类全量，机器 seal 版 pending）

codex iw-pos 三轮 50 个失败点全量解剖（方法：raw.jsonl 逐事件重放）：

- retry-after-error ×24（48%）：首写被 schema 整单拒绝后自修重发成功。错误构成
  `pinned` 字符串化 ×27、trigger 超 120 码点 ×7、未文档化字段 ×1。
- upsert-refine ×20（40%）：同 name 渐进精化重写（store 实际 upsert，1 条收敛）。
- multi-entry-writes ×6（12%）：multi-fact 分写（iw-pos-013/006/024；dataset
  `observable` 与 `max_calls:1` 自相矛盾）。

claude 短板（未靶向，正交）：reg 81.2%（已召回答错）、ir-pos 82.1%。
完整定案与 T055 v0.2.9 修订记录：[failbook.md T054 节](../failbook.md)。

## 费用（实付，牌价 input ¥0.8/1M · cache-read ¥0.1/1M · output ¥2.7/1M）

| 腿 | 费用 |
|---|---|
| claude o1（result usage 口径） | ¥8.23 |
| codex o1/o2/o3（turn.completed，cached 已扣） | ¥5.17 × 3 ≈ ¥15.51 |
| **本轮有效腿合计** | **≈ ¥23.7** |

（早期作废轮费用另计，未含在本表。）

## Pending（封盘前置）

`failure-archive` / `compare` 要求完整 host × 3 ordinal 矩阵且 plan 硬编码三 host
（`formalLaneConfig` Lanes / `measuredToolIdentities`）。opencode 停跑后矩阵不完整，
机器 sealed archive 构造不出。两条路（维护者决策）：

1. **2-host 重规划**：runner 改 host 集可配 → 新 runner digest → 新 plan + 新
   series → 重跑 claude+codex 6 腿（≈¥40）。彻底移除自动升级的 flaky CLI，
   T058 holdout 同步受益。
2. **pin 版本补跑 opencode o2/o3**（≈¥10-20，有再遇升级窗口风险）：保持三 host
   契约与既有 4 腿投资，但 flaky host 进入正式分数族。

在决策落地前，本 receipt 的 binary per-case 中位数表即为 T055 修订效果比对的
工作基线；机器 seal 的 fail-to-pass 集合待矩阵补全后由 `failure-archive` + `compare`
产出（SC-5 判定用）。
