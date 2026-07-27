#!/usr/bin/env python3
"""按类别的 oracle 覆盖上限诊断 —— 零 token、零 GPU、全离线。

回答的问题:**若存在一条完美的 category-conditional 检索道**(只对某个题目类别
开启,道内排序由 oracle 给出、零附带伤害),它最多能救回几道该类别的召回侧错题?

这是"建机制之前先量靶子"的门:oracle 上限低于同期实测的噪声标尺(同配置复跑的
Δ,近期为 0.84–0.93pp)时,机制即便完美实现也无法被现有仪器证明有效 ⇒ 直接 NO-GO,
不必写代码、不必开机器。

做法
----
1. 用 Python 精确复现 bench 的覆盖判据(`cmd/locomo-bench/attribution.go`):
   - fact 命中:session 门 + τ=0.8 方向词包含(fact 的内容词有 ≥τ 比例出现在
     gold turn 文本里);
   - chunk 命中:DiaID 精确重合(chunk 名 `chunk-c<conv>-s<sess>-<idx>` 由
     `buildSessionChunks` 的分块顺序决定,此处按 chunks.go 的 900/1100 rune
     规则复现)。
2. **自校验**:拿 trace 里已记录的 top-30 `covers_gold` 逐条比对复现结果。
   不一致率 >1% 直接中止 —— 不许拿假判据下结论。
3. 对每道目标题(指定类别 × 多数投票答错 × gold 未进 top-30),扫**整店**,枚举
   所有覆盖 gold turn 的条目,按 `memory_entries.category` 分组。
4. 上限 = 覆盖条目里含目标 category 的题数(道内 oracle 排序 ⇒ 只要它在道里就
   必然能浮到 top-R)。

用法
----
    python3 scripts/oracle-category-coverage.py \
        testdata/locomo/locomo10.json \
        .locomo-run/<run>/trace-base.jsonl \
        .locomo-run/009-bge-chunks-store \
        .locomo-run/009-opinion-store \
        '.locomo-run/009-full-A-base/run-*/results-hybrid.jsonl' \
        /tmp/oracle-detail.json \
        open_domain preference

最后两个参数分别是题目类别(trace 的 `category_name`,下划线式)与检索道要浮现的
条目类别(`memory_entries.category`,如 preference / event / chunk / user)。
两个 store 参数可以传同一个目录;传两个是为了对比"补抽取有没有加出新覆盖"。
"""
import glob
import json
import re
import sqlite3
import sys
import unicodedata
from collections import Counter, defaultdict

TAU = 0.8              # attribution.go: defaultFactCoverageTau
CHUNK_TARGET = 900     # chunks.go: chunkTargetChars
CHUNK_MAX = 1100       # chunks.go: chunkMaxChars

# attribution.go: attributionStopwords(冻结,改动会破坏可复现性)
STOPWORDS = {
    "a", "an", "and", "are", "as", "at", "be", "been",
    "but", "by", "for", "from", "had", "has", "have", "he",
    "her", "his", "in", "is", "it", "its", "of", "on",
    "or", "she", "that", "the", "their", "them", "they",
    "this", "to", "was", "were", "will", "with",
}

WORD_SPLIT = re.compile(r"[^0-9a-z]+")


def content_words(text):
    """复现 contentWordSet:小写 → 按非字母数字切 → 去停用词 → 去重。"""
    folded = unicodedata.normalize("NFKD", text).lower()
    return {w for w in WORD_SPLIT.split(folded) if w and w not in STOPWORDS}


def fact_session_number(session_id):
    i = session_id.rfind("sess")
    if i < 0:
        return -1
    try:
        return int(session_id[i + 4:])
    except ValueError:
        return -1


def gold_turn_session(turn_id):
    inner = turn_id[1:] if turn_id.startswith("D") else turn_id
    sep = inner.find(":")
    if sep <= 0:
        return -1
    try:
        return int(inner[:sep])
    except ValueError:
        return -1


def fact_covers(fact_content, fact_session_id, gold_turn_text, gold_session):
    """复现 factCoversGoldTurn。"""
    if gold_session < 0 or fact_session_number(fact_session_id) != gold_session:
        return False
    fw = content_words(fact_content)
    tw = content_words(gold_turn_text)
    if not fw or not tw:
        return False
    return len(fw & tw) / len(fw) >= TAU


def load_conversations(path):
    """复现 dataset.go 的 session/turn 解析(不折 blip_caption)。"""
    convs = {}
    for cid, item in enumerate(json.load(open(path))):
        by_index = {}
        for key, val in item.get("conversation", {}).items():
            if not key.startswith("session_"):
                continue
            rest = key[len("session_"):]
            if "date_time" in rest:
                continue
            try:
                idx = int(rest)
            except ValueError:
                continue
            by_index[idx] = [
                (t.get("speaker", ""), t.get("text", ""), t.get("dia_id", ""))
                for t in val if t.get("text", "").strip()
            ]
        convs[cid] = {"id": cid, "sessions": dict(sorted(by_index.items()))}
    return convs


def build_session_chunks(turns):
    """复现 buildSessionChunks(soft target 900 / hard cap 1100,按 rune 计)。"""
    chunks, buf, dia_ids, size = [], [], [], 0
    for speaker, text, dia in turns:
        line = (speaker + ": " + text)[:CHUNK_MAX]
        n = len(line)
        if size > 0 and size + 1 + n > CHUNK_TARGET:
            chunks.append(dia_ids)
            buf, dia_ids, size = [], [], 0
        if size > 0:
            size += 1
        buf.append(line)
        size += n
        if dia:
            dia_ids.append(dia)
    if size > 0:
        chunks.append(dia_ids)
    return chunks


def chunk_turn_index(conv):
    """复现 probeChunkTurns:chunk 名 → 它覆盖的 DiaID 列表。"""
    out = {}
    for sidx, turns in conv["sessions"].items():
        for i, dia_ids in enumerate(build_session_chunks(turns)):
            if dia_ids:
                out["chunk-c%d-s%d-%03d" % (conv["id"], sidx, i)] = dia_ids
    return out


def turn_text_index(conv):
    """复现 turnTextIndex:speaker + " " + text(抽取会把第一人称解析成说话人)。"""
    out = {}
    for turns in conv["sessions"].values():
        for speaker, text, dia in turns:
            if dia:
                out[dia] = speaker + " " + text
    return out


def load_store(store_dir, conv_id):
    db = sqlite3.connect("%s/conv%d.db" % (store_dir, conv_id))
    try:
        return list(db.execute(
            "select name, content, category, source_session_id from memory_entries"))
    finally:
        db.close()


def covering_entries(rows, chunk_turns, turn_text, golds):
    """整店里所有覆盖 gold turn 的条目 → [(name, category, mapped_turns)]。"""
    hits = []
    for name, content, category, sess in rows:
        if name in chunk_turns:
            mapped = [g for g in golds if g in chunk_turns[name]]
        else:
            mapped = [g for g in golds
                      if fact_covers(content, sess, turn_text.get(g, ""), gold_turn_session(g))]
        if mapped:
            hits.append((name, category, mapped))
    return hits


def main():
    if len(sys.argv) != 9:
        print(__doc__)
        return 2
    (data, trace_path, base_store, alt_store,
     results_glob, out_path, target_category, lane_category) = sys.argv[1:]

    convs = load_conversations(data)
    traces = {(r["conv"], r["q"]): r for r in (json.loads(l) for l in open(trace_path))}

    rep_paths = sorted(glob.glob(results_glob))
    votes = defaultdict(list)
    for p in rep_paths:
        for line in open(p):
            row = json.loads(line)
            votes[(row["conv"], row["q"])].append(bool(row["correct"]))
    if not votes:
        print("!! --results glob 没匹配到任何逐题结果,无法判定答错题")
        return 1
    maj = {k: sum(v) * 2 > len(v) for k, v in votes.items()}
    print("答题产物 reps=%d  题数=%d" % (len(rep_paths), len(maj)))

    ct_cache, tt_cache, base_cache, alt_cache = {}, {}, {}, {}

    def ctx(cid):
        if cid not in ct_cache:
            ct_cache[cid] = chunk_turn_index(convs[cid])
            tt_cache[cid] = turn_text_index(convs[cid])
            base_cache[cid] = load_store(base_store, cid)
            alt_cache[cid] = load_store(alt_store, cid)
        return ct_cache[cid], tt_cache[cid]

    # ---- 自校验:复现判据必须重现 trace 已记录的 covers_gold ----
    checked = mismatch = 0
    for (cid, q), tr in sorted(traces.items()):
        golds = tr["gold_turns"]
        if not golds:
            continue
        ct, tt = ctx(cid)
        index = {n: (c, s) for n, c, _cat, s in base_cache[cid]}
        for hit in tr["retrieved"]:
            name = hit["name"]
            if name not in index:
                continue
            content, sess = index[name]
            if name in ct:
                mine = [g for g in golds if g in ct[name]]
            else:
                mine = [g for g in golds
                        if fact_covers(content, sess, tt.get(g, ""), gold_turn_session(g))]
            checked += 1
            if bool(mine) != bool(hit["covers_gold"]):
                mismatch += 1
                if mismatch <= 5:
                    print("  MISMATCH conv=%d q=%d %s mine=%s trace=%s"
                          % (cid, q, name, mine, hit["mapped_gold_turns"]))
    print("自校验: 比对 %d 条 top-30 命中, 不一致 %d 条 (%.4f%%)"
          % (checked, mismatch, 100.0 * mismatch / max(checked, 1)))
    if mismatch > checked * 0.01:
        print("!! 复现判据与 bench 不一致超过 1%,中止 —— 不拿假判据下结论")
        return 1

    # ---- 目标题:指定类别 × 多数投票答错 × gold 未进 top-30 ----
    targets = sorted(
        (cid, q) for (cid, q), tr in traces.items()
        if tr["category_name"] == target_category
        and not maj.get((cid, q), True)
        and not any(h["covers_gold"] for h in tr["retrieved"])
    )
    total_questions = len(traces)
    print("\n目标题(%s × 答错 × gold 未进 top-30) = %d 题" % (target_category, len(targets)))
    if not targets:
        return 0

    summary, detail = Counter(), []
    for cid, q in targets:
        ct, tt = ctx(cid)
        golds = traces[(cid, q)]["gold_turns"]
        row = {"conv": cid, "q": q, "golds": golds,
               "pool_rank": traces[(cid, q)]["gold_rank_pool"]}
        for label, rows in (("base", base_cache[cid]), ("alt", alt_cache[cid])):
            hits = covering_entries(rows, ct, tt, golds)
            row[label] = {"n": len(hits), "cats": dict(Counter(c for _n, c, _m in hits))}
        detail.append(row)
        summary["base_any"] += row["base"]["n"] > 0
        summary["base_lane"] += row["base"]["cats"].get(lane_category, 0) > 0
        summary["alt_any"] += row["alt"]["n"] > 0
        summary["alt_lane"] += row["alt"]["cats"].get(lane_category, 0) > 0
        summary["nothing"] += row["base"]["n"] == 0 and row["alt"]["n"] == 0

    n = len(targets)
    pct = lambda k: 100.0 * summary[k] / n
    print("\n" + "=" * 74)
    print("整店 oracle 覆盖(分母 = %d 道目标题,全库 %d 题)" % (n, total_questions))
    print("  base 店存在覆盖 gold 的条目             : %3d (%.1f%%)" % (summary["base_any"], pct("base_any")))
    print("    其中 %-10s 类(= 检索道能触及)   : %3d (%.1f%%)  → oracle 上限 %.2fpp overall"
          % (lane_category, summary["base_lane"], pct("base_lane"),
             100.0 * summary["base_lane"] / total_questions))
    print("  alt  店存在覆盖 gold 的条目             : %3d (%.1f%%)" % (summary["alt_any"], pct("alt_any")))
    print("    其中 %-10s 类                   : %3d (%.1f%%)  → oracle 上限 %.2fpp overall"
          % (lane_category, summary["alt_lane"], pct("alt_lane"),
             100.0 * summary["alt_lane"] / total_questions))
    print("  两个店都无覆盖条目(抽取缺口)            : %3d (%.1f%%)" % (summary["nothing"], pct("nothing")))
    print("=" * 74)

    print("\n逐题明细:  conv/q  pool_rank | base(n, cats) | alt(n, cats)")
    for r in detail:
        print("  %2d/%-4d %8s | %2d %-44s | %2d %s"
              % (r["conv"], r["q"], r["pool_rank"],
                 r["base"]["n"], json.dumps(r["base"]["cats"], sort_keys=True),
                 r["alt"]["n"], json.dumps(r["alt"]["cats"], sort_keys=True)))

    json.dump(detail, open(out_path, "w"), indent=1)
    print("\n明细写入 %s" % out_path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
