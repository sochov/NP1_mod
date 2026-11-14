"""Per-plate reading and wrong-answer rates over an evaluation kit (every frame).

For each frame the boxes of all compared models are clustered (IoU >= 0.3). A cluster is
a plate that should have been read when any model's reading in it belongs to a candidate
group verified as ok; that serial is the truth. Per model:

  detection rate = plates with a box of the model / plates that should have been read
  reading rate   = plates read with the true serial / plates that should have been read
  wrong rate     = answers with a wrong serial / answers given (all readings of the model)
                   an answer at a plate is wrong when it differs from the truth; any other
                   answer is judged by its own candidate group (bad = wrong; unk and
                   unverified are counted as unknown and stay in the denominator)

Plates that no compared model read correctly are not in the reference, so the rates are
relative to the union of the compared models.

    python tools/plate/kit_rates.py --kit DATA/eval-kit --models v1b v6b v6 v6b-d3
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from eval_engine import box_iou  # noqa: E402
from eval_video import load_jsonl  # noqa: E402

BINS = [(0, 40), (40, 70), (70, 110), (110, 10000)]


def xyxy(b):
    return [b[0], b[1], b[0] + b[2], b[1] + b[3]]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--kit", type=Path, required=True)
    ap.add_argument("--models", nargs="+", required=True)
    ap.add_argument("--datasets", nargs="*")
    a = ap.parse_args()
    datasets = a.datasets or sorted(p.name for p in (a.kit / "datasets").iterdir() if (p / "gt" / "verdicts.json").exists())
    stats = {m: defaultdict(lambda: defaultdict(int)) for m in a.models}  # model -> scope -> counters
    for ds in datasets:
        gt = a.kit / "datasets" / ds / "gt"
        cands = json.loads((gt / "candidates.json").read_text(encoding="utf-8"))
        verdicts = json.loads((gt / "verdicts.json").read_text(encoding="utf-8"))
        groups = defaultdict(list)
        for c in cands:
            groups[c["number"]].append((c["first"], c["last"], verdicts.get(c["id"], {}).get("verdict", "unverified")))

        def verdict(num, t):
            for f0, f1, v in groups.get(num, []):
                if f0 - 60 <= t <= f1 + 60:
                    return v
            return "unverified"

        runs = {m: {r["file"]: r for r in load_jsonl([str(a.kit / "engine-outputs" / ds / f"{m}.jsonl")])} for m in a.models}
        for f in runs[a.models[0]]:
            t = runs[a.models[0]][f]["t"]
            dets = [(m, i, p) for m in a.models for i, p in enumerate(runs[m][f]["plates"]) if p["reading"]["number"]]
            # cluster boxes across models
            clusters = []
            for d in dets:
                for c in clusters:
                    if any(box_iou(xyxy(d[2]["box"]), xyxy(o[2]["box"])) >= 0.3 for o in c):
                        c.append(d)
                        break
                else:
                    clusters.append([d])
            truth_of = {}
            for ci, c in enumerate(clusters):
                ok = Counter(d[2]["reading"]["number"] for d in c if verdict(d[2]["reading"]["number"], t) == "ok")
                if ok:
                    truth_of[ci] = ok.most_common(1)[0][0]
            for ci, truth in truth_of.items():
                width = max(d[2]["box"][2] for d in clusters[ci])
                b = next(bb for bb in BINS if bb[0] <= width < bb[1])
                for m in a.models:
                    mine = [d for d in clusters[ci] if d[0] == m]
                    for scope in ("all", ds, f"{b[0]}-{b[1] if b[1] < 10000 else ''}px"):
                        s = stats[m][scope]
                        s["should"] += 1
                        if mine:
                            s["detected"] += 1
                            best = max(mine, key=lambda d: d[2]["reading"]["confidence"])
                            if best[2]["reading"]["number"] == truth:
                                s["correct"] += 1
                                if best[2]["reading"]["confidence"] >= 0.6:
                                    s["correct06"] += 1
            for ci, c in enumerate(clusters):
                for m, i, p in c:
                    num, conf = p["reading"]["number"], p["reading"]["confidence"]
                    if ci in truth_of:
                        v = "ok" if num == truth_of[ci] else "bad"
                    else:
                        v = verdict(num, t)
                        v = v if v in ("ok", "bad") else "unknown"
                    for scope in ("all", ds):
                        s = stats[m][scope]
                        s["answers"] += 1
                        s[f"ans_{v}"] += 1
                        if conf >= 0.6:
                            s["answers06"] += 1
                            s[f"ans06_{v}"] += 1
    out = {}
    for m in a.models:
        out[m] = {}
        for scope, s in stats[m].items():
            r = {"shouldRead": s["should"], "detected": s["detected"], "correct": s["correct"], "correct(conf>=0.6)": s["correct06"],
                 "detectionRate": round(s["detected"] / max(1, s["should"]), 3), "missRate": round(1 - s["detected"] / max(1, s["should"]), 3),
                 "readingRate": round(s["correct"] / max(1, s["should"]), 3), "readingRate(conf>=0.6)": round(s["correct06"] / max(1, s["should"]), 3)}
            if s["answers"]:
                r.update({"answers": s["answers"], "wrong": s["ans_bad"], "unknown": s["ans_unknown"], "wrongRate": round(s["ans_bad"] / s["answers"], 3),
                          "answers(conf>=0.6)": s["answers06"], "wrong(conf>=0.6)": s["ans06_bad"], "unknown(conf>=0.6)": s["ans06_unknown"],
                          "wrongRate(conf>=0.6)": round(s["ans06_bad"] / max(1, s["answers06"]), 3)})
            out[m][scope] = r
    text = json.dumps(out, ensure_ascii=False, indent=1)
    (a.kit / "results").mkdir(exist_ok=True)
    (a.kit / "results" / "rates.json").write_text(text, encoding="utf-8")
    print(text)


if __name__ == "__main__":
    main()
