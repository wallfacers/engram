
#!/usr/bin/env python3
"""Combined /v1/rerank + /v1/embeddings server for locomo-bench.

**037 US1/US2 评测的实际 serving 实现**（2026-08-12 US1 实测，T008/T018）。
与 `rerank_server.py`（参考版，模板/截断略有差异）不同——**本文件是配对评测
权威**：US2 必须用它 serve merged 训练产物（仅改 --model 路径），保证与 US1
除模型权重外逐字节可比（模板 `instruct+"\\n\\n"+Query/Document`、2048 截断、
fp16、embedding 无前缀）。

Rerank: Qwen3-Reranker-0.6B via transformers (sigmoid(yes_no logit diff))
Embeddings: BAAI/bge-small-en-v1.5 via sentence-transformers (~130MB)
"""
import argparse, asyncio, json, time, torch, numpy as np
from contextlib import asynccontextmanager
from transformers import AutoModelForCausalLM, AutoTokenizer
from sentence_transformers import SentenceTransformer
from fastapi import FastAPI
from pydantic import BaseModel
import uvicorn

# --- Globals ---
RERANK_MODEL = None
RERANK_TOKENIZER = None
YES_ID = None
NO_ID = None
EMBED_MODEL = None
MODEL_PATH = ""
INSTRUCTION = "Instruct: Given a question about past conversations, retrieve the memory entries that answer it.\nQuery: {query}"

# --- Pydantic models ---
class RerankRequest(BaseModel):
    model: str = ""
    query: str
    documents: list[str]
    top_n: int | None = None
    return_documents: bool = True

class EmbeddingRequest(BaseModel):
    model: str = ""
    input: str | list[str]
    encoding_format: str = "float"

class EmbeddingData(BaseModel):
    object: str = "embedding"
    index: int
    embedding: list[float]

class EmbeddingResponse(BaseModel):
    object: str = "list"
    data: list[EmbeddingData]
    model: str

# --- Rerank ---
def rerank_score(query, docs):
    instruct_text = INSTRUCTION.format(query=query)
    scores = []
    with torch.no_grad():
        for d in docs:
            text = instruct_text + "\n\n" + f"Query: {query}\nDocument: {d}"
            enc = RERANK_TOKENIZER(text, return_tensors="pt", truncation=True, max_length=2048)
            if torch.cuda.is_available():
                enc = {k: v.cuda() for k, v in enc.items()}
            logits = RERANK_MODEL(**enc).logits[0, -1]
            s = torch.sigmoid(logits[YES_ID] - logits[NO_ID]).item()
            scores.append(round(s, 8))
    return scores

# --- Embed ---
def embed_texts(texts):
    if isinstance(texts, str):
        texts = [texts]
    vecs = EMBED_MODEL.encode(texts, normalize_embeddings=True)
    return vecs.tolist()

# --- FastAPI ---
@asynccontextmanager
async def lifespan(app: FastAPI):
    global RERANK_MODEL, RERANK_TOKENIZER, YES_ID, NO_ID, EMBED_MODEL
    print(f"Loading rerank model from {MODEL_PATH}...")
    RERANK_TOKENIZER = AutoTokenizer.from_pretrained(MODEL_PATH)
    RERANK_MODEL = AutoModelForCausalLM.from_pretrained(MODEL_PATH, torch_dtype=torch.float16)
    if torch.cuda.is_available():
        RERANK_MODEL = RERANK_MODEL.cuda()
    RERANK_MODEL.eval()
    YES_ID = RERANK_TOKENIZER.convert_tokens_to_ids("yes")
    NO_ID = RERANK_TOKENIZER.convert_tokens_to_ids("no")
    print(f"Rerank loaded. yes_id={YES_ID} no_id={NO_ID}")
    
    emb_name = "BAAI/bge-small-en-v1.5"
    print(f"Loading embed model {emb_name}...")
    # local_files_only=True: AutoDL 无 HF 外网（实测 Network unreachable），
    # bge-small 已在 /root/.cache/huggingface/hub 本地缓存；仅影响加载方式，
    # 不影响向量计算（与 US1 行为一致）
    EMBED_MODEL = SentenceTransformer(emb_name, local_files_only=True)
    if torch.cuda.is_available():
        EMBED_MODEL = EMBED_MODEL.cuda()
    print(f"Embed model loaded. dim={EMBED_MODEL.get_sentence_embedding_dimension()}, GPU={torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'cpu'}")
    print("Server ready.")
    yield

app = FastAPI(lifespan=lifespan)

@app.post("/v1/rerank")
@app.post("/rerank")
async def rerank(req: RerankRequest):
    start = time.time()
    scores = rerank_score(req.query, req.documents)
    results = []
    for i, (s, doc) in enumerate(zip(scores, req.documents)):
        results.append({"index": i, "relevance_score": s, "document": doc if req.return_documents else None})
    results.sort(key=lambda r: r["relevance_score"], reverse=True)
    if req.top_n:
        results = results[:req.top_n]
    elapsed = time.time() - start
    return {"model": MODEL_PATH, "results": results}

@app.post("/v1/embeddings")
@app.post("/embeddings")
async def embeddings(req: EmbeddingRequest):
    vecs = embed_texts(req.input)
    data = [{"object": "embedding", "index": i, "embedding": v} for i, v in enumerate(vecs)]
    return {"object": "list", "data": data, "model": "bge-small-en-v1.5"}

@app.get("/health")
async def health():
    gpu = torch.cuda.get_device_name(0) if torch.cuda.is_available() else "cpu"
    return {"status": "ok", "model": MODEL_PATH, "gpu": gpu}

@app.get("/v1/models")
async def models():
    return {"data": [{"id": "Qwen3-Reranker-0.6B", "object": "model"}, {"id": "bge-small-en-v1.5", "object": "model"}]}

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", required=True)
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument("--host", default="0.0.0.0")
    args = parser.parse_args()
    MODEL_PATH = args.model
    uvicorn.run(app, host=args.host, port=args.port)

