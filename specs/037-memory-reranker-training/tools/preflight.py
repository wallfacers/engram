#!/usr/bin/env python3
"""preflight.py — 评测前 rerank 端点探活（fail-closed）

T005. 契约: contracts/rerank-serving.md（preflight fail-closed）。
评测前探活 /v1/rerank 端点：请求成功/失败计数，**零成功即标记 INVALID 并 exit 1**
——禁止 locomo-bench 在 retriever.go:707-710 静默回退（rerank 失效退化为无重排）
后仍出 GO 报告。

用法:
  python3 tools/preflight.py --base http://<vllm>:8000/v1 --model <model-id> \
      --run-dir ./.locomo-run/037-us1 [--probe-docs N] [--strict]

产出: <run-dir>/preflight.json（requests_success/requests_failed/verdict）。
--strict: 任一请求失败即 INVALID（默认：全部成功才算 PASS）。
"""
import argparse
import json
import os
import sys
import time
import urllib.request
import urllib.error

PROBE_DOCS = [
    "Caroline went to the LGBTQ support group on 7 May 2023.",
    "Melanie painted a sunrise last year.",
    "The charity race raised awareness for mental health.",
]


def probe(base, model, docs):
    payload = {"model": model, "query": "When did Caroline go to the LGBTQ support group?",
               "documents": docs, "top_n": 1}
    req = urllib.request.Request(
        f"{base}/rerank",
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json"},
        method="POST")
    t0 = time.time()
    with urllib.request.urlopen(req, timeout=30) as resp:
        body = json.loads(resp.read().decode())
        latency_ms = (time.time() - t0) * 1000
    results = body.get("results", [])
    scores = [r.get("relevance_score") for r in results]
    if not scores:
        raise ValueError(f"rerank 响应无 results: {body}")
    return scores, latency_ms


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--base", required=True, help="vLLM OpenAI 兼容 base（含 /v1）")
    ap.add_argument("--model", required=True, help="EMBED_RERANK_MODEL 模型 id")
    ap.add_argument("--run-dir", required=True, help="locomo-bench run 目录（写 preflight.json）")
    ap.add_argument("--probe-rounds", type=int, default=3, help="探活请求轮数")
    ap.add_argument("--strict", action="store_true", help="任一失败即 INVALID")
    args = ap.parse_args()

    os.makedirs(args.run_dir, exist_ok=True)
    success, failed = 0, 0
    first_scores, first_latency = None, None
    errors = []
    for i in range(args.probe_rounds):
        try:
            scores, lat = probe(args.base, args.model, PROBE_DOCS)
            success += 1
            if first_scores is None:
                first_scores, first_latency = scores, lat
        except Exception as e:  # noqa: BLE001 — fail-closed 探活捕获一切
            failed += 1
            errors.append(str(e))

    ok = (success > 0) and (not args.strict or failed == 0)
    verdict = "PASS" if ok else "INVALID"
    report = {
        "preflight": verdict,
        "requests_success": success,
        "requests_failed": failed,
        "errors": errors,
        "probe_scores": first_scores,
        "probe_latency_ms": round(first_latency, 1) if first_latency else None,
        "strict": args.strict,
        "note": "零成功 → INVALID：禁止静默回退后出 GO 报告（retriever.go:707-710）",
    }
    out = os.path.join(args.run_dir, "preflight.json")
    with open(out, "w") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)

    print(f"preflight: {verdict} (success={success} failed={failed}) → {out}")
    if first_scores is not None:
        print(f"probe scores: {first_scores}  latency: {first_latency:.0f}ms")
    if errors:
        print("errors:", errors[:3])
    if not ok:
        sys.exit(1)


if __name__ == "__main__":
    main()
