#!/usr/bin/env python3
"""budget_watchdog.py — 训练预算记账 / watchdog（跨 run 累计硬门）

T006. 契约: data-model.md TrainingConfig.budget_ceiling（¥100 / 8 GPU·时，机器可执行）。
跨 run 累计 GPU 时/费用；超限自动拒绝启动新 run；每 run manifest 记录
seed/hash/exit status/e2e report ID 关联（review D3/F）。

用法:
  # 记账文件（每 run 一条 JSONL）——AutoDL 数据盘，防系统盘污染
  export BUDGET_LEDGER=/root/autodl-tmp/037-reranker/budget.jsonl

  # run 开始前 gate check（超限 → exit 1 拒绝启动）
  python3 tools/budget_watchdog.py gate --ledger $BUDGET_LEDGER \
      --ceiling-cost 100 --ceiling-gpu-hours 8

  # run 结束后记一笔
  python3 tools/budget_watchdog.py add --ledger $BUDGET_LEDGER \
      --gpu-hours 1.5 --cost-rate 2.5 --tag bce-infonce-epoch1 \
      --seed 42 --data-hash <sha> --report-id 037-us2 \
      --exit-status 0

  # 汇总
  python3 tools/budget_watchdog.py status --ledger $BUDGET_LEDGER

gate 返回:
  exit 0 = 预算内可启动; exit 1 = 超限（打印累计/上限）。
"""
import argparse
import hashlib
import json
import os
import sys
import time


def load_ledger(path):
    entries = []
    if os.path.exists(path):
        with open(path) as f:
            for line in f:
                line = line.strip()
                if line:
                    entries.append(json.loads(line))
    return entries


def totals(entries):
    cost = sum(e.get("cost", 0.0) for e in entries)
    hours = sum(e.get("gpu_hours", 0.0) for e in entries)
    return cost, hours


def sha256_file_or_str(x):
    if os.path.exists(x):
        h = hashlib.sha256()
        with open(x, "rb") as f:
            for chunk in iter(lambda: f.read(65536), b""):
                h.update(chunk)
        return h.hexdigest()[:16]
    return hashlib.sha256(str(x).encode()).hexdigest()[:16]


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    sub = ap.add_subparsers(dest="cmd", required=True)

    g = sub.add_parser("gate")
    g.add_argument("--ledger", required=True)
    g.add_argument("--ceiling-cost", type=float, default=100.0)
    g.add_argument("--ceiling-gpu-hours", type=float, default=8.0)
    g.add_argument("--dry-run", action="store_true")

    a = sub.add_parser("add")
    a.add_argument("--ledger", required=True)
    a.add_argument("--gpu-hours", type=float, required=True)
    a.add_argument("--cost-rate", type=float, default=0.0, help="元/GPU·时")
    a.add_argument("--cost", type=float, default=None, help="直接给总费用（覆盖 rate×hours）")
    a.add_argument("--tag", default="")
    a.add_argument("--seed", type=int, default=None)
    a.add_argument("--data-hash", default=None)
    a.add_argument("--report-id", default=None)
    a.add_argument("--exit-status", type=int, default=None)

    s = sub.add_parser("status")
    s.add_argument("--ledger", required=True)
    s.add_argument("--ceiling-cost", type=float, default=100.0)
    s.add_argument("--ceiling-gpu-hours", type=float, default=8.0)
    args = ap.parse_args()

    if args.cmd == "status":
        entries = load_ledger(args.ledger)
        cost, hours = totals(entries)
        print(f"runs={len(entries)}  gpu_hours={hours:.2f}/{args.ceiling_gpu_hours}  "
              f"cost=¥{cost:.2f}/{args.ceiling_cost}")
        for e in entries:
            print(f"  {e.get('ts','?'):20s} {e.get('tag',''):20s} "
                  f"h={e.get('gpu_hours'):.2f} ¥{e.get('cost'):.2f} "
                  f"exit={e.get('exit_status')} report={e.get('report_id')}")
        if cost > args.ceiling_cost or hours > args.ceiling_gpu_hours:
            print("超限：拒绝新 run")
            sys.exit(1)
        sys.exit(0)

    if args.cmd == "gate":
        entries = load_ledger(args.ledger)
        cost, hours = totals(entries)
        over = cost > args.ceiling_cost or hours > args.ceiling_gpu_hours
        print(f"gate: gpu_hours={hours:.2f}/{args.ceiling_gpu_hours}  "
              f"cost=¥{cost:.2f}/{args.ceiling_cost}  → {'超限(拒绝)' if over else '可启动'}")
        if args.dry_run:
            print("dry-run: 不写 ledger")
        sys.exit(1 if over else 0)

    # add
    entries = load_ledger(args.ledger)
    cost = args.cost if args.cost is not None else args.gpu_hours * args.cost_rate
    entry = {
        "ts": time.strftime("%Y-%m-%dT%H:%M:%S"),
        "gpu_hours": args.gpu_hours,
        "cost": round(cost, 2),
        "tag": args.tag,
        "seed": args.seed,
        "data_hash": args.data_hash or (sha256_file_or_str(args.data_hash) if args.data_hash else None),
        "report_id": args.report_id,
        "exit_status": args.exit_status,
    }
    os.makedirs(os.path.dirname(os.path.abspath(args.ledger)), exist_ok=True)
    with open(args.ledger, "a") as f:
        f.write(json.dumps(entry, ensure_ascii=False) + "\n")
    c, h = totals(load_ledger(args.ledger))
    print(f"recorded: {args.tag} h={args.gpu_hours} ¥{cost:.2f} → 累计 h={h:.2f} ¥{c:.2f}")


if __name__ == "__main__":
    main()
