# Contract: 本地 reranker sidecar(US1)/ 大 embedder sidecar(US3)

**冻结契约**——sidecar 实现必须严格匹配,否则引擎 `HTTPReranker` 解码失败并静默降级为融合序(掩盖 rerank 效果)。

## 死规则前置(硬)

- sidecar **只从本地模型文件加载** cross-encoder 与 embedding 模型;源码内**不得**出现任何云 rerank/embedding 服务 URL(DashScope / Cohere / Jina 云 / OpenAI 云等)。
- `GET /v1/models` 返回的模型列表**不得**含任何云 rerank 型号(如 `gte-rerank-v2`)。007 铲除的 7999 gte-rerank 代理禁复活。
- 凭据(远端 host/port/password)只走 env / SSH host alias / 隧道,绝不落文件/日志/响应。

## 端点 1:`POST /rerank`(裸)与 `POST /v1/rerank`

引擎 `embedding/rerank.go` 打 `{EMBED_BASE_URL}/rerank`。

**Request**
```json
{ "model": "bge-reranker-v2-m3", "query": "…", "documents": ["doc0","doc1", "…"], "top_n": 12 }
```

**Response(严格)**
```json
{ "results": [ {"index": 3, "relevance_score": 0.91}, {"index": 0, "relevance_score": 0.55} ] }
```
- `index`:必须是入参 `documents` 的 0 基下标,且 ∈ `[0, len(documents))`(越界=引擎报错)。
- `relevance_score`:float,降序不做强制(引擎自行排序),但应反映相关性。
- `results` 可少于 `documents`(top_n 截断)但下标必须有效。
- 错误时可返回 `{"error":{"message":"…"}}` + 非 200 → 引擎降级为融合序(不崩)。

## 端点 2:`POST /v1/embeddings`(US1 复用,US3 换模型)

OpenAI 兼容;US1 用 `bge-small-en-v1.5`(384d,**与固化 store 同源,不可换**);US3 用大模型(整店重建后查询同源)。

**Request** `{ "model":"bge-small-en-v1.5", "input": ["…","…"] }`
**Response** `{ "object":"list", "data":[{"object":"embedding","index":0,"embedding":[…384/1024…]}], "model":"…", "usage":{…} }`

## 端点 3:`GET /v1/models`

`{ "object":"list", "data":[ {"id":"bge-small-en-v1.5","object":"model"}, {"id":"bge-reranker-v2-m3","object":"model"} ] }`

## 部署与访问

- 部署:远端 97GB GPU 机(与 vllm 共存),或本地 CPU(fallback,慢)。
- 访问:本地 SSH 隧道 → `EMBED_BASE_URL=http://127.0.0.1:<tunnel>/v1`,`EMBED_RERANK_MODEL=bge-reranker-v2-m3`,`EMBED_MODEL=bge-small-en-v1.5`。
- WSL2 长命令 setsid 分离启动(见 [docs/remote-eval-box.md](../../../docs/remote-eval-box.md));记录不含文本的 batch 延迟到产物目录。

## 自检(启动后、跑 bench 前)

1. `curl -s $EMBED_BASE_URL/models` → 只含 bge-small + reranker,**无云型号**。
2. `curl -sX POST $EMBED_BASE_URL/rerank -d '{"model":"…","query":"a","documents":["a","b"],"top_n":2}'` → 严格 `{"results":[{"index":..,"relevance_score":..}]}`。
3. `curl -sX POST $EMBED_BASE_URL/embeddings -d '{"model":"bge-small-en-v1.5","input":["x"]}'` → 384 维。
