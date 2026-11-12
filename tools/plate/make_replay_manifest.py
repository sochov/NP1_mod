"""Build a plate-replay manifest from dash-camera clips (PC verification only).

Start times come from the clip names (YYYYMMDD_HHMMSS, JST). When the clips
carry LIGOGPSINFO tracks, GPS samples are decoded with ExifTool's LigoGPS
module (tools/plate/train/ligogps_extract.pl) and stored as offsets from the
clip start (one sample per second).

    python tools/plate/make_replay_manifest.py --exiftool-lib O:/NP1_plate_data/tools/Image-ExifTool-13.63/lib \
        --out O:/NP1_plate_data/demo/replay.json --fps 4 CLIP [CLIP ...]
"""
from __future__ import annotations

import argparse
import csv
import io
import json
import re
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
JST = timezone(timedelta(hours=9))


def gps_track(exiftool_lib: str, clip: Path) -> list[list[float]]:
    result = subprocess.run(["perl", str(ROOT / "tools/plate/train/ligogps_extract.pl"), exiftool_lib, str(clip)],
                            capture_output=True, text=True, encoding="utf-8", errors="replace")
    rows = list(csv.DictReader(io.StringIO(result.stdout)))
    track = []
    for r in rows:
        lat, lon = float(r["lat"]), float(r["lon"])
        if 20 < lat < 50 and 120 < lon < 155:  # sanity: Japan
            track.append([float(r["sample"]) - 1, lat, lon])
    return track


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("clips", nargs="+", type=Path)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--fps", type=float, default=4)
    ap.add_argument("--exiftool-lib")
    args = ap.parse_args()
    clips = []
    for clip in sorted(args.clips):
        m = re.match(r"(\d{8})_(\d{6})", clip.name)
        if not m:
            continue
        start = datetime.strptime(m.group(1) + m.group(2), "%Y%m%d%H%M%S").replace(tzinfo=JST)
        entry = {"path": str(clip), "start": start.isoformat()}
        if args.exiftool_lib:
            track = gps_track(args.exiftool_lib, clip)
            if track:
                entry["gps"] = track
        clips.append(entry)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps({"fps": args.fps, "clips": clips}, ensure_ascii=False), encoding="utf-8")
    print(f"{len(clips)} clips, {sum(1 for c in clips if 'gps' in c)} with GPS -> {args.out}")


if __name__ == "__main__":
    main()
