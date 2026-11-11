"""Real-plate training data built from Lipla-jp teacher labels.

build_cache() crops every confidently labeled plate (with context) from the
source images into one compressed .npz so training does not decode large
photos repeatedly. RealPlates then turns each cached crop into a reader input
exactly like the engine does: detector-box jitter, margin, distance
simulation (plate 26..150 px wide, never upscaled beyond the source), sensor
degradation, gray 96x192.

Heads whose teacher confidence is low get the ignore index (-100), so a doubtful
teacher reading never becomes a training target.
"""
from __future__ import annotations

import hashlib
import json
import math
import random
from pathlib import Path

import cv2
import numpy as np

import plates

IGNORE = -100
MIN_PLATE_SCORE = 0.75
MIN_NUMBER_SCORE = 0.9
MIN_FIELD_SCORE = 0.85
MAX_CACHED_PLATE_W = 360


def targets_from_label(p: dict) -> list[int] | None:
    number = p.get("number", "")
    if p.get("unread") or p["score"] < MIN_PLATE_SCORE or p["numberScore"] < MIN_NUMBER_SCORE:
        return None
    if not (number.isdigit() and 1 <= len(number) <= 4 and number[0] != "0"):
        return None
    region = plates.HEADS[0][1]
    t = [region.index(p["area"]) if p["areaScore"] >= MIN_FIELD_SCORE and p["area"] in region else IGNORE]
    cls = p.get("classNumber", "")
    if p["classScore"] >= MIN_FIELD_SCORE and 1 <= len(cls) <= 3 and all(c in plates.CLASS_CHARS for c in cls):
        t += [plates.CLASS_CHARS.index(c) for c in list(cls) + [""] * (3 - len(cls))]
    else:
        t += [IGNORE] * 3
    kana = plates.HEADS[4][1]
    t += [kana.index(p["kana"]) if p["kanaScore"] >= MIN_FIELD_SCORE and p["kana"] in kana else IGNORE]
    digits = [""] * (4 - len(number)) + list(number)
    t += [plates.NUMBER_CHARS.index(d) for d in digits]
    return t


def split_of(key: str, val_fraction: float) -> str:
    h = int(hashlib.sha1(key.encode()).hexdigest()[:8], 16) / 0xFFFFFFFF
    return "val" if h < val_fraction else "train"


def main():
    import argparse
    ap = argparse.ArgumentParser(description="Build the real plate cache from Lipla labels")
    ap.add_argument("--data", type=Path, default=Path("O:/NP1_plate_data"))
    ap.add_argument("--out", type=Path, required=True)
    args = ap.parse_args()
    d = args.data
    sources = [(d / "labels/photos.jsonl", d / "photos-norm", "photo", 0.15)]
    sources += [(f, d / "dashcam/train", "dashcam", 0.1) for f in sorted((d / "labels").glob("dashcam-train-*.jsonl"))]
    print(json.dumps(build_cache(sources, args.out)))


def build_cache(sources: list[tuple[Path, Path, str, float]], out: Path) -> dict:
    """sources: (labels.jsonl, image root, domain name, val fraction)."""
    crops, quads, targets, splits, domains, widths = [], [], [], [], [], []
    for labels, root, domain, val_fraction in sources:
        for line in labels.read_text(encoding="utf-8").splitlines():
            try:
                item = json.loads(line)
            except json.JSONDecodeError:  # a line still being written by lipla_label.py
                continue
            good = [(p, targets_from_label(p)) for p in item.get("plates", [])]
            good = [(p, t) for p, t in good if t is not None]
            if not good:
                continue
            path = root / item.get("image", item["file"])
            img = cv2.imdecode(np.fromfile(str(path), np.uint8), cv2.IMREAD_COLOR) if path.exists() else None  # non-ASCII safe
            if img is None:
                continue
            gray_ok = img.shape[:2] == (item["h"], item["w"])
            if not gray_ok:
                continue
            split = split_of(item["file"], val_fraction)
            for p, t in good:
                q = np.asarray(p["quad"], np.float32)
                x0, y0 = q.min(0)
                x1, y1 = q.max(0)
                bw, bh = x1 - x0, y1 - y0
                if bw < 12 or bh < 6:
                    continue
                cx0, cy0 = int(max(0, x0 - bw * 0.6)), int(max(0, y0 - bh * 0.8))
                cx1, cy1 = int(min(img.shape[1], x1 + bw * 0.6)), int(min(img.shape[0], y1 + bh * 0.8))
                crop = img[cy0:cy1, cx0:cx1]
                scale = min(1.0, MAX_CACHED_PLATE_W / bw)
                if scale < 1:
                    crop = cv2.resize(crop, (max(1, int(crop.shape[1] * scale)), max(1, int(crop.shape[0] * scale))), interpolation=cv2.INTER_AREA)
                ok, buf = cv2.imencode(".jpg", crop, [cv2.IMWRITE_JPEG_QUALITY, 95])
                crops.append(np.frombuffer(buf.tobytes(), np.uint8))
                quads.append(((q - [cx0, cy0]) * scale).astype(np.float32))
                targets.append(t)
                splits.append(split)
                domains.append(domain)
                widths.append(float(bw))
    out.parent.mkdir(parents=True, exist_ok=True)
    np.savez(out, crops=np.array(crops, dtype=object), quads=np.stack(quads), targets=np.array(targets, np.int64),
             splits=np.array(splits), domains=np.array(domains), widths=np.array(widths, np.float32))
    summary = {"plates": len(crops), "train": int(sum(s == "train" for s in splits)), "val": int(sum(s == "val" for s in splits))}
    for d in sorted(set(domains)):
        summary[d] = int(sum(x == d for x in domains))
    return summary


class RealPlates:
    def __init__(self, cache: Path, split: str):
        z = np.load(cache, allow_pickle=True)  # our own cache file, written by build_cache
        keep = z["splits"] == split
        self.crops = z["crops"][keep]
        self.quads = z["quads"][keep]
        self.targets = z["targets"][keep]
        self.widths = z["widths"][keep]
        self.domains = z["domains"][keep]

    def __len__(self):
        return len(self.crops)

    def render(self, i: int, rng: random.Random, out_w: int = 192, out_h: int = 96, plate_px: int | None = None, augment: bool = True):
        img = cv2.imdecode(self.crops[i], cv2.IMREAD_COLOR)
        img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
        q = self.quads[i]
        x0, y0 = q.min(0)
        x1, y1 = q.max(0)
        bw, bh = x1 - x0, y1 - y0
        native = float(self.widths[i])  # plate width in the source image
        if plate_px is None:
            plate_px = int(math.exp(rng.uniform(math.log(26), math.log(150))))
        plate_px = min(plate_px, int(native))  # never invent detail the source lacks
        if augment:
            jx, jy = rng.gauss(0, 0.04) * bw, rng.gauss(0, 0.06) * bh
            sw, sh = rng.uniform(0.9, 1.12), rng.uniform(0.9, 1.12)
        else:
            jx = jy = 0.0
            sw = sh = 1.0
        bw2, bh2 = bw * sw * 1.12, bh * sh * 1.12  # + engine margin (0.06 per side)
        cx, cy = (x0 + x1) / 2 + jx, (y0 + y1) / 2 + jy
        a0, b0 = int(max(0, cx - bw2 / 2)), int(max(0, cy - bh2 / 2))
        a1, b1 = int(min(img.shape[1], cx + bw2 / 2)), int(min(img.shape[0], cy + bh2 / 2))
        crop = img[b0:b1, a0:a1]
        if augment:
            return plates.degrade(crop, rng, out_w, out_h, max(16, int(plate_px * crop.shape[1] / max(1, bw)))), self.targets[i]
        small_w = max(16, int(plate_px * crop.shape[1] / max(1, bw)))
        small = cv2.resize(crop, (small_w, max(8, int(crop.shape[0] * small_w / crop.shape[1]))), interpolation=cv2.INTER_AREA)
        gray = cv2.cvtColor(small, cv2.COLOR_RGB2GRAY)
        return cv2.resize(gray, (out_w, out_h), interpolation=cv2.INTER_LINEAR), self.targets[i]


if __name__ == "__main__":
    main()
