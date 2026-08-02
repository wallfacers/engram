# 服务器租用规格清单（023 训练 + 评测）

**用途**：Qwen2.5-7B-Instruct LoRA/QLoRA 训练（supervised 臂）+ 本地 sidecar 推理 + 三臂配对
评测。参考 022/026 的 AutoDL 先例（remote-eval-box 纪律 + 磁盘卫生）。

## 必选规格

| 项 | 规格 | 理由 |
|---|---|---|
| **GPU** | 单卡 **24 GiB**（首选 RTX 4090 24G；3090 24G 备选；A10G 24G 吞吐低不建议） | 7B BF16 权重 ~15 GiB + LoRA 训练/QLoRA 余量；4090 推理吞吐足够 p95≤2s |
| **CPU** | ≥8 核（建议 12–16 核） | 数据预处理、vllm 调度、合成对话生成 |
| **内存** | ≥48 GB（建议 64 GB） | 语料加载、索引构建、并行数据 pipeline |
| **系统盘** | ≥40 GB（AutoDL 实际 ~30G，只放代码/工具） | 系统盘小而贵，纪律：只放 engram 代码 + 编译工具 |
| **数据盘** | `/root/autodl-tmp`，建议预留 **≥100 GB** | 权重（~15G）、HF 缓存、venv、训练数据、run 目录全放这里 |
| **网络** | 可访问 HF Hub（模型/数据集） | 拉 Qwen2.5-7B-Instruct + OASST1/ultrachat_200k；AutoDL 有学术加速 |

## 磁盘布局（硬纪律）

```text
/root/autodl-tmp/
├── 023-runs/            # 数据构建 + 训练产物 + 日志 + eval 结果（主工作区）
├── eval-backup-<ts>/    # 删 run 前备份小结果（*.json/*.log/*.sh + store/）
└── ...                  # 模型缓存、venv 也放数据盘
```

- 跑前 `df -h /`；系统盘 >80% 先清（`.cache/`、`NNN-runs/`、`miniconda3/pkgs/`）。
- **永远不要**把 run 目录放 `/root`（022/024/025/026 曾堆积 22.4G 在系统盘）。

## 训练环境（T014）

```bash
# venv + 依赖（放数据盘）
python -m venv /root/autodl-tmp/023-venv
/root/autodl-tmp/023-venv/bin/pip install torch transformers peft trl datasets \
    accelerate bitsandbytes vllm  # QLoRA 需 bitsandbytes
```

- sidecar：vllm（合并 LoRA 后 serve，OpenAI 兼容）或 ollama；local_planner.go 走 provider 抽象。
- 模型权重、数据、venv、缓存放数据盘；代码副本（engram）在系统盘。

## 包时估算（≤ 24 GPU-hours 一次正式重建）

| 阶段 | 资源 | 估算 |
|---|---|---|
| 数据构建 + 审计（T008–T013） | CPU/内存 | 数小时（可在开发机预做，服务器只做训练相关） |
| 7B LoRA 训练（T015，几千样本、seq≤2048、单 epoch） | 1×4090 | **6–12 h** |
| LoRA 合并 + 冻结产物（T017） | 1×4090 | <1 h |
| 三臂配对评测（T019，LoCoMo 1540×3 臂 + vllm 推理） | 1×4090 + judge | 3–8 h（judge 走独立端点，见下） |
| 合计 | | **≤ 24 GPU-hours** |

**租法建议**：一次正式重建 ≈ 24 GPU-hours → RTX 4090 包 **2–3 天**（含重试/调试余量），或按
小时租。别整租空闲（省钱纪律——空闲必停）。

## Judge 端点（复用 remote-eval-box 模式）

- answer/extract 模型走租来的 GPU（vllm，OpenAI 兼容，近免费）。
- judge（deepseek-v4-flash 或既定 judge）走独立端点（`JUDGE_PROVIDER/BASE_URL/MODEL/API_KEY`），
  凭据经 env，不进 tracked 文件/日志/工具响应。
- 凭据（SSH host/port/password、API key）由维护者现场提供，只经 env/tunnel，绝不落盘进 repo。

## 验收清单（租机后）

- [ ] `nvidia-smi` 确认单卡 24 GiB；`df -h /` 与 `df -h /root/autodl-tmp` 确认磁盘布局。
- [ ] vllm/ollama 能 serve Qwen2.5-7B-Instruct（合并 LoRA 前先验证底模可跑，OpenAI 兼容端点通）。
- [ ] `CGO_ENABLED=0 go test -count=1 ./...` 在代码副本上绿（引擎测试）。
- [ ] HF 能拉 Qwen2.5-7B-Instruct（约 15 GB）与 OASST1/ultrachat_200k。
- [ ] T014 完成后跑 T015 训练冒烟（几十步）确认显存 ≤24 GiB、无 OOM。
