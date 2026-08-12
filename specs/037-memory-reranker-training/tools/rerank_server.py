#!/usr/bin/env python3
"""rerank_server.py — 记忆重排 serving（transformers + FastAPI 聚合端点）

037 US2 的 serving 方案（T018）。替代 vLLM：
  1. vLLM 0.27 cu13 与 CUDA 12.8 驱动不兼容（US1 实测，T008）。
  2. vLLM serve 训练产物有兼容 bug（serve merged 返回 base 分数；--enable-lora
     报 modules_to_save 不支持）——transformers 加载 merged 已验证训练生效
     （T017），本端点绕开该问题。

同一端口聚合两个 OpenAI 兼容端点（locomo-bench 无独立 EMBED_RERANK_BASE_URL，
embedding 与 rerank 共享 EMBED_BASE_URL，contracts/rerank-serving.md）：
  POST {base}/embeddings    # BAAI/bge-small-en-v1.5（与 US1 同源），384-dim
  POST {base}/rerank        # Qwen3-Reranker-0.6B（base 或 merged 训练产物）

**score equation 冻结**（contracts/rerank-serving.md + train_reranker.py）：
  instruct = "Instruct: {instruction}\\nQuery: {query}"
  doc      = "Query: {query}\\nDocument: {document}"
  score    = sigmoid(logits[:, -1, yes_id] - logits[:, -1, no_id])
  截断顺序 = doc 段优先截断（instruct 段保留），与训练 encode_batch 一致。
  INSTRUCTION 与训练完全同一字符串。

用法（远程 GPU 机器）:
  # serve 训练产物（merged bce-infonce）
  python3 tools/rerank_server.py \
      --rerank-model /root/autodl-tmp/037-reranker/ckpts/bce-infonce/merged \
      --port 8000
  # serve base（US1 对照）
  python3 tools/rerank_server.py --rerank-model /root/autodl-tmp/037-reranker/models/Qwen3-Reranker-0.6B --port 8000

评测消费:
  EMBED_BASE_URL=http://<host>:8000/v1 EMBED_RERANK_MODEL=engram-memory-reranker-0.6b-v1 \
  go run ./cmd/locomo-bench --data testdata/locomo/locomo.json \
      --run-dir <run-dir> --retrieval 'hybrid,hybrid+rerank' ...
"""
import argparse
import time

import torch
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
import uvicorn

# ---- 冻结项（contracts/rerank-serving.md + train_reranker.py）----
INSTRUCTION = "Given a question about past conversations, retrieve the memory entries that answer it."
MAX_LEN = 4096            # 训练默认 max_len
EMBED_MODEL_DEFAULT = "BAAI/bge-small-en-v1.5"

app = FastAPI(title="engram memory reranker")


def load_rerank(model_dir: str):
    """Qwen3-Reranker（base 或 merged）：AutoModelForCausalLM + yes/no token logits。

    与 train_reranker.py 同一加载方式，保证 merged 产物打分逻辑一致。"""
    from transformers import AutoModelForCausalLM, AutoTokenizer
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = AutoModelForCausalLM.from_pretrained(model_dir, torch_dtype=torch.bfloat16)
    model = model.to(device).eval()
    tok = AutoTokenizer.from_pretrained(model_dir)
    if tok.pad_token is None:
        tok.pad_token = tok.eos_token
    if model.config.pad_token_id is None:
        model.config.pad_token_id = tok.pad_token_id
    yes_id, no_id = tok.convert_tokens_to_ids("yes"), tok.convert_tokens_to_ids("no")
    if yes_id is None or no_id is None or yes_id < 0 or no_id < 0:
        raise ValueError(f"tokenizer 缺 yes/no token (yes={yes_id} no={no_id})")
    return model, tok, device, yes_id, no_id


def load_embed(model_name: str):
    """bge-small-en-v1.5（384-dim）：query 前缀 + mean pooling + L2 normalize。

    与 US1 rerank_server.py 同源（BAAI/bge-small-en-v1.5），保证 US1/US2 的
    hybrid 臂检索可比。"""
    from transformers import AutoModel, AutoTokenizer
    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = AutoModel.from_pretrained(model_name).to(device).eval()
    tok = AutoTokenizer.from_pretrained(model_name)
    return model, tok, device


def rerank_scores(model, tok, device, yes_id, no_id, query: str, documents):
    """对 (query, documents) 打分，返回 sigmoid score 数组（与训练 score equation 一致）。"""
    instruct = f"Instruct: {INSTRUCTION}\nQuery: {query}"
    docs = [f"Query: {query}\nDocument: {d}" for d in documents]
    enc = tok([instruct] * len(docs), padding=True, truncation=True,
              max_length=MAX_LEN, return_tensors="pt")
    doc_enc = tok(docs, padding=True, truncation=True,
                  max_length=MAX_LEN - enc["input_ids"].shape[1], return_tensors="pt")
    input_ids = torch.cat([enc["input_ids"], doc_enc["input_ids"]], dim=1).to(device)
    attn = torch.cat([enc["attention_mask"], doc_enc["attention_mask"]], dim=1).to(device)
    with torch.no_grad():
        logits = model(input_ids=input_ids, attention_mask=attn).logits
        yes_no = logits[:, -1, yes_id] - logits[:, -1, no_id]
        return torch.sigmoid(yes_no).float().cpu().numpy().tolist()


def embed_texts(model, tok, device, texts):
    """mean pooling + L2 normalize（BGE 标准 pipeline，不加检索前缀）。"""
    enc = tok(texts, padding=True, truncation=True, max_length=512, return_tensors="pt").to(device)
    with torch.no_grad():
        out = model(**enc)
    last_hidden = out.last_hidden_state  # (B, L, H)
    mask = enc["attention_mask"].unsqueeze(-1).float()
    summed = (last_hidden * mask).sum(dim=1)
    lens = mask.sum(dim=1).clamp(min=1e-9)
    vecs = summed / lens
    vecs = torch.nn.functional.normalize(vecs, p=2, dim=1)
    return vecs.float().cpu().numpy().tolist()


@app.post("/v1/rerank")
async def rerank(request: Request):
    body = await request.json()
    query = body.get("query", "")
    documents = body.get("documents", [])
    top_n = body.get("top_n", len(documents))
    if not query or not documents:
        return JSONResponse({"error": {"message": "query and documents are required"}}, status_code=400)
    scores = rerank_scores(RERANK_MODEL, RERANK_TOK, RERANK_DEVICE,
                           RERANK_YES, RERANK_NO, query, documents)
    order = sorted(range(len(documents)), key=lambda i: scores[i], reverse=True)[:top_n]
    results = [{"index": i, "relevance_score": scores[i]} for i in order]
    return {"results": results}


@app.post("/v1/embeddings")
async def embeddings(request: Request):
    body = await request.json()
    inputs = body.get("input", [])
    if isinstance(inputs, str):
        inputs = [inputs]
    if not inputs:
        return JSONResponse({"error": {"message": "input is required"}}, status_code=400)
    # 不加检索前缀：locomo-bench 批量调用无法区分 query/doc，且 US1 用
    # sentence-transformers 默认行为（不加前缀）——保持 US1/US2 的 hybrid 臂可比。
    vecs = embed_texts(EMBED_MODEL, EMBED_TOK, EMBED_DEVICE, inputs)
    data = [{"embedding": v, "index": i} for i, v in enumerate(vecs)]
    return {"data": data, "model": body.get("model", "")}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--rerank-model", required=True,
                    help="Qwen3-Reranker 模型目录（base 或 merged 训练产物）")
    ap.add_argument("--embed-model", default=EMBED_MODEL_DEFAULT)
    ap.add_argument("--port", type=int, default=8000)
    ap.add_argument("--host", default="0.0.0.0")
    args = ap.parse_args()

    global RERANK_MODEL, RERANK_TOK, RERANK_DEVICE, RERANK_YES, RERANK_NO
    global EMBED_MODEL, EMBED_TOK, EMBED_DEVICE

    t0 = time.time()
    RERANK_MODEL, RERANK_TOK, RERANK_DEVICE, RERANK_YES, RERANK_NO = load_rerank(args.rerank_model)
    print(f"[rerank_server] rerank loaded from {args.rerank_model} "
          f"(device={RERANK_DEVICE}, yes={RERANK_YES} no={RERANK_NO}) {time.time()-t0:.1f}s")
    t0 = time.time()
    EMBED_MODEL, EMBED_TOK, EMBED_DEVICE = load_embed(args.embed_model)
    print(f"[rerank_server] embed loaded {args.embed_model} (device={EMBED_DEVICE}) {time.time()-t0:.1f}s")

    # 启动前自检：score equation 与 embedding 冒烟
    probe = rerank_scores(RERANK_MODEL, RERANK_TOK, RERANK_DEVICE,
                          RERANK_YES, RERANK_NO, "test", ["passage one", "passage two"])
    print(f"[rerank_server] smoke rerank scores: {[round(s, 3) for s in probe]}")
    v = embed_texts(EMBED_MODEL, EMBED_TOK, EMBED_DEVICE, ["test sentence"])
    print(f"[rerank_server] smoke embed dim={len(v[0])} norm={sum(x*x for x in v[0])**.5:.3f}")

    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()
