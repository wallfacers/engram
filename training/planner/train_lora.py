#!/usr/bin/env python3
"""023 supervised arm: TRL SFT + LoRA on Qwen2.5-7B-Instruct.

Trains the Evidence Planner to map (query, frozen candidates) → proposal JSON.
The conversation format MUST match the Go adapter
(cmd/locomo-bench/local_planner.go: plannerSystemPrompt + renderPlannerPrompt)
so inference after training is distribution-consistent with training. Keep the
two templates in sync; the pairing/validity harness does not re-render.

Offline only. Base model + LoRA adapter; single 24 GiB GPU (QLoRA 4-bit keeps
headroom). One full rebuild ≤ 24 GPU-hours (spec FR-034).

Usage:
  python3 train_lora.py \
      --data data/processed/train.jsonl \
      --base-model Qwen2.5-7B-Instruct \
      --out models/planner-lora \
      --config configs/train.yaml
"""

import argparse
import json
import os
import random

import torch
import yaml
from datasets import load_dataset
from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training
from transformers import (
    AutoModelForCausalLM,
    AutoTokenizer,
    BitsAndBytesConfig,
    TrainingArguments,
)
from trl import SFTTrainer

# ---- Prompt templates. MUST mirror cmd/locomo-bench/local_planner.go ----
SYSTEM_PROMPT = (
    "You are the Evidence Planner for a memory retrieval system. Given a question "
    "and a ranked list of candidate evidence, decide which evidence to "
    "keep/extract/merge and what constraints the answer must satisfy.\n\n"
    "Emit ONLY a JSON object with this exact shape:\n"
    '{"need":{"entities":["..."],"time_constraints":["YYYY-MM-DD"],"operands":'
    '[{"name":"...","satisfied":false}],"list_cardinality":{"known":false,"count":0},'
    '"update_state":"","gap":null},"actions":[{"kind":"KEEP","candidate_id":"...",'
    '"source_id":"..."}]}\n\n'
    "Action kinds are exactly: KEEP, EXTRACT, DROP, MERGE, FETCH_SOURCE.\n"
    "Every action must reference only the candidate/source ids given to you. "
    "Do not invent ids, sources, or constraints."
)


def render_user(query, candidates):
    """Mirror renderPlannerPrompt (local_planner.go)."""
    lines = [f"Question: {query}", "", "Ranked candidates:"]
    if not candidates:
        lines.append("(none)")
    for i, c in enumerate(candidates):
        srcs = ",".join(c.get("source_ids", []))
        lines.append(
            f"[{i}] id={c['id']} kind={c.get('kind','')} rank={c.get('rank', i)} "
            f"score={c.get('score', 0.0):.4f} sources={srcs}\n{c.get('text','')}"
        )
    lines.append("")
    lines.append("Emit the proposal JSON now.")
    return "\n".join(lines)


def format_example(ex):
    """One jsonl Training Example → (system, user, assistant target JSON)."""
    target = json.dumps(ex["target"], ensure_ascii=False)
    return SYSTEM_PROMPT, render_user(ex["query"], ex["candidates"]), target


def build_dataset(path, tokenizer, max_seq, seed):
    ds = load_dataset("json", data_files=path, split="train")
    rng = random.Random(seed)

    def tokenize(batch):
        sys_text, user_text, asst_text = [], [], []
        for q, cands, target in zip(batch["query"], batch["candidates"], batch["target"]):
            sys_text.append(SYSTEM_PROMPT)
            user_text.append(render_user(q, cands))
            asst_text.append(json.dumps(target, ensure_ascii=False))
        texts = [
            tokenizer.apply_chat_template(
                [
                    {"role": "system", "content": s},
                    {"role": "user", "content": u},
                    {"role": "assistant", "content": a},
                ],
                tokenize=False,
            )
            for s, u, a in zip(sys_text, user_text, asst_text)
        ]
        return tokenizer(
            texts, truncation=True, max_length=max_seq, padding=False, return_tensors=None
        )

    return ds.map(tokenize, batched=True, remove_columns=ds.column_names)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--data", required=True, help="data/processed/train.jsonl (023 Training Examples)")
    ap.add_argument("--base-model", default="Qwen2.5-7B-Instruct")
    ap.add_argument("--out", default="models/planner-lora")
    ap.add_argument("--config", default="configs/train.yaml")
    ap.add_argument("--seed", type=int, default=0)
    args = ap.parse_args()

    with open(args.config) as f:
        cfg = yaml.safe_load(f)

    torch.manual_seed(args.seed)
    random.seed(args.seed)

    bnb = BitsAndBytesConfig(
        load_in_4bit=True,
        bnb_4bit_use_double_quant=True,
        bnb_4bit_quant_type="nf4",
        bnb_4bit_compute_dtype=torch.bfloat16,
    )
    model = AutoModelForCausalLM.from_pretrained(
        args.base_model, quantization_config=bnb, device_map="auto",
        trust_remote_code=True, torch_dtype=torch.bfloat16,
    )
    tokenizer = AutoTokenizer.from_pretrained(args.base_model, trust_remote_code=True)
    tokenizer.padding_side = "right"

    model = prepare_model_for_kbit_training(model)
    peft = LoraConfig(
        r=cfg["lora"]["r"],
        lora_alpha=cfg["lora"]["alpha"],
        lora_dropout=cfg["lora"]["dropout"],
        bias="none",
        task_type="CAUSAL_LM",
        target_modules=cfg["lora"]["target_modules"],
    )
    model = get_peft_model(model, peft)

    dataset = build_dataset(args.data, tokenizer, cfg["training"]["max_seq_length"], args.seed)

    training_args = TrainingArguments(
        output_dir=args.out,
        per_device_train_batch_size=cfg["training"]["per_device_batch_size"],
        gradient_accumulation_steps=cfg["training"]["grad_accum"],
        learning_rate=cfg["training"]["learning_rate"],
        num_train_epochs=cfg["training"]["epochs"],
        logging_steps=cfg["training"]["logging_steps"],
        save_steps=cfg["training"]["save_steps"],
        bf16=True,
        gradient_checkpointing=True,
        optim="paged_adamw_8bit",
        seed=args.seed,
        report_to=[],
        save_total_limit=2,
    )
    trainer = SFTTrainer(
        model=model,
        args=training_args,
        train_dataset=dataset,
        tokenizer=tokenizer,
        dataset_text_field="input_ids",  # already tokenized via build_dataset
        max_seq_length=cfg["training"]["max_seq_length"],
    )
    trainer.train()
    trainer.save_model(args.out)
    print(f"adapter saved to {args.out}")

    # Frozen training fingerprint (FR-015): record the summary for the data card.
    summary = {
        "base_model": args.base_model,
        "seed": args.seed,
        "config": cfg,
        "train_examples": len(dataset),
        "lora_r": cfg["lora"]["r"],
    }
    os.makedirs(args.out, exist_ok=True)
    with open(os.path.join(args.out, "train_summary.json"), "w") as f:
        json.dump(summary, f, indent=2)


if __name__ == "__main__":
    main()
