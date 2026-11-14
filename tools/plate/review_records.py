"""Contact sheets for checking engine records by eye.

Replays the device store logic (eval_video.simulate) on engine output and, for
each record of the chosen kind, crops the plate from the frame where it was read
with the highest confidence and writes it enlarged next to the reading. Sheets
contain other people's plates: write them outside the repository.

    python tools/plate/review_records.py --engine DATA/np1-1113/engine-v1b.jsonl \
        --teacher DATA/np1-1113/labels/teacher-*.jsonl --frames DATA/np1-1113/frames \
        --policy min2 --kind unconfirmed --out DATA/review/v1b-min2
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFont

sys.path.insert(0, str(Path(__file__).resolve().parent))
from eval_video import confident, frame_time, load_jsonl, simulate  # noqa: E402

POLICIES = {"min2": {"min": 2}, "min1": {"min": 1}}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--engine", required=True)
    ap.add_argument("--teacher", nargs="+", required=True)
    ap.add_argument("--frames", required=True)
    ap.add_argument("--policy", default="min2", help="min2 | min1 | instant<tau>")
    ap.add_argument("--kind", default="unconfirmed", choices=["unconfirmed", "confirmed", "all", "single"])
    ap.add_argument("--min-conf", type=float, default=0.6)
    ap.add_argument("--out", required=True)
    ap.add_argument("--fps", type=float, default=2.0)
    ap.add_argument("--instant-width", type=int, default=45)
    a = ap.parse_args()
    pol = POLICIES.get(a.policy) or {"min": 2, "instant": float(a.policy.replace("instant", "")), "width": a.instant_width}
    engine = load_jsonl([a.engine])
    teacher = {r["file"]: r for r in load_jsonl(a.teacher) if "plates" in r}
    tread = {}
    for f, tr in teacher.items():
        for t in tr["plates"]:
            if confident(t):
                tread.setdefault(str(t["number"]), []).append(frame_time(f, a.fps))
    reads = sorted((r["t"], p["reading"]["number"], p["reading"]["confidence"], p["box"][2]) for r in engine for p in r["plates"])
    by_read = {}
    for r in engine:
        for p in r["plates"]:
            by_read.setdefault(p["reading"]["number"], []).append((r["t"], p["reading"]["confidence"], r["file"], p))
    recs = simulate(reads, pol, a.min_conf)
    rows = []
    for rec in recs:
        ok = any(rec["first"] - 60 <= t <= rec["last"] + 60 for t in tread.get(rec["num"], []))
        if a.kind == "confirmed" and not ok or a.kind == "unconfirmed" and ok or a.kind == "single" and rec["n"] != 1:
            continue
        cands = [x for x in by_read[rec["num"]] if rec["first"] <= x[0] <= rec["last"]]
        t, conf, f, p = max(cands, key=lambda x: x[1])
        rows.append((rec, conf, f, p, ok))
    out = Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    try:
        font = ImageFont.truetype("C:/Windows/Fonts/YuGothB.ttc", 20)
    except Exception:
        font = ImageFont.load_default()
    cw, ch, per = 360, 150, 24
    index = []
    for s in range(0, len(rows), per):
        sheet = Image.new("RGB", (cw * 4, ch * ((min(per, len(rows) - s) + 3) // 4)), (30, 30, 30))
        d = ImageDraw.Draw(sheet)
        for k, (rec, conf, f, p, ok) in enumerate(rows[s:s + per]):
            img = cv2.imdecode(np.fromfile(str(Path(a.frames) / f), np.uint8), cv2.IMREAD_COLOR)
            x, y, w, h = p["box"]
            m = int(max(w, h) * 0.6)
            crop = img[max(0, y - m):y + h + m, max(0, x - m):x + w + m]
            crop = cv2.cvtColor(crop, cv2.COLOR_BGR2RGB)
            scale = min((cw - 10) / crop.shape[1], (ch - 34) / crop.shape[0])
            crop = cv2.resize(crop, (max(1, int(crop.shape[1] * scale)), max(1, int(crop.shape[0] * scale))), interpolation=cv2.INTER_CUBIC)
            X, Y = (k % 4) * cw, (k // 4) * ch
            sheet.paste(Image.fromarray(crop), (X + 5, Y + 30))
            r = p["reading"]
            label = f"#{s + k + 1} {r['region']}{r['class']}{r['kana']} {r['number']} c{conf:.2f} n{rec['n']} w{w}"
            d.text((X + 5, Y + 4), label, fill=(120, 255, 120) if ok else (255, 230, 80), font=font)
            index.append({"no": s + k + 1, "file": f, "box": p["box"], "reading": r, "conf": conf, "sightings": rec["n"], "teacherConfirmed": ok})
        sheet.save(out / f"sheet-{s // per + 1:02d}.jpg", quality=90)
    (out / "index.json").write_text(json.dumps(index, ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"{len(rows)} records -> {out}")


if __name__ == "__main__":
    main()
