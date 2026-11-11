"""Auto-label real Japanese plates with Lipla-jp (MIT) as a teacher model.

Writes one JSON line per image with plate corners and per-field readings and
scores. Photos are EXIF-rotated (HEIC supported) and saved as normalized JPEG
copies so coordinates refer to an image on disk. For dash-camera frames,
--crops adds a centered 4:3 pass because Lipla prefers plates >= 1/10 of the
image width.

Run with O:/NP1_plate_data/venv-lipla (lipla-jp 0.4.1, onnxruntime-gpu):

    python tools/plate/train/lipla_label.py --images "O:/NP1_plate_data/ナンバー写真" \
        --out O:/NP1_plate_data/labels/photos.jsonl --norm-dir O:/NP1_plate_data/photos-norm
"""
from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

import cv2
import numpy as np
from PIL import Image, ImageOps

EXTS = (".jpg", ".jpeg", ".png", ".heic", ".heif")


def load(path: Path, max_side: int) -> np.ndarray:
    img = ImageOps.exif_transpose(Image.open(path)).convert("RGB")
    if max(img.size) > max_side:
        img.thumbnail((max_side, max_side), Image.LANCZOS)
    return cv2.cvtColor(np.asarray(img), cv2.COLOR_RGB2BGR)


def to_record(r, dx=0.0, dy=0.0, scale=1.0) -> dict:
    quad = (np.asarray(r.vertices, np.float32).reshape(4, 2) * scale + [dx, dy]).round(1).tolist()
    return {"quad": quad, "score": float(r.score), "type": r.class_name, "area": r.area, "areaScore": float(r.area_score),
            "classNumber": str(r.class_number), "classScore": float(r.class_number_score), "kana": r.kana,
            "kanaScore": float(r.kana_score), "number": str(int(r.number)) if str(r.number).lstrip("-").isdigit() else str(r.number),
            "numberScore": float(r.number_score)}


def tiles(w: int, h: int, small_only: bool = False):
    """4:3 tiles at 90% and 50% of the image height. Lipla resizes its input
    to 640 px, so a 50% tile shows each plate about twice as large.
    small_only (dash-camera frames) keeps just the bottom row of 50% tiles:
    the corner detector is the GPU bottleneck."""
    out = []
    for frac, cols, rows in ((0.9, 3, 1), (0.5, 4, 2 if h > w * 0.6 else 1)):
        if small_only and frac > 0.5:
            continue
        th = int(h * frac)
        tw = min(w, int(th * 4 / 3))
        xs = np.linspace(0, w - tw, cols).astype(int)
        ys = [h - th] if rows == 1 else [int(h * 0.25), h - th]
        out += [(int(x), int(y), tw, th) for y in ys for x in xs]
    return out


def bbox(q):
    q = np.asarray(q)
    return q[:, 0].min(), q[:, 1].min(), q[:, 0].max(), q[:, 1].max()


def iou(a, b):
    ax0, ay0, ax1, ay1 = bbox(a)
    bx0, by0, bx1, by1 = bbox(b)
    iw, ih = max(0, min(ax1, bx1) - max(ax0, bx0)), max(0, min(ay1, by1) - max(ay0, by0))
    inter = iw * ih
    return inter / ((ax1 - ax0) * (ay1 - ay0) + (bx1 - bx0) * (by1 - by0) - inter + 1e-6)


def merge(plates: list[dict]) -> list[dict]:
    plates = sorted(plates, key=lambda p: -p["score"])
    kept = []
    for p in plates:
        if all(iou(p["quad"], k["quad"]) < 0.4 for k in kept):
            kept.append(p)
    return kept


def recognize(rec, img: np.ndarray, extra_tiles, det_thresh: float, ocr_min: float = 0.6) -> list[dict]:
    """Run Lipla's corner detector on the image and tiles, merge duplicate
    quads, then read each plate once (OCR dominates Lipla's run time)."""
    h, w = img.shape[:2]
    quads = []
    for x0, y0, cw, ch in [(0, 0, w, h), *extra_tiles]:
        pose = rec.pose_model(np.ascontiguousarray(img[y0:y0 + ch, x0:x0 + cw]))
        for kpts, score, name in zip(pose.kpts, pose.scores, pose.class_names):
            if score >= det_thresh:
                q = np.asarray(kpts, np.float32).reshape(-1, 2)[:4] + [x0, y0]
                quads.append({"quad": q.tolist(), "score": float(score), "type": name})
    out = []
    for d in merge(quads):
        if d["score"] < ocr_min:
            # Not read (OCR dominates run time); kept as an uncertain box that
            # detector training ignores rather than treats as background.
            out.append({**d, "unread": True})
            continue
        r = rec._recognize_detection(img, np.asarray(d["quad"], np.float32).reshape(-1).tolist(), d["score"], d["type"])
        if r is not None:
            out.append(to_record(r))
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--images", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--norm-dir", type=Path, help="save EXIF-normalized JPEG copies here (photos)")
    ap.add_argument("--max-side", type=int, default=3000)
    ap.add_argument("--crops", action="store_true", help="add 4:3 tile passes so small plates are found")
    ap.add_argument("--small-tiles", action="store_true", help="only the bottom row of half-height tiles")
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--every", type=int, default=1, help="use every Nth file (frames)")
    ap.add_argument("--shard", default="0/1", help="i/n: process the i-th of n interleaved shards")
    ap.add_argument("--det-thresh", type=float, default=0.25)
    ap.add_argument("--ocr-min", type=float, default=0.6, help="read only detections at or above this score")
    args = ap.parse_args()
    try:
        import pillow_heif
        pillow_heif.register_heif_opener()
    except ImportError:
        pass
    import onnxruntime
    import lipla
    onnxruntime.preload_dlls()
    # HEURISTIC avoids an exhaustive cuDNN search for every new input width.
    rec = lipla.Recognizer(providers=[("CUDAExecutionProvider", {"cudnn_conv_algo_search": "HEURISTIC"}), "CPUExecutionProvider"])
    args.out.parent.mkdir(parents=True, exist_ok=True)
    done = set()
    if args.out.exists():
        done = {json.loads(line)["file"] for line in args.out.read_text(encoding="utf-8").splitlines() if line.strip()}
    files = sorted(p for p in args.images.rglob("*") if p.suffix.lower() in EXTS)
    files = files[:: args.every]
    shard, shards = (int(v) for v in args.shard.split("/"))
    files = files[shard::shards]
    if args.limit:
        files = files[: args.limit]
    t0, n, plates_total = time.time(), 0, 0
    with args.out.open("a", encoding="utf-8") as out:
        for path in files:
            rel = path.relative_to(args.images).as_posix()
            if rel in done:
                continue
            try:
                img = load(path, args.max_side)
            except Exception as e:  # unreadable file: record and continue
                out.write(json.dumps({"file": rel, "error": type(e).__name__}, ensure_ascii=False) + "\n")
                continue
            h, w = img.shape[:2]
            image_path = rel
            if args.norm_dir:
                target = args.norm_dir / (Path(rel).with_suffix(".jpg"))
                target.parent.mkdir(parents=True, exist_ok=True)
                if not target.exists():
                    cv2.imwrite(str(target), img, [cv2.IMWRITE_JPEG_QUALITY, 93])
                image_path = target.relative_to(args.norm_dir).as_posix()
            found = recognize(rec, img, tiles(w, h, args.small_tiles) if args.crops else [], args.det_thresh, args.ocr_min)
            plates_total += len(found)
            out.write(json.dumps({"file": rel, "image": image_path, "w": w, "h": h, "plates": found}, ensure_ascii=False) + "\n")
            out.flush()
            n += 1
            if n % 100 == 0:
                print(f"{n} images, {plates_total} plates, {(time.time() - t0) / n:.2f}s/img", flush=True)
    print(f"done: {n} images, {plates_total} plates", flush=True)


if __name__ == "__main__":
    sys.exit(main())
