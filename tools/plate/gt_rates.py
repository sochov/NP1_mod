"""Per-plate reading rate and wrong-answer rate on frames that a teacher labeled.

Reference plates come from an independent detector (Lipla-jp teacher boxes): every
teacher box whose serial can be fixed from the hand verdicts (gt_candidates.py +
verdicts.json) is a plate the engine "should have read". A box gets its true serial
when the teacher's reading or any compared model's reading at that box belongs to a
candidate group verified as ok.

  reading rate  = plates read correctly / plates that should have been read
  wrong rate    = answers with a wrong serial / answers given (every engine reading)
  detection     = plates with any engine box (IoU >= 0.3) / plates that should have been read

An engine answer at a reference plate is right when its serial equals the plate's true
serial; an answer elsewhere is judged by its own candidate group (ok / bad / unk).

    python tools/plate/gt_rates.py --set NAME FRAMES_ROOT TEACHER_GLOB GT_DIR TIME_MODE \
        --engine v1b=PATH v6b=PATH ...
TIME_MODE: "fps2" (frames <clip>/f_%05d.jpg at 2 fps) or "every2" (<cam>/<YYYYMMDD_HHMMSS>X_%04d.jpg, one
frame per 2 s, clip names mapped to the kit's <cam>_<YYMMDD>_<HHMMSS>).
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from eval_engine import box_iou  # noqa: E402
from eval_video import frame_time, load_jsonl, teacher_box  # noqa: E402

ROI = (320, 240, 1280, 768)
BINS = [(0, 40), (40, 70), (70, 110), (110, 10000)]


def time_of(file: str, mode: str) -> float:
    if mode == "fps2":
        return frame_time(file, 2.0)
    cam, name = file.split("/")[-2:]
    m = re.match(r"(\d{8})_(\d{6})\w*_(\d{4})\.jpg$", name)
    hh, mm, ss = int(m.group(2)[:2]), int(m.group(2)[2:4]), int(m.group(2)[4:])
    return hh * 3600 + mm * 60 + ss + (int(m.group(3)) - 1) * 2.0


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--set", nargs=5, action="append", required=True, metavar=("NAME", "FRAMES", "TEACHER", "GT", "MODE"))
    ap.add_argument("--engine", nargs="+", required=True, help="model=engine-jsonl-for-set1,engine-jsonl-for-set2,...")
    ap.add_argument("--needs", help="write teacher plates whose serial is not fixed yet here (json, for checking by eye)")
    ap.add_argument("--truth", help="hand transcriptions: json {key: serial | \"unreadable\"}, key = set|file|x0,y0")
    ap.add_argument("--out")
    a = ap.parse_args()
    models = {}
    for spec in a.engine:
        name, paths = spec.split("=", 1)
        models[name] = paths.split(",")
    res = {m: defaultdict(int) for m in models}
    by_width = {m: {b: defaultdict(int) for b in BINS} for m in models}
    needs, excluded = [], defaultdict(int)
    truth_file = json.loads(Path(a.truth).read_text(encoding="utf-8")) if a.truth and Path(a.truth).exists() else {}
    for si, (name, frames, teacher_glob, gt_dir, mode) in enumerate(a.set):
        cands = json.loads((Path(gt_dir) / "candidates.json").read_text(encoding="utf-8"))
        verdicts = json.loads((Path(gt_dir) / "verdicts.json").read_text(encoding="utf-8"))
        groups = defaultdict(list)
        for c in cands:
            groups[c["number"]].append((c["first"], c["last"], verdicts.get(c["id"], {}).get("verdict", "unverified")))

        def verdict(num, t):
            for f0, f1, v in groups.get(num, []):
                if f0 - 60 <= t <= f1 + 60:
                    return v
            return "unverified"

        engines = {m: {r["file"]: r for r in load_jsonl([paths[si]])} for m, paths in models.items()}
        for tr in load_jsonl([teacher_glob]):
            if "plates" not in tr:
                continue
            f = tr["file"]
            if any(f not in engines[m] for m in models):
                continue
            t = time_of(f, mode)
            refs = []
            for p in tr["plates"]:
                if p["score"] < 0.5:
                    continue
                tb = teacher_box(p)
                nums = []
                if not p.get("unread") and str(p.get("number", "")).isdigit():
                    nums.append(str(p["number"]))
                for m in models:
                    for q in engines[m][f]["plates"]:
                        b = q["box"]
                        if q["reading"]["number"] and box_iou([b[0], b[1], b[0] + b[2], b[1] + b[3]], tb) >= 0.3:
                            nums.append(q["reading"]["number"])
                ok = [n for n in dict.fromkeys(nums) if verdict(n, t) == "ok"]
                width = float(tb[2] - tb[0])
                key = f"{name}|{f}|{int(tb[0])},{int(tb[1])}"
                if key in truth_file:  # transcribed by eye
                    if truth_file[key] != "unreadable":
                        refs.append((tb, truth_file[key], width))
                    else:
                        excluded["unreadable by eye"] += 1
                elif ok:
                    refs.append((tb, ok[0], width))
                else:
                    kind = "no verified serial" if nums else "nothing read"
                    needs.append({"key": key, "kind": kind, "set": name, "frames": frames, "file": f, "reads": list(dict.fromkeys(nums)), "width": width, "box": [float(v) for v in tb]})
                    excluded[kind] += 1
            for m in models:
                plates = engines[m][f]["plates"]
                used = set()
                for tb, truth, width in refs:
                    b = next(bb for bb in BINS if bb[0] <= width < bb[1])
                    cx, cy = (tb[0] + tb[2]) / 2, (tb[1] + tb[3]) / 2
                    in_roi = int(bool(ROI[0] <= cx <= ROI[0] + ROI[2] and ROI[1] <= cy <= ROI[1] + ROI[3]))
                    for d in (res[m], by_width[m][b]):
                        d["should"] += 1
                        d["should_roi"] += in_roi
                    best = max(range(len(plates)), key=lambda i: box_iou([plates[i]["box"][0], plates[i]["box"][1], plates[i]["box"][0] + plates[i]["box"][2], plates[i]["box"][1] + plates[i]["box"][3]], tb), default=None)
                    if best is None:
                        continue
                    pb = plates[best]["box"]
                    if box_iou([pb[0], pb[1], pb[0] + pb[2], pb[1] + pb[3]], tb) < 0.3:
                        continue
                    used.add(best)
                    rd = plates[best]["reading"]
                    for d in (res[m], by_width[m][b]):
                        d["detected"] += 1
                        d["detected_roi"] += in_roi
                        if rd["number"] == truth:
                            d["correct_any"] += 1
                            if rd["confidence"] >= 0.6:
                                d["correct_06"] += 1
                # every answer the engine gave in this frame
                for i, q in enumerate(plates):
                    rd = q["reading"]
                    if not rd["number"]:
                        continue
                    if i in used:
                        tb = next(r for r in refs if box_iou([q["box"][0], q["box"][1], q["box"][0] + q["box"][2], q["box"][1] + q["box"][3]], r[0]) >= 0.3)
                        v = "ok" if rd["number"] == tb[1] else "bad"
                    else:
                        v = verdict(rd["number"], t)
                    for suffix, keep in (("any", True), ("06", rd["confidence"] >= 0.6)):
                        if keep:
                            res[m][f"answers_{suffix}"] += 1
                            res[m][f"{v}_{suffix}"] += 1
    out = {"excludedTeacherPlates": dict(excluded), "models": {}}
    for m in models:
        r = res[m]
        out["models"][m] = {
            "shouldRead": r["should"], "shouldReadInROI": r["should_roi"],
            "detectionRate": round(r["detected"] / max(1, r["should"]), 3), "detectionRateInROI": round(r["detected_roi"] / max(1, r["should_roi"]), 3),
            "readingRate(any conf)": round(r["correct_any"] / max(1, r["should"]), 3), "readingRate(conf>=0.6)": round(r["correct_06"] / max(1, r["should"]), 3),
            "answers(any)": r["answers_any"], "wrong(any)": r["bad_any"], "unknown(any)": r["unk_any"] + r["unverified_any"],
            "wrongRate(any)": round(r["bad_any"] / max(1, r["answers_any"]), 3),
            "answers(conf>=0.6)": r["answers_06"], "wrong(conf>=0.6)": r["bad_06"], "unknown(conf>=0.6)": r["unk_06"] + r["unverified_06"],
            "wrongRate(conf>=0.6)": round(r["bad_06"] / max(1, r["answers_06"]), 3),
            "byWidth": {f"{lo}-{hi if hi < 10000 else ''}px": dict(by_width[m][(lo, hi)]) for lo, hi in BINS},
        }
    text = json.dumps(out, ensure_ascii=False, indent=1)
    print(text)
    if a.out:
        Path(a.out).write_text(text, encoding="utf-8")
    if a.needs:
        Path(a.needs).write_text(json.dumps(needs, ensure_ascii=False, indent=1), encoding="utf-8")
        print(f"{len(needs)} teacher plates need a verdict -> {a.needs}", file=sys.stderr)


if __name__ == "__main__":
    main()
