# 远端评测答题机 (remote eval box) — 运行手册

**用途**:LoCoMo/评测的**答题 + 抽取**大模型跑在一台租用的云 GPU 上(vllm,OpenAI 兼容),让全量评测**近免费**(答题/抽取本地化,只剩很小的判题 token)。这台机器**不属于 engram 交付物**——它只是评测基础设施;engram 本体永远 local-first、离线可跑,绝不依赖它。

> ⚠️ **省钱纪律(第一要义)**:这是**按时计费**的租用实例,**空闲必停**。维护者会在不评测时手动停机。任何"让它一直挂着"的做法都是烧钱。开跑前确认要用、跑完即停;别把它当常驻服务。

## 每次重启后会变的东西(必须现场重新拿)

停机再开(或换实例)后,以下**全部会变**,由维护者在会话开始时**当场提供**,**绝不写进任何被 git 追踪的文件、日志或工具响应**(密钥硬规):

| 变化项 | 说明 |
|---|---|
| SSH host / IP | 例如 `connect.xxx.seetacloud.com`(域名/IP 每次不同) |
| SSH 端口 | 例如 `-p 52988`(每次不同) |
| root 密码 | 每次不同;仅内存/隧道用,用完即弃 |
| vllm 显存/资源参数 | 换卡或换实例后 `--gpu-memory-utilization` / `--max-model-len` 可能要调 |
| **实例是否已在跑 vllm** | 全新实例上 vllm **不会自动起**——需重跑启动脚本 |

**持久盘通常保留**(AutoDL 系 `/root/autodl-tmp` 多为持久卷)→ 模型权重、启动脚本一般**还在**,但**进程不在**。所以标准动作 = 「SSH 进去 → 重跑 `serve-final.sh` → 等冷启动 → 本地重建隧道」。若持久盘也没了,则需重新下模型(见下)。

## 稳定不变的东西(脚本里写死,不用问维护者)

- **启动脚本**:`/root/autodl-tmp/serve-final.sh`
- **模型权重目录**:`/root/autodl-tmp/model`
- **对外模型名**(`--served-model-name`):`Qwen/Qwen3.6-35B-A3B-FP8`(MoE `qwen3_5_moe`,~35G FP8)
- **内部端口**:`8000`;**API key**:`local-eval`(仅本机/隧道内,非真凭据)
- **关键 flag**:`--max-model-len 32768`、`--gpu-memory-utilization 0.92`、`--default-chat-template-kwargs '{"enable_thinking":false}'`(**关思考链**,否则答题会吐一大段 reasoning,判分口径全乱)
- **软件栈**:vllm `0.19.1` + torch `2.10.0+cu128`,CUDA 13,单卡 **RTX PRO 6000 Blackwell 97GB**
- **冷启动**:~3.5 min(权重加载 ~21s + CUDA graph 编译)

`serve-final.sh` 内容(供参考,机器上已有):
```bash
vllm serve /root/autodl-tmp/model \
  --served-model-name Qwen/Qwen3.6-35B-A3B-FP8 \
  --port 8000 --api-key local-eval \
  --max-model-len 32768 --gpu-memory-utilization 0.92 \
  --default-chat-template-kwargs '{"enable_thinking":false}'
```

## 标准启动流程(重启后)

1. **拿当场凭据**(host/port/password)——维护者提供。用 `! ssh ...` 或本会话内 setsid 隧道,凭据只走内存。
2. **SSH 进机器**,确认 vllm 是否在跑:`curl -s localhost:8000/v1/models`。没跑 → `setsid bash /root/autodl-tmp/serve-final.sh >serve.log 2>&1 & disown`,轮询 `serve.log` 到出现 `Application startup complete`。
3. **本地建隧道**(WSL2,遵守长命令 setsid 分离硬规):
   ```bash
   setsid ssh -N -p <PORT> -L 8000:127.0.0.1:8000 root@<HOST> </dev/null >/dev/null 2>&1 & disown
   ```
   验证:`curl -s http://127.0.0.1:8000/v1/models`(**别用 pgrep 匹配自己的命令串**——会假阳性,见踩坑史)。
4. **本地 embed sidecar**(与这台机无关,纯本地):`fastembed → /v1/embeddings` on `127.0.0.1:7999`(见 `scratchpad/embed_server.py`,bge-small-en-v1.5 384d)。
5. 评测 env 拆分:答题/抽取走 `LOCOMO_*`(→ 隧道 :8000,免费);判题走 `JUDGE_*`(→ deepseek flash,详见 [judge 端点拆分](../cmd/locomo-bench/judge_config.go))。

## 100+GB 系统内存 — 能放什么

这台机除了 97GB 显存,还有 **100+GB 系统 RAM**。可用于评测期的内存密集副产物,但**要点**:

- **它是远端**,任何放上去的东西都要走 SSH 隧道访问 → 有网络往返延迟;**只适合评测吞吐场景,不适合 engram 本体运行时**(本体必须本地/离线)。
- 合适的用途:更大的 embedding 模型、批量抽取缓存、把 LoCoMo 数据集/中间 store 放在其内存盘(tmpfs)加速、或在同机再起一个 embedding sidecar 省本地内存。
- **不合适**:把 engram 的持久 SQLite store 常驻远端(违反 local-first);把任何需要长期存活的东西放上去(**空闲必停** → 数据蒸发)。
- 若要用其内存,优先"评测期临时、可重建、随停机蒸发"的数据;持久产物一律落本地 scratchpad/仓库。

## 踩坑史(复用前必读)

- **假阳性隧道检测**:`pgrep -f` 会匹配到你自己刚发的命令串 → 误判"隧道已存在"。用 `curl /v1/models` 或 `ss -ltnp | grep :8000` 判存活。
- **WSL2 长命令必须 setsid 分离**:Bash 工具靠 stdout EOF 判完成,隧道/serve 会"假死"。见 CLAUDE.md「Long-Running Commands on WSL2」。
- **凭据零落盘**:host/port/password/真 API key 绝不进被追踪文件、日志、工具响应。判题 DeepSeek key 走 env(可存 `~/.config/engram/` 非追踪路径,同 [[locomo-relay-key]] 惯例)。
- **`enable_thinking:false` 不能漏**:漏了 → 答题吐 reasoning,force-answer 简洁口径失效,判分虚高/虚低。
- **别把本地 embed sidecar :7999 变成付费云 rerank 代理**:死规则——禁用付费云 rerank(gte-rerank 等)作涨分杠杆。sidecar 只做本地 embedding。

相关记忆:`local-vllm-answer-stack`、`locomo-relay-key`、`offline-coverage-bakeoff-setup`。
