"""Extract still frames from dash-camera clips for labeling and evaluation.

Frames are scaled to the NP1 export size (1920x1080) so plates appear at the
size the device will see. Clips are split by date into train / eval so the
evaluation footage is never used for training.

    python tools/plate/train/extract_frames.py --src "X:/.../front" --name cama \
        --until YYYYMMDD --eval-dates YYYYMMDD --every 2 --out O:/NP1_plate_data/dashcam
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path


def extract(clip: Path, out_dir: Path, every: float) -> int:
    out_dir.mkdir(parents=True, exist_ok=True)
    pattern = out_dir / f"{clip.stem}_%04d.jpg"
    if any(out_dir.glob(f"{clip.stem}_*.jpg")):
        return len(list(out_dir.glob(f"{clip.stem}_*.jpg")))
    cmd = ["ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-i", str(clip), "-map", "0:v:0",
           "-vf", f"fps=1/{every},scale=1920:1080:flags=area", "-q:v", "3", str(pattern)]
    subprocess.run(cmd, check=False)
    return len(list(out_dir.glob(f"{clip.stem}_*.jpg")))


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--src", type=Path, required=True)
    ap.add_argument("--name", required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--until", default="99999999", help="last date (YYYYMMDD) to include")
    ap.add_argument("--eval-dates", nargs="*", default=[])
    ap.add_argument("--eval-last", type=int, default=0, help="also hold out the last N clips")
    ap.add_argument("--every", type=float, default=2.0)
    ap.add_argument("--jobs", type=int, default=6)
    args = ap.parse_args()
    clips = sorted(p for p in args.src.iterdir() if p.suffix.lower() == ".mp4")
    dated = []
    for c in clips:
        m = re.match(r"(\d{8})_(\d{6})", c.name)
        if m and m.group(1) <= args.until:
            dated.append((m.group(1), c))
    eval_set = {c for d, c in dated if d in args.eval_dates}
    if args.eval_last:
        eval_set |= {c for _, c in dated[-args.eval_last:]}
    jobs = [(c, args.out / ("eval" if c in eval_set else "train") / args.name) for _, c in dated]
    with ThreadPoolExecutor(args.jobs) as pool:
        counts = list(pool.map(lambda j: extract(j[0], j[1], args.every), jobs))
    manifest = {"name": args.name, "every": args.every, "clips": [{"clip": str(c), "split": d.parent.name, "frames": n} for (c, d), n in zip(jobs, counts)]}
    (args.out / f"manifest-{args.name}.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=1), encoding="utf-8")
    print(args.name, "clips", len(jobs), "eval clips", len(eval_set), "frames", sum(counts))


if __name__ == "__main__":
    main()
