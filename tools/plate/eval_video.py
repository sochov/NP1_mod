"""Evaluate the plate engine on frames extracted from dash-camera clips.

Frames are expected as <root>/<clip>/f_00001.jpg ... extracted at --fps from each
clip (ffmpeg -vf fps=N). The clip directory name must contain HHMMSS after the
date (e.g. FILEM_261113_172833_0000) so that times can be compared across clips.

    # 1) run a model package over every frame (x86_64 engine in WSL)
    python tools/plate/eval_video.py run --frames DATA/np1-1113/frames \
        --package DATA/package-v1b --out DATA/np1-1113/engine-v1b.jsonl

    # 2) score against Lipla teacher labels (any number of jsonl files)
    python tools/plate/eval_video.py score --engine DATA/np1-1113/engine-v1b.jsonl \
        --teacher DATA/np1-1113/labels/teacher-*.jsonl

Frame metrics use confident teacher plates (score >= 0.75, number >= 0.9) as
references. Record metrics replay the device store logic (readable serial,
confidence >= 0.6, same serial within 30 s is one vehicle) under several
confirmation policies and check each record against the teacher: a record is
"confirmed" when the teacher read the same serial within +-60 s.
"""
from __future__ import annotations

import argparse
import glob
import json
import re
import sys
import time
from collections import defaultdict
from pathlib import Path

import cv2
import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parent))
from eval_engine import EngineProcess, rgb_to_nv12, W, H, box_iou  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
TEACHER_PLATE, TEACHER_NUMBER = 0.75, 0.9


def clip_start(name: str) -> float:
    m = re.search(r"_(\d{6})_(\d{6})", name)
    if not m:
        raise ValueError(f"no time in clip name: {name}")
    hh, mm, ss = int(m.group(2)[:2]), int(m.group(2)[2:4]), int(m.group(2)[4:])
    return hh * 3600 + mm * 60 + ss


def frame_time(rel: str, fps: float) -> float:
    clip, name = rel.split("/")[-2:]
    k = int(re.search(r"(\d+)", name).group(1))
    return clip_start(clip) + (k - 1) / fps


def cmd_run(a):
    files = sorted(Path(a.frames).rglob(a.glob))
    if a.labels:  # only frames that the teacher labeled
        wanted = {r["file"] for r in load_jsonl(a.labels)}
        files = [p for p in files if p.relative_to(a.frames).as_posix() in wanted]
    done = set()
    out = Path(a.out)
    if out.exists():
        done = {json.loads(l)["file"] for l in out.read_text(encoding="utf-8").splitlines() if l.strip()}
    pkg = Path(a.package)
    config = Path(a.config) if a.config else pkg / "plate-model.json"
    engine = EngineProcess(ROOT / "artifacts/plate/plate-engine-host", pkg, config, host=True)
    t0, n = time.time(), 0
    with out.open("a", encoding="utf-8") as f:
        for p in files:
            rel = p.relative_to(a.frames).as_posix()
            if rel in done:
                continue
            bgr = cv2.imread(str(p))
            if bgr.shape[:2] != (H, W):
                bgr = cv2.resize(bgr, (W, H), interpolation=cv2.INTER_AREA)
            reply = engine.run(rgb_to_nv12(cv2.cvtColor(bgr, cv2.COLOR_BGR2RGB)))
            try:
                t = round(frame_time(rel, a.fps), 2)
            except (ValueError, AttributeError):
                t = None
            f.write(json.dumps({"file": rel, "t": t, "plates": reply["plates"], "ms": round(reply.get("ms", 0), 1)}, ensure_ascii=False) + "\n")
            n += 1
            if n % 500 == 0:
                print(f"{n} frames, {(time.time() - t0) / n * 1000:.0f} ms/frame", flush=True)
    engine.close()
    print(f"done {n} frames", flush=True)


def load_jsonl(paths):
    rows = []
    for pat in paths:
        for p in glob.glob(pat):
            rows += [json.loads(l) for l in open(p, encoding="utf-8") if l.strip()]
    return rows


def teacher_box(t):
    q = np.asarray(t["quad"], np.float32)
    return [*q.min(0), *q.max(0)]


def confident(t):
    return not t.get("unread") and t["score"] >= TEACHER_PLATE and t.get("numberScore", 0) >= TEACHER_NUMBER and str(t.get("number", "")).isdigit()


def simulate(reads, policy, min_conf=0.6, window=30.0):
    """reads: (t, number, conf[, width]) sorted by t. Returns record dicts (first, last, num, n).
    policy: min = sightings needed; instant = confidence that records at once when the
    plate is at least policy["width"] px wide (device: plate.InstantMinWidth)."""
    open_, cand, records = {}, {}, []
    for t, num, conf, *rest in reads:
        width = rest[0] if rest else 10000
        if not num or conf < min_conf:
            continue
        for d in (open_, cand):
            for k in [k for k, r in d.items() if t - r["last"] > window]:
                if d is open_:
                    records.append(open_.pop(k))
                else:
                    cand.pop(k)
        if num in open_:
            open_[num]["last"] = t
            open_[num]["n"] += 1
            continue
        r = cand.setdefault(num, {"first": t, "last": t, "num": num, "n": 0, "max": 0})
        r["last"] = t
        r["n"] += 1
        r["max"] = max(r["max"], conf)
        need = policy.get("min", 2)
        if r["n"] >= need or (conf >= policy.get("instant", 2.0) and width >= policy.get("width", 0)):
            open_[num] = cand.pop(num)
    records += list(open_.values())
    return records


def cmd_score(a):
    engine = {r["file"]: r for r in load_jsonl([a.engine])}
    teacher = {r["file"]: r for r in load_jsonl(a.teacher) if "plates" in r}
    roi = a.roi
    # ---------------- frame level (frames the teacher labeled)
    bins = [(0, 40), (40, 70), (70, 110), (110, 10000)]
    fr = {b: defaultdict(int) for b in bins}
    calib = defaultdict(lambda: [0, 0])  # conf bin -> [correct, total] on confident teacher plates
    cbins = [0.6, 0.8, 0.9, 0.95, 0.98, 0.99, 0.995, 1.01]
    other_reads = defaultdict(int)  # engine reads (conf >= 0.6) not matched to a confident teacher plate
    single = []  # (confidence, correct, engine box width) of every detected reference: one-shot reads
    frames = 0
    for f, tr in teacher.items():
        er = engine.get(f)
        if er is None:
            continue
        frames += 1
        reads = [(p, [p["box"][0], p["box"][1], p["box"][0] + p["box"][2], p["box"][1] + p["box"][3]]) for p in er["plates"]]
        matched = set()
        for t in tr["plates"]:
            tb = teacher_box(t)
            if not confident(t):
                continue
            cx, cy = (tb[0] + tb[2]) / 2, (tb[1] + tb[3]) / 2
            in_roi = int(bool(roi[0] <= cx <= roi[0] + roi[2] and roi[1] <= cy <= roi[1] + roi[3]))
            width = float(tb[2] - tb[0])
            b = next(bb for bb in bins if bb[0] <= width < bb[1])
            s = fr[b]
            s["refs"] += 1
            s["refs_roi"] += in_roi
            best = max(range(len(reads)), key=lambda i: box_iou(reads[i][1], tb), default=None)
            if best is None or box_iou(reads[best][1], tb) < 0.3:
                continue
            matched.add(best)
            r = reads[best][0]["reading"]
            s["det"] += 1
            s["det_roi"] += in_roi
            ok = r["number"] == str(t["number"])
            s["num_ok"] += ok
            single.append((r["confidence"], ok, float(reads[best][0]["box"][2])))
            if r["confidence"] >= 0.6:
                s["rec"] += 1
                s["rec_ok"] += ok
            ci = next(i for i in range(len(cbins) - 1) if cbins[i] <= r["confidence"] < cbins[i + 1]) if r["confidence"] >= 0.6 else None
            if ci is not None:
                calib[ci][0] += ok
                calib[ci][1] += 1
        for i, (p, _) in enumerate(reads):
            if i not in matched and p["reading"]["confidence"] >= 0.6 and p["reading"]["number"]:
                other_reads["unmatched"] += 1
    tot = defaultdict(int)
    for b in bins:
        for k, v in fr[b].items():
            tot[k] += v
    res = {"frames": frames, "byWidth": {f"{lo}-{hi if hi < 10000 else ''}px": dict(fr[(lo, hi)]) for lo, hi in bins}, "total": dict(tot),
           "detectionRecall": round(tot["det"] / max(1, tot["refs"]), 3), "detectionRecallROI": round(tot["det_roi"] / max(1, tot["refs_roi"]), 3),
           "numberAccuracyOfDetected": round(tot["num_ok"] / max(1, tot["det"]), 3), "recordedPrecision": round(tot["rec_ok"] / max(1, tot["rec"]), 3),
           "recordedRecall": round(tot["rec_ok"] / max(1, tot["refs"]), 3), "unmatchedConfidentReads": other_reads["unmatched"],
           "calibration": {f"{cbins[i]:.3f}-{cbins[i + 1]:.3f}": {"correct": c, "n": n, "acc": round(c / max(1, n), 3)} for i, (c, n) in sorted(calib.items())}}
    # One-shot: a single frame's reading of each reference, kept when its confidence is at
    # least thr (optionally only plates >= 45 px wide). "recall" is correct kept reads over refs.
    res["oneShot"] = {}
    for minw in (0, 45):
        for thr in (0.0, 0.6, 0.7, 0.8, 0.9):
            kept = [(c, ok) for c, ok, w in single if c >= thr and w >= minw]
            refs = sum(1 for _, _, w in single if w >= minw)
            good = sum(ok for _, ok in kept)
            res["oneShot"][f"w>={minw} conf>={thr}"] = {"kept": len(kept), "correct": good, "wrong": len(kept) - good,
                                                         "precision": round(good / max(1, len(kept)), 3), "recall": round(good / max(1, refs), 3)}
    if a.frames_only or any(r.get("t") is None for r in engine.values()):
        out = json.dumps(res, ensure_ascii=False, indent=1)
        print(out)
        if a.out:
            Path(a.out).write_text(out, encoding="utf-8")
        return
    # ---------------- record level (all engine frames, policies)
    tread = defaultdict(list)  # number -> teacher times
    for f, tr in teacher.items():
        for t in tr["plates"]:
            if confident(t):
                tread[str(t["number"])].append(engine.get(f, {}).get("t", frame_time(f, a.fps)))
    # vehicles: teacher sightings of one number split by gaps > 60 s
    vehicles = []
    for num, ts in tread.items():
        ts.sort()
        grp = [ts[0]]
        for t in ts[1:]:
            if t - grp[-1] > 60:
                vehicles.append((num, grp[0], grp[-1], len(grp)))
                grp = []
            grp.append(t)
        vehicles.append((num, grp[0], grp[-1], len(grp)))
    reads = sorted((r["t"], p["reading"]["number"], p["reading"]["confidence"], p["box"][2]) for r in engine.values() for p in r["plates"])
    policies = {"min2": {"min": 2}, "min1": {"min": 1}}
    for tau in a.instant:
        policies[f"min2+instant{tau}"] = {"min": 2, "instant": tau, "width": a.instant_width}
    res["records"] = {}
    for name, pol in policies.items():
        recs = simulate(reads, pol)
        conf = [r for r in recs if any(abs(t - r["first"]) <= 60 or abs(t - r["last"]) <= 60 or r["first"] <= t <= r["last"] for t in tread.get(r["num"], []))]
        got = [v for v in vehicles if any(r["num"] == v[0] and r["first"] - 60 <= v[2] and v[1] <= r["last"] + 60 for r in recs)]
        short = [v for v in vehicles if v[3] == 1]
        got_short = [v for v in got if v[3] == 1]
        res["records"][name] = {"records": len(recs), "teacherConfirmed": len(conf), "unconfirmed": len(recs) - len(conf),
                                "teacherVehicles": len(vehicles), "vehiclesRecorded": len(got), "vehicleRecall": round(len(got) / max(1, len(vehicles)), 3),
                                "shortVehicles(1 teacher frame)": len(short), "shortRecorded": len(got_short)}
    out = json.dumps(res, ensure_ascii=False, indent=1)
    print(out)
    if a.out:
        Path(a.out).write_text(out, encoding="utf-8")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    r = sub.add_parser("run")
    r.add_argument("--frames", required=True)
    r.add_argument("--package", required=True)
    r.add_argument("--config")
    r.add_argument("--out", required=True)
    r.add_argument("--fps", type=float, default=2.0)
    r.add_argument("--glob", default="f_*.jpg")
    r.add_argument("--labels", nargs="*", default=[], help="process only frames listed in these teacher label files")
    s = sub.add_parser("score")
    s.add_argument("--engine", required=True)
    s.add_argument("--teacher", nargs="+", required=True)
    s.add_argument("--fps", type=float, default=2.0)
    s.add_argument("--roi", type=float, nargs=4, default=[320, 240, 1280, 768])
    s.add_argument("--instant", type=float, nargs="*", default=[0.8, 0.85, 0.9, 0.95])
    s.add_argument("--instant-width", type=int, default=45)
    s.add_argument("--out")
    s.add_argument("--frames-only", action="store_true")
    a = ap.parse_args()
    {"run": cmd_run, "score": cmd_score}[a.cmd](a)


if __name__ == "__main__":
    main()
