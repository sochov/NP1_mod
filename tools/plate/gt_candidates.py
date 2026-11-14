"""Build hand-verification candidates for a clip set (private output).

Every serial read by the teacher or by any engine run (confidence >= --min-conf) is
grouped by serial and time (gap > 60 s starts a new group). For each group the
widest, most confident sighting is cropped onto contact sheets, so a person can
mark each group as correct / wrong / unreadable in verdicts.json. The verified
table then scores any model and confirmation policy (eval_video.py score --gt).

    python tools/plate/gt_candidates.py --frames DATA/np1-1113/frames \
        --teacher DATA/np1-1113/labels/teacher-*.jsonl \
        --engine DATA/np1-1113/engine-v1b.jsonl DATA/np1-1113/engine-v6b.jsonl \
        --out DATA/np1-1113/gt
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
from eval_video import confident, frame_time, load_jsonl, teacher_box  # noqa: E402


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--frames", required=True)
    ap.add_argument("--teacher", nargs="+", required=True)
    ap.add_argument("--engine", nargs="+", required=True)
    ap.add_argument("--min-conf", type=float, default=0.5)
    ap.add_argument("--fps", type=float, default=2.0)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    out = Path(a.out)
    out.mkdir(parents=True, exist_ok=True)
    known = {}
    if (out / "candidates.json").exists():
        known = {c["id"]: c for c in json.loads((out / "candidates.json").read_text(encoding="utf-8"))}
    sightings = []  # (t, number, score, file, xyxy, source)
    for r in load_jsonl(a.teacher):
        for t in r.get("plates", []):
            if confident(t):
                sightings.append((frame_time(r["file"], a.fps), str(t["number"]), t["numberScore"], r["file"], teacher_box(t), "teacher"))
    for path in a.engine:
        tag = Path(path).stem
        for r in load_jsonl([path]):
            for p in r["plates"]:
                rd = p["reading"]
                if rd["number"] and rd["confidence"] >= a.min_conf:
                    x, y, w, h = p["box"]
                    sightings.append((r["t"], rd["number"], rd["confidence"], r["file"], [x, y, x + w, y + h], tag))
    groups = {}
    for s in sorted(sightings):
        g = groups.setdefault(s[1], [])
        if g and s[0] - g[-1][-1][0] <= 60:
            g[-1].append(s)
        else:
            g.append([s])
    cands = []
    for num, gs in groups.items():
        for g in gs:
            cid = f"{num}@{int(g[0][0])}"
            # A group continuing an already verified one keeps its id (new sightings, same car).
            prev = next((k for k, c in known.items() if c["number"] == num and c["first"] - 60 <= g[-1][0] and g[0][0] <= c["last"] + 60), None)
            best = max(g, key=lambda s: ((s[4][2] - s[4][0]) * (0.5 + s[2]), s[2]))
            cands.append({"id": prev or cid, "number": num, "first": g[0][0], "last": g[-1][0], "sources": sorted({s[5] for s in g}),
                          "best": {"file": best[3], "box": [float(v) for v in best[4]], "score": best[2], "source": best[5]}, "n": len(g)})
    cands.sort(key=lambda c: c["first"])
    (out / "candidates.json").write_text(json.dumps(cands, ensure_ascii=False, indent=1), encoding="utf-8")
    verdicts = json.loads((out / "verdicts.json").read_text(encoding="utf-8")) if (out / "verdicts.json").exists() else {}
    todo = [c for c in cands if c["id"] not in verdicts]
    font = ImageFont.truetype("C:/Windows/Fonts/YuGothB.ttc", 20)
    cw, ch, per = 360, 170, 24
    for old in out.glob("todo-*.jpg"):
        old.unlink()
    for s in range(0, len(todo), per):
        part = todo[s:s + per]
        sheet = Image.new("RGB", (cw * 4, ch * ((len(part) + 3) // 4)), (30, 30, 30))
        d = ImageDraw.Draw(sheet)
        for k, c in enumerate(part):
            img = cv2.imdecode(np.fromfile(str(Path(a.frames) / c["best"]["file"]), np.uint8), cv2.IMREAD_COLOR)
            x0, y0, x1, y1 = [int(v) for v in c["best"]["box"]]
            m = int(max(x1 - x0, 20) * 0.5)
            crop = cv2.cvtColor(img[max(0, y0 - m):y1 + m, max(0, x0 - m):x1 + m], cv2.COLOR_BGR2RGB)
            sc = min((cw - 10) / crop.shape[1], (ch - 30) / crop.shape[0])
            crop = cv2.resize(crop, (max(1, int(crop.shape[1] * sc)), max(1, int(crop.shape[0] * sc))), interpolation=cv2.INTER_CUBIC)
            X, Y = (k % 4) * cw, (k // 4) * ch
            sheet.paste(Image.fromarray(crop), (X + 5, Y + 28))
            d.text((X + 4, Y + 3), f"{s + k + 1}. {c['number']}  w{x1 - x0} n{c['n']}", fill=(255, 230, 80), font=font)
        sheet.save(out / f"todo-{s // per + 1:02d}.jpg", quality=90)
    (out / "todo.json").write_text(json.dumps([{"no": i + 1, "id": c["id"], "number": c["number"]} for i, c in enumerate(todo)], ensure_ascii=False, indent=1), encoding="utf-8")
    print(f"{len(cands)} candidate groups, {len(todo)} to verify")


if __name__ == "__main__":
    main()
