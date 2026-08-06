#!/usr/bin/env python3
"""028 US2 SFT: train a time-anchored event extractor (Qwen 3B, LoRA).

Input: train-028-v2.jsonl (build_training_data.py output). Each sample is
(message+context+session_date -> time-anchored event JSON). The assistant
target is the event_json; the model learns to output absolute dates given the
session date (research.md R4: SFT is the main signal; RL optional later).

Runs on AutoDL (single GPU). Hyperparams/seed recorded for FR-003 reproducibility.
"""
import argparse
import json
import random

import torch
from datasets import Dataset
from peft import LoraConfig, get_peft_model, prepare_model_for_kbit_training
from transformers import (
    AutoModelForCausalLM,
    AutoTokenizer,
    BitsAndBytesConfig,
    DataCollatorForSeq2Seq,
    TrainingArguments,
    Trainer,
)

SYSTEM_PROMPT = (
    "You extract structured, time-anchored events from a conversation message for a memory system.\n"
    "Respond with ONLY JSON:\n"
    '{"fact_entries":[{"text":"self-contained fact","grounded":true}],'
    '"relation_entries":[{"relation_type":"interpersonal|causal|co_participation|temporal_order|preference",'
    '"subject":"...","object":"...","text":"..."}],'
    '"absolute_ts":"YYYY-MM-DD","relative_ref":"original phrase"}\n'
    "Rules: facts self-contained (resolve pronouns); relation subject/object/text non-empty; "
    "convert every relative time to an ABSOLUTE date relative to the [session date] — put it in "
    "absolute_ts and keep the original phrase in relative_ref; absolute_ts YYYY-MM-DD; payload <= 2000 runes."
)


def to_example(s, tokenizer):
    ctx = "\n".join(f"  {c}" for c in s["context_turns"][-3:])
    speaker = s["input_text"].split(":", 1)[0]
    user = f"[source_id={s['source_msg_id']}]\n[session date: {s['session_date']}]\n{ctx}\nMessage: {s['input_text']}"
    assistant = json.dumps(s["event_json"], ensure_ascii=False)
    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {"role": "user", "content": user},
        {"role": "assistant", "content": assistant},
    ]
    return tokenizer.apply_chat_template(messages, tokenize=False, add_generation_prompt=False)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--base", default="Qwen/Qwen2.5-3B-Instruct")
    ap.add_argument("--out", required=True)
    ap.add_argument("--epochs", type=int, default=3)
    ap.add_argument("--lr", type=float, default=2e-4)
    ap.add_argument("--lora-r", type=int, default=16)
    ap.add_argument("--batch-size", type=int, default=4)
    ap.add_argument("--grad-accum", type=int, default=4)
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()

    random.seed(args.seed)
    torch.manual_seed(args.seed)
    samples = [json.loads(l) for l in open(args.data)]

    tokenizer = AutoTokenizer.from_pretrained(args.base, trust_remote_code=True)
    if tokenizer.pad_token is None:
        tokenizer.pad_token = tokenizer.eos_token

    bnb = BitsAndBytesConfig(load_in_4bit=True, bnb_4bit_compute_dtype=torch.bfloat16,
                             bnb_4bit_use_double_quant=True, bnb_4bit_quant_type="nf4")
    model = AutoModelForCausalLM.from_pretrained(args.base, quantization_config=bnb,
                                                 device_map="auto", trust_remote_code=True)
    model = prepare_model_for_kbit_training(model)
    lora = LoraConfig(r=args.lora_r, lora_alpha=2 * args.lora_r, target_modules=["q_proj", "k_proj",
                                                                                  "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj"],
                      lora_dropout=0.05, bias="none", task_type="CAUSAL_LM")
    model = get_peft_model(model, lora)

    texts = [to_example(s, tokenizer) for s in samples]
    ds = Dataset.from_dict({"text": texts})

    def tok(batch):
        return tokenizer(batch["text"], truncation=True, max_length=1024, padding=False)
    ds = ds.map(tok, batched=True, remove_columns=["text"])

    tr_args = TrainingArguments(
        output_dir=args.out,
        per_device_train_batch_size=args.batch_size,
        gradient_accumulation_steps=args.grad_accum,
        learning_rate=args.lr,
        num_train_epochs=args.epochs,
        logging_steps=50,
        save_steps=500,
        save_total_limit=2,
        bf16=True,
        seed=args.seed,
        report_to=[],
    )
    Trainer(model=model, args=tr_args, train_dataset=ds,
            data_collator=DataCollatorForSeq2Seq(tokenizer, pad_to_multiple_of=8)).train()

    model.save_pretrained(f"{args.out}/lora")
    tokenizer.save_pretrained(f"{args.out}/lora")
    print(f"LoRA saved to {args.out}/lora")


if __name__ == "__main__":
    main()
