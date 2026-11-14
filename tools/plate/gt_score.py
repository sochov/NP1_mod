"""Score engine runs against hand-verified candidates (gt_candidates.py + verdicts.json).

Every serial read at or above --min-conf belongs to one candidate group (same serial,
within 60 s), whose verdict is ok / bad / unk. Reported per engine run:

  one-shot reads   single-frame readings at each confidence threshold: correct (ok),
                   wrong (bad), unverifiable (unk), precision = ok / (ok + bad)
  one-shot cars    distinct verified-correct groups read at least once at the threshold
  records          the device store replayed under each policy (eval_video.simulate)

Reads that fall in no verified group are listed as "unverified" so new candidates can
be checked (rerun gt_candidates.py with the new engine file).

    python tools/plate/gt_score.py --gt DATA/np1-1113/gt --engine DATA/np1-1113/engine-v1b.jsonl
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from eval_video import load_jsonl, simulate  # noqa: E402


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--gt", required=True)
    ap.add_argument("--engine", nargs="+", required=True)
    ap.add_argument("--min-conf", type=float, default=0.5)
    ap.add_argument("--instant", type=float, nargs="*", default=[0.7, 0.8, 0.85, 0.9])
    ap.add_argument("--instant-width", type=int, default=45)
    ap.add_argument("--out")
    a = ap.parse_args()
    gt = Path(a.gt)
    cands = json.loads((gt / "candidates.json").read_text(encoding="utf-8"))
    verdicts = json.loads((gt / "verdicts.json").read_text(encoding="utf-8"))
    by_num = {}
    for c in cands:
        c["verdict"] = verdicts.get(c["id"], {}).get("verdict", "unverified")
        by_num.setdefault(c["number"], []).append(c)

    def group(num, t0, t1=None):
        t1 = t0 if t1 is None else t1
        for c in by_num.get(num, []):
            if c["first"] - 60 <= t1 and t0 <= c["last"] + 60:
                return c
        return None

    report = {}
    for path in a.engine:
        rows = [r for r in load_jsonl([path])]
        reads = [(r["t"], p["reading"]["number"], p["reading"]["confidence"], p["box"][2]) for r in rows for p in r["plates"] if p["reading"]["number"]]
        res = {"oneShotReads": {}, "oneShotCars": {}, "records": {}}
        for thr in (0.5, 0.6, 0.7, 0.8, 0.9):
            for minw in (0, 45):
                cnt = {"ok": 0, "bad": 0, "unk": 0, "unverified": 0}
                cars = set()
                for t, num, conf, w in reads:
                    if conf < thr or w < minw:
                        continue
                    g = group(num, t)
                    v = g["verdict"] if g else "unverified"
                    cnt[v] = cnt.get(v, 0) + 1
                    if v == "ok":
                        cars.add(g["id"])
                key = f"conf>={thr}" + (f" w>={minw}" if minw else "")
                cnt["precision"] = round(cnt["ok"] / max(1, cnt["ok"] + cnt["bad"]), 3)
                res["oneShotReads"][key] = cnt
                res["oneShotCars"][key] = len(cars)
        policies = {"min2": {"min": 2}, "min1": {"min": 1}}
        for tau in a.instant:
            policies[f"min2+instant{tau}"] = {"min": 2, "instant": tau, "width": a.instant_width}
        for name, pol in policies.items():
            recs = simulate(sorted(reads), pol)
            cnt = {"ok": 0, "bad": 0, "unk": 0, "unverified": 0}
            cars = set()
            for r in recs:
                g = group(r["num"], r["first"], r["last"])
                v = g["verdict"] if g else "unverified"
                cnt[v] = cnt.get(v, 0) + 1
                if v == "ok":
                    cars.add(g["id"])
            cnt["records"] = len(recs)
            cnt["correctCars"] = len(cars)
            cnt["precision"] = round(cnt["ok"] / max(1, cnt["ok"] + cnt["bad"]), 3)
            res["records"][name] = cnt
        report[Path(path).stem] = res
    total_ok = sum(1 for c in cands if c["verdict"] == "ok")
    out = {"verifiedCorrectGroups": total_ok, "runs": report}
    text = json.dumps(out, ensure_ascii=False, indent=1)
    print(text)
    if a.out:
        Path(a.out).write_text(text, encoding="utf-8")


if __name__ == "__main__":
    main()
