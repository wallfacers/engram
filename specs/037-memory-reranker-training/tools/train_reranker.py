#!/usr/bin/env python3
"""train_reranker.py — 两段式 LoRA 重排训练（Qwen3-Reranker-0.6B）

T015/T017. 配方: research.md R2; TrainingConfig: data-model.md
两段式:
  stage1 BCE pointwise（sigmoid(yes−no) 回归 label）
  stage2 InfoNCE listwise（in-batch negatives, multi-positive mask）
score equation / template / 截断: **冻结唯一实现**（contracts/rerank-serving.md 三方
一致性）。

**score equation（2026-08-11 T016 实测修正）**: 训练用 **Qwen3 原生 yes/no token
logits**（`logits[:, -1, yes_token_id] − logits[:, -1, no_token_id]` 的 sigmoid），
与 vllm `--hf-overrides classifier_from_token=["no","yes"]` 的 `/v1/rerank`
relevance_score 完全一致。**不用 AutoModelForSequenceClassification 的随机
score.weight**——smoke 实测验证该头训练的产物经 vllm serve 分数与 base 完全相同
（训练被忽略），违反 score equation 契约。

基座输入格式（Qwen3-Reranker 官方，T004 实测冻结）:
  instruct = "Instruct: {instruction}\nQuery: {query}"
  doc      = "Query: {query}\nDocument: {document}"
  score = sigmoid(logits[-1, yes_id] − logits[-1, no_id])

用法:
  python3 tools/train_reranker.py --data train-r1.jsonl --subset 200 \
      --epochs 1 --output smoke-run/                     # T016 smoke
  python3 tools/train_reranker.py --data train-r1.jsonl --epochs 3 \
      --checkpoint-suffix bce --output /root/autodl-tmp/037-reranker/bce/
  python3 tools/train_reranker.py --data train-r1.jsonl --epochs 3 \
      --checkpoint-suffix bce-infonce --output /root/autodl-tmp/037-reranker/bce-infonce/

注意:
  - AutoDL 磁盘卫生: 产物写 /root/autodl-tmp/（系统盘仅 ~30G）
  - 预算 watchdog（T006）gate 前置; seed 固定可复现; manifest 写 hash/超参/依赖版本
  - bf16; max_len 2048-4096; LoRA r=16 alpha=32 target=attn+mlp
"""
import argparse
import hashlib
import json
import os
import random
import sys
import time
from collections import defaultdict

# 冻结项（contracts/rerank-serving.md）: score equation / template / 截断
BASE_MODEL = "Qwen/Qwen3-Reranker-0.6B"
INSTRUCTION = "Given a question about past conversations, retrieve the memory entries that answer it."
LOSS_INFONCE = True   # stage2 默认开（--checkpoint-suffix=bce 时关）
SEED_FIXED = 42


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()


def load_samples(data_path, subset, max_query_len, max_doc_len):
    """加载训练 JSONL → [{query, document, label, group_id, ...}]"""
    samples = []
    with open(data_path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            s = json.loads(line)
            q, d = s["query"], s["document"]
            if len(q) > max_query_len or len(d) > max_doc_len:
                continue
            samples.append({
                "query": q, "document": d,
                "label": float(s["label"]),
                "is_positive": bool(s["is_positive"]),
                "group_id": s["query_group_id"],
                "category": s["category"],
                "negative_type": s.get("negative_type"),
            })
    if subset:
        samples = samples[:subset]
    return samples


def build_texts(samples, instruction):
    """Qwen3-Reranker 输入: instruct 段 + (query, document) 段（冻结模板）。"""
    texts = []
    for s in samples:
        instruct = f"Instruct: {instruction}\nQuery: {s['query']}"
        doc = f"Query: {s['query']}\nDocument: {s['document']}"
        texts.append((instruct, doc))
    return texts


def make_manifest(args, data_hash, code_hash, deps, num_samples, n_pos, n_neg, groups):
    return {
        "base_model": BASE_MODEL,
        "checkpoint_suffix": args.checkpoint_suffix,
        "stages": "bce" if args.checkpoint_suffix == "bce" else "bce-infonce",
        "seed": args.seed,
        "epochs": args.epochs,
        "max_len": args.max_len,
        "lora_r": args.lora_r, "lora_alpha": args.lora_alpha,
        "lr": args.lr,
        "score_equation": "sigmoid(yes_logit - no_logit)",
        "template": "Instruct/Query/Document（Qwen3-Reranker 原生）",
        "data_hash": data_hash,
        "code_hash": code_hash,
        "deps": deps,
        "num_samples": num_samples,
        "n_pos": n_pos, "n_neg": n_neg,
        "num_query_groups": groups,
        "ts": time.strftime("%Y-%m-%dT%H:%M:%S"),
    }


def train(args):
    import torch
    import transformers
    import peft
    from peft import LoraConfig, get_peft_model
    from transformers import AutoModelForSequenceClassification, AutoTokenizer

    torch.manual_seed(args.seed)
    random.seed(args.seed)
    os.makedirs(args.output, exist_ok=True)

    # 数据
    samples = load_samples(args.data, args.subset, args.max_query_len, args.max_doc_len)
    n_pos = sum(1 for s in samples if s["is_positive"])
    n_neg = len(samples) - n_pos
    groups = len({s["group_id"] for s in samples})
    print(f"data: {len(samples)} samples (pos={n_pos} neg={n_neg}) groups={groups}")

    # 模型 + tokenizer（--base-model 可指向本地路径，离线可用）
    # Qwen3ForCausalLM: 输出 token logits；score 用 yes/no token logits 差（Qwen3-Reranker 原生机制，
    # 与 vllm classifier_from_token=["no","yes"] 一致）。不用 SequenceClassification 随机 score.weight
    # （T016 smoke 实测：该头训练的产物 vllm serve 分数与 base 相同，训练被忽略）。
    from transformers import AutoModelForCausalLM
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = AutoModelForCausalLM.from_pretrained(
        args.base_model, torch_dtype=torch.bfloat16)
    model = model.to(device)  # 训练必须上 GPU（默认 CPU 会跑极慢）
    tokenizer = AutoTokenizer.from_pretrained(args.base_model)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token  # Qwen3 系列需显式 pad（batch>1）
    if model.config.pad_token_id is None:
        model.config.pad_token_id = tokenizer.pad_token_id  # transformers 5.x batch>1 检查 config
    NO_ID = tokenizer.convert_tokens_to_ids("no")
    YES_ID = tokenizer.convert_tokens_to_ids("yes")
    if NO_ID is None or YES_ID is None or NO_ID < 0 or YES_ID < 0:
        raise ValueError(f"tokenizer 缺 no/yes token (no={NO_ID} yes={YES_ID})")
    lora = LoraConfig(
        r=args.lora_r, lora_alpha=args.lora_alpha,
        target_modules=["q_proj", "k_proj", "v_proj", "o_proj",
                        "gate_proj", "up_proj", "down_proj"],  # attn + mlp
        lora_dropout=0.05, bias="none", task_type="SEQ_CLS")
    model = get_peft_model(model, lora)
    model.print_trainable_parameters()

    # 优化器 / schedule
    optimizer = torch.optim.AdamW(model.parameters(), lr=args.lr)
    total_steps = len(samples) * args.epochs // (args.per_device_batch * args.grad_accum)
    scheduler = transformers.get_cosine_schedule_with_warmup(
        optimizer, num_warmup_steps=max(1, total_steps // 20), num_training_steps=total_steps)

    texts = build_texts(samples, INSTRUCTION)

    def encode_batch(batch_idx):
        # 冻结模板: instruct 段 + doc 段；截断顺序 = doc 截断（query 保留）
        instructs = [texts[i][0] for i in batch_idx]
        docs = [texts[i][1] for i in batch_idx]
        enc = tokenizer(
            instructs, text_pair=None, padding=True, truncation=True,
            max_length=args.max_len, return_tensors="pt")
        doc_enc = tokenizer(
            docs, padding=True, truncation=True,
            max_length=args.max_len - enc["input_ids"].shape[1],
            return_tensors="pt")
        # 拼接 instruct + doc（Qwen3-Reranker: instruct 段后接 doc 段）
        input_ids = torch.cat([enc["input_ids"], doc_enc["input_ids"]], dim=1)
        attention_mask = torch.cat([enc["attention_mask"], doc_enc["attention_mask"]], dim=1)
        return input_ids.to(model.device), attention_mask.to(model.device)

    use_stage2 = (args.checkpoint_suffix != "bce") and LOSS_INFONCE

    # ---- 两段式训练 ----
    for epoch in range(args.epochs):
        idx = list(range(len(samples)))
        random.shuffle(idx)
        model.train()
        for start in range(0, len(idx), args.per_device_batch * args.grad_accum):
            micro_batches = []
            for b_start in range(start, min(start + args.per_device_batch * args.grad_accum, len(idx)),
                                 args.per_device_batch):
                batch_idx = idx[b_start:b_start + args.per_device_batch]
                if not batch_idx:
                    break
                input_ids, attn = encode_batch(batch_idx)
                out = model(input_ids=input_ids, attention_mask=attn)
                logits = out.logits  # (B, seq, vocab) — Qwen3 token logits
                yes_no = logits[:, -1, YES_ID] - logits[:, -1, NO_ID]  # 冻结 score equation
                labels = torch.tensor([samples[i]["label"] for i in batch_idx],
                                      device=model.device)
                # stage1 BCE
                loss = torch.nn.functional.binary_cross_entropy_with_logits(yes_no, labels)
                # stage2 InfoNCE（multi-positive mask）
                if use_stage2 and epoch >= args.stage2_start_epoch:
                    groups_in_batch = [samples[i]["group_id"] for i in batch_idx]
                    # 同 group 索引 = multi-positive mask
                    pos_mask = torch.zeros((len(batch_idx), len(batch_idx)), device=model.device)
                    for a, ga in enumerate(groups_in_batch):
                        for b_ix, gb in enumerate(groups_in_batch):
                            if ga == gb:
                                pos_mask[a, b_ix] = 1.0
                    exp = torch.exp(yes_no)
                    denom = exp.sum()
                    # 每样本: -log( sum_{j in same group} exp_j / denom )
                    num = (pos_mask * exp.view(-1, 1)).sum(dim=1)
                    nce = -(torch.log(num / denom + 1e-9)).mean()
                    loss = loss + args.infonce_weight * nce
                loss.backward()
                micro_batches.append(loss.item())
            optimizer.step()
            scheduler.step()
            optimizer.zero_grad()
            if start % (args.per_device_batch * args.grad_accum * 20) == 0 or start + args.per_device_batch >= len(idx):
                print(f"  epoch {epoch} step {start // (args.per_device_batch * args.grad_accum)}/"
                      f"{total_steps} loss={sum(micro_batches)/len(micro_batches):.4f}")

    # 保存
    model.save_pretrained(os.path.join(args.output, "adapter"))
    if args.merge:
        merged = model.merge_and_unload()
        merged.save_pretrained(os.path.join(args.output, "merged"))
        tokenizer.save_pretrained(os.path.join(args.output, "merged"))
    print(f"saved: {args.output}")

    # manifest
    data_hash = sha256_file(args.data)
    code_hash = sha256_file(__file__)
    deps = {"torch": torch.__version__, "transformers": transformers.__version__,
            "peft": peft.__version__}
    manifest = make_manifest(args, data_hash, code_hash, deps, len(samples), n_pos, n_neg, groups)
    with open(os.path.join(args.output, "manifest.json"), "w") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
    print(f"manifest: {os.path.join(args.output, 'manifest.json')}")


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--data", required=True)
    ap.add_argument("--base-model", default=BASE_MODEL,
                    help="基座模型路径（HF id 或本地目录；AutoDL 离线用本地路径）")
    ap.add_argument("--subset", type=int, default=0)
    ap.add_argument("--epochs", type=int, default=3)
    ap.add_argument("--output", required=True)
    ap.add_argument("--checkpoint-suffix", default="bce-infonce", choices=["bce", "bce-infonce"])
    ap.add_argument("--seed", type=int, default=SEED_FIXED)
    ap.add_argument("--max-len", type=int, default=4096)
    ap.add_argument("--max-query-len", type=int, default=512)
    ap.add_argument("--max-doc-len", type=int, default=4096)
    ap.add_argument("--lora-r", type=int, default=16)
    ap.add_argument("--lora-alpha", type=int, default=32)
    ap.add_argument("--lr", type=float, default=2e-5)
    ap.add_argument("--per-device-batch", type=int, default=8)
    ap.add_argument("--grad-accum", type=int, default=4)
    ap.add_argument("--stage2-start-epoch", type=int, default=2, help="InfoNCE 从第几个 epoch 起")
    ap.add_argument("--infonce-weight", type=float, default=1.0)
    ap.add_argument("--merge", action="store_true", help="保存 LoRA merge 后模型（供 vllm）")
    args = ap.parse_args()
    train(args)


if __name__ == "__main__":
    main()
