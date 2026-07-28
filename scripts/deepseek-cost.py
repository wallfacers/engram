#!/usr/bin/env python3
"""DeepSeek 精确成本计算 —— 从 bench 实测 cost.json 的 usage + 官方价。

关键口径:DeepSeek 的 `input_tokens` 只计 **cache 未命中** 部分;cache 命中部分
在 `cache_read_input_tokens`(bench 若记录则用,否则从 ctx_mean × calls 反推)。
output 无缓存。

官方价(¥/Mtok):
                  in(hit)  in(miss)  out
  deepseek-v4-pro   0.025     3       6
  deepseek-v4-flash 0.02      1       2
"""
import json
import sys

PRO = {"hit": 0.025, "miss": 3.0, "out": 6.0}
FLASH = {"hit": 0.02, "miss": 1.0, "out": 2.0}


def role_cost(role_usage, price, ctx_mean):
    """单角色成本。in_miss = 记录的 input_tokens;in_hit = 反推(总 in − miss)。"""
    calls = role_usage.get("calls", 0)
    in_miss = role_usage.get("in_tokens", 0)        # DeepSeek 只报未命中
    out = role_usage.get("out_tokens", 0)
    # 反推总 in(若 bench 记了 cache_read_input_tokens 用它,否则用 ctx_mean × calls)
    cache_read = role_usage.get("cache_read_input_tokens", 0)
    if cache_read:
        in_hit = cache_read
    else:
        total_in = ctx_mean * calls if ctx_mean else in_miss  # ctx_mean 含 system+context+question
        in_hit = max(0, total_in - in_miss)
    c_hit = in_hit / 1e6 * price["hit"]
    c_miss = in_miss / 1e6 * price["miss"]
    c_out = out / 1e6 * price["out"]
    return c_hit + c_miss + c_out, dict(calls=calls, in_hit=int(in_hit), in_miss=int(in_miss),
                                        out=int(out), hit_fees=c_hit, miss_fees=c_miss, out_fees=c_out)


def main():
    path = sys.argv[1]
    d = json.load(open(path))
    br = d["by_role"]
    ctx = d.get("answer_context_tokens_mean", 0)
    a_detail = role_cost(br.get("answer", {}), PRO, ctx)
    j_detail = role_cost(br.get("judge", {}), FLASH, 0)  # judge 无 ctx_mean,用记录值
    a_cost, a = a_detail
    j_cost, j = j_detail
    total = a_cost + j_cost
    print("=" * 64)
    print("成本精算(实测 usage × 官方 DeepSeek 价)")
    print("=" * 64)
    print("answer (v4-pro):")
    print("  calls=%d  in_hit=%d  in_miss=%d  out=%d" % (a["calls"], a["in_hit"], a["in_miss"], a["out"]))
    print("  hit费=¥%.4f  miss费=¥%.4f  out费=¥%.4f  = ¥%.2f" % (a["hit_fees"], a["miss_fees"], a["out_fees"], a_cost))
    print("judge (v4-flash):")
    print("  calls=%d  in_miss=%d  out=%d  = ¥%.2f" % (j["calls"], j["in_miss"], j["out"], j_cost))
    print("-" * 64)
    print("合计 = ¥%.2f  ($%.2f @7.1)" % (total, total / 7.1))
    print("=" * 64)
    # 带宽:假设 in 全 miss 的上界(缓存失效场景)
    a_full = (a["in_hit"] + a["in_miss"]) / 1e6 * PRO["miss"] + a["out"] / 1e6 * PRO["out"]
    print("上界(若缓存全失效,in 按 miss 计)= ¥%.2f" % (a_full + j_cost))


if __name__ == "__main__":
    main()
