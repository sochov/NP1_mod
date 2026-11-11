"""Fetch the Open Images V7 subset used to train the plate detector.

Streams the official box annotation CSVs, keeps images that contain a
"Vehicle registration plate" box (plus their "Car" boxes), and downloads
those images resized to a long side of --max-side. Images are CC BY 2.0
(see Open Images); they stay in the ignored tmp/ tree and are not shipped.

    python tools/plate/train/fetch_openimages.py --out tmp/plate-train/openimages --splits validation test train --limit 9000
"""
from __future__ import annotations

import argparse
import csv
import io
import json
import shutil
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from PIL import Image

PLATE = "/m/01jfm_"
CAR = "/m/0k4j"
CSV = {
    "validation": "https://storage.googleapis.com/openimages/v5/validation-annotations-bbox.csv",
    "test": "https://storage.googleapis.com/openimages/v5/test-annotations-bbox.csv",
    "train": "https://storage.googleapis.com/openimages/v6/oidv6-train-annotations-bbox.csv",
}
IMAGE_URL = "https://open-images-dataset.s3.amazonaws.com/{split}/{id}.jpg"
UA = {"User-Agent": "np1-plate-dataset-fetch/1.0"}


def stream_boxes(split: str):
    """Yield (image_id, label, xmin, xmax, ymin, ymax) for plate/car rows."""
    req = urllib.request.Request(CSV[split], headers=UA)
    with urllib.request.urlopen(req, timeout=120) as resp:
        reader = csv.reader(io.TextIOWrapper(resp, encoding="utf-8", newline=""))
        header = next(reader)
        col = {name: i for i, name in enumerate(header)}
        for row in reader:
            label = row[col["LabelName"]]
            if label in (PLATE, CAR):
                yield row[col["ImageID"]], label, *(float(row[col[k]]) for k in ("XMin", "XMax", "YMin", "YMax"))


def download(item, out: Path, max_side: int) -> bool:
    image_id, split = item
    target = out / "images" / f"{image_id}.jpg"
    if target.exists():
        return True
    try:
        req = urllib.request.Request(IMAGE_URL.format(split=split, id=image_id), headers=UA)
        with urllib.request.urlopen(req, timeout=60) as resp:
            data = resp.read()
        img = Image.open(io.BytesIO(data)).convert("RGB")
        img.thumbnail((max_side, max_side), Image.LANCZOS)
        tmp = target.with_suffix(".part")
        img.save(tmp, "JPEG", quality=90)
        tmp.replace(target)
        return True
    except Exception:
        return False


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--splits", nargs="+", default=["validation", "test"], choices=list(CSV))
    ap.add_argument("--limit", type=int, default=9000, help="maximum images with plates")
    ap.add_argument("--max-side", type=int, default=1280)
    ap.add_argument("--min-free-gib", type=float, default=10)
    args = ap.parse_args()
    (args.out / "images").mkdir(parents=True, exist_ok=True)
    index_path = args.out / "index.json"
    index = json.loads(index_path.read_text()) if index_path.exists() else {}
    for split in args.splits:
        if sum(1 for v in index.values() if v["plates"]) >= args.limit:
            break
        per_image: dict[str, dict] = {}
        for image_id, label, x0, x1, y0, y1 in stream_boxes(split):
            entry = per_image.setdefault(image_id, {"split": split, "plates": [], "cars": []})
            entry["plates" if label == PLATE else "cars"].append([x0, y0, x1, y1])
        for image_id, entry in per_image.items():
            if entry["plates"] and image_id not in index:
                index[image_id] = entry
        print(split, "images with plates so far:", sum(1 for v in index.values() if v["plates"]), flush=True)
    items = [(k, v["split"]) for k, v in index.items() if v["plates"]][: args.limit]
    index = {k: index[k] for k, _ in items}
    index_path.write_text(json.dumps(index))
    if shutil.disk_usage(args.out).free / 2**30 < args.min_free_gib + len(items) * 0.25 / 1024:
        raise SystemExit("not enough free disk space for the requested images")
    ok = 0
    with ThreadPoolExecutor(16) as pool:
        for i, success in enumerate(pool.map(lambda it: download(it, args.out, args.max_side), items), 1):
            ok += success
            if i % 500 == 0:
                print(f"downloaded {ok}/{i}", flush=True)
    print(f"done: {ok}/{len(items)} images", flush=True)


if __name__ == "__main__":
    main()
