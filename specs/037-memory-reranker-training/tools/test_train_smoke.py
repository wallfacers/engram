#!/usr/bin/env python3
"""test_train_smoke.py — 训练 smoke 校验（产物可加载 + 三方排序一致性 + score 分布）

T016. 契约: contracts/rerank-serving.md（score equation 冻结 + 三方一致性）
检查项:
  1. 产物可加载（adapter / merged 模型 + manifest 齐全）
  2. 训练 / 合并 checkpoint / vLLM /v1/rerank **三方排序一致性**:
     同一 (query, docs) 集合 → 排序完全一致（rank correlation=1）
  3. score 分布不过度聚簇（对标 008 BGE 左偏反例——负样本分数贴近 1 是病态）
  4. 峰值 VRAM / tok/s / p95 长度实测（200/1000 样本，外推全量预算）→ 写 manifest

用法（AutoDL GPU）:
  python3 tools/test_train_smoke.py /root/autodl-tmp/037-reranker/bce-infonce/ \
      --vllm-base http://127.0.0.1:8000/v1 --eval-docs eval-set.json \
      --samples-file train-r1.jsonl --subset 200
"""
import argparse
import json
import os
import sys


def load_eval_set(path):
    """(query, [docs]) 一致性测试集。"""
    with open(path) as f:
        return json.load(f)


def rank_corr(scores_a, scores_b):
    """两个分数列表的排序一致性（0-1；1=完全一致）。"""
    order_a = sorted(range(len(scores_a)), key=lambda i: -scores_a[i])
    order_b = sorted(range(len(scores_b)), key=lambda i: -scores_b[i])
    pos_b = {idx: p for p, idx in enumerate(order_b)}
    same = sum(1 for p, idx in enumerate(order_a) if pos_b[idx] == p)
    return same / len(scores_a)


def check_train_score(training_model, eval_set):
    """用训练代码路径给 (query, docs) 打分 → 排序。"""
    # 实际由 train_reranker 的 score equation 冻结实现（T016 在 GPU 上跑）
    raise NotImplementedError("train-score 侧在 AutoDL 上通过 import train_reranker 复用")


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("run_dir", help="train_reranker.py 产物目录")
    ap.add_argument("--vllm-base", default=None, help="vLLM base URL（三方一致性用）")
    ap.add_argument("--eval-docs", required=True, help="一致性测试 (query, docs) 集合 JSON")
    ap.add_argument("--samples-file", default=None, help="训练 JSONL（score 分布统计）")
    ap.add_argument("--subset", type=int, default=200, help="score 分布统计样本数")
    ap.add_argument("--score-spread-min", type=float, default=0.15,
                    help="score 分布过聚簇判定: 正负样本 score 均值差低于此 → 病态")
    args = ap.parse_args()

    # 1) 产物可加载
    manifest_path = os.path.join(args.run_dir, "manifest.json")
    for p in [manifest_path, os.path.join(args.run_dir, "adapter")]:
        if not os.path.exists(p):
            sys.exit(f"FAIL: 产物缺失 {p}")
    manifest = json.load(open(manifest_path))
    print(f"产物可加载: manifest(score_equation={manifest.get('score_equation')}, "
          f"stages={manifest.get('stages')}) ✓")

    # 2) 三方排序一致性
    eval_set = load_eval_set(args.eval_docs)
    if args.vllm_base:
        import urllib.request
        scores_vllm = {}
        for item in eval_set:
            payload = {"model": "trained", "query": item["query"],
                       "documents": item["docs"], "top_n": len(item["docs"])}
            req = urllib.request.Request(
                f"{args.vllm_base}/rerank",
                data=json.dumps(payload).encode(),
                headers={"Content-Type": "application/json"}, method="POST")
            with urllib.request.urlopen(req, timeout=60) as resp:
                body = json.loads(resp.read().decode())
            scores_vllm[item["id"]] = [r["relevance_score"] for r in sorted(
                body["results"], key=lambda r: r["index"])]
        # 合并侧: 用加载 merged 模型打分（T016 GPU 实现）
        scores_merged = check_train_score(None, eval_set)  # TODO: merged model 打分
        for item in eval_set:
            sc = scores_vllm.get(item["id"])
            if sc is None:
                sys.exit(f"FAIL: vllm 缺 {item['id']}")
        print(f"三方一致性: 排序一致率待 GPU 完整跑（vllm 端点已探活 {len(scores_vllm)} 组）")

    # 3) score 分布不过度聚簇（有样本文件时）
    if args.samples_file:
        # T016 GPU: 用训练模型给样本打分，正负均值差 vs --score-spread-min
        print(f"score 分布统计: 需 GPU 完整跑（阈值 spread≥{args.score_spread_min}）")

    # 4) VRAM/tok/s 实测 → 外推（T016 GPU 上记录，写 manifest 旁）
    print("VRAM/tok/s 实测: AutoDL GPU 上记录（200/1000 样本），外推全量预算")


if __name__ == "__main__":
    main()
