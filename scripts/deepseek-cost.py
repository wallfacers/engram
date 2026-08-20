#!/usr/bin/env python3
"""DeepSeek 精确成本计算 —— 从 bench 实测 cost.json 的 usage + 官方价。

关键口径:DeepSeek 的 `input_tokens` 只计 **cache 未命中** 部分;cache 命中部分
在 `cache_read_input_tokens`(bench 若记录则用,否则从 ctx_mean × calls 反推)。
output 无缓存。

官方价(¥/Mtok，2026-08-16 起):
                  in(hit)  in(miss)  out
  deepseek-v4-pro      0.30     9      27    # 高峰
  deepseek-v4-flash    0.10     3       9    # 高峰

默认按高峰时段（北京时间 09:00–12:00、14:00–18:00）计价；传入第二个
参数 `offpeak` 使用半价的空闲时段价格。
"""
import json
import sys

PEAK_PRO = {"hit": 0.30, "miss": 9.0, "out": 27.0}
PEAK_FLASH = {"hit": 0.10, "miss": 3.0, "out": 9.0}


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
    if len(sys.argv) not in (2, 3) or (len(sys.argv) == 3 and sys.argv[2] not in ("peak", "offpeak")):
        raise SystemExit("usage: deepseek-cost.py <cost.json> [peak|offpeak]")
    path = sys.argv[1]
    period = sys.argv[2] if len(sys.argv) == 3 else "peak"
    factor = 0.5 if period == "offpeak" else 1.0
    pro = {key: value * factor for key, value in PEAK_PRO.items()}
    flash = {key: value * factor for key, value in PEAK_FLASH.items()}
    d = json.load(open(path))
    br = d["by_role"]
    ctx = d.get("answer_context_tokens_mean", 0)
    a_detail = role_cost(br.get("answer", {}), pro, ctx)
    j_detail = role_cost(br.get("judge", {}), flash, 0)  # judge 无 ctx_mean,用记录值
    a_cost, a = a_detail
    j_cost, j = j_detail
    total = a_cost + j_cost
    print("=" * 64)
    print("成本精算(实测 usage × 官方 DeepSeek %s时段价)" % ("空闲" if period == "offpeak" else "高峰"))
    print("=" * 64)
    print("answer (v4-pro, %s):" % period)
    print("  calls=%d  in_hit=%d  in_miss=%d  out=%d" % (a["calls"], a["in_hit"], a["in_miss"], a["out"]))
    print("  hit费=¥%.4f  miss费=¥%.4f  out费=¥%.4f  = ¥%.2f" % (a["hit_fees"], a["miss_fees"], a["out_fees"], a_cost))
    print("judge (v4-flash, %s):" % period)
    print("  calls=%d  in_miss=%d  out=%d  = ¥%.2f" % (j["calls"], j["in_miss"], j["out"], j_cost))
    print("-" * 64)
    print("合计 = ¥%.2f  ($%.2f @7.1)" % (total, total / 7.1))
    print("=" * 64)
    # 带宽:假设 in 全 miss 的上界(缓存失效场景)
    a_full = (a["in_hit"] + a["in_miss"]) / 1e6 * pro["miss"] + a["out"] / 1e6 * pro["out"]
    print("上界(若缓存全失效,in 按 miss 计)= ¥%.2f" % (a_full + j_cost))


if __name__ == "__main__":
    main()
