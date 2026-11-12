"""Per-field reader accuracy on held-out real plates, with the engine's
blanking rule (a field below fieldThreshold is left unread).

Reports correct / wrong / blank for the serial number and for region, class
and kana separately, per simulated plate width, plus a per-region breakdown
for the regions given with --focus.

    python tools/plate/train/eval_reader_fields.py --model DATA/reader-v4/reader.pt \
        --cache DATA/real-cache-b.npz --out reader-fields.json --focus 相模 横浜 湘南
"""
from __future__ import annotations

import argparse
import json
import random
import sys
from pathlib import Path

import numpy as np
import torch

sys.path.insert(0, str(Path(__file__).resolve().parent))
import plates  # noqa: E402
import real_data  # noqa: E402
import train_reader as tr  # noqa: E402

FIELDS = ("number", "region", "class", "kana")


def decode(logits: torch.Tensor):
    """Per head argmax and probability, grouped by field like the engine."""
    out = []
    offset = 0
    for (field, labels), n in zip(plates.HEADS, plates.HEAD_SIZES):
        p = torch.softmax(logits[:, offset:offset + n], 1)
        conf, idx = p.max(1)
        out.append((field, idx.cpu().numpy(), conf.cpu().numpy()))
        offset += n
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--model", type=Path, required=True)
    ap.add_argument("--cache", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--field-threshold", type=float, default=0.6)
    ap.add_argument("--focus", nargs="*", default=["相模", "横浜", "湘南"])
    args = ap.parse_args()
    device = "cuda" if torch.cuda.is_available() else "cpu"
    model = tr.Reader(plates.HEAD_SIZES).to(device)
    model.load_state_dict(torch.load(args.model, map_location=device))
    model.eval()
    real = real_data.RealPlates(args.cache, "val")
    names = [f for f, _ in plates.HEADS]
    regions = plates.HEADS[0][1]
    result = {"n": len(real), "fieldThreshold": args.field_threshold, "byWidth": {}, "byRegion": {}}
    for px, label in ((30, "30px"), (45, "45px"), (70, "70px"), (110, "110px"), (10000, "元の解像度")):
        rng = random.Random(0)
        xs, ys = [], []
        for i in range(len(real)):
            img, t = real.render(i, rng, tr.IN_W, tr.IN_H, plate_px=px, augment=False)
            xs.append(img)
            ys.append(t)
        x = torch.from_numpy(np.stack(xs)).unsqueeze(1).to(device).float() / 255
        y = np.stack(ys)
        with torch.no_grad():
            heads = decode(model(x))
        stats = {f: {"correct": 0, "wrong": 0, "blank": 0} for f in FIELDS}
        by_region = {}
        for i in range(len(real)):
            for field in FIELDS:
                idx = [k for k, f in enumerate(names) if f == field]
                if (y[i, idx] == real_data.IGNORE).any():
                    continue  # teacher not confident about this field
                conf = min(heads[k][2][i] for k in idx)
                ok = all(heads[k][1][i] == y[i, k] for k in idx)
                if field != "number" and conf < args.field_threshold:
                    outcome = "blank"
                else:
                    outcome = "correct" if ok else "wrong"
                stats[field][outcome] += 1
                if label == "元の解像度" or label == "45px":
                    region_idx = y[i, 0]
                    if region_idx != real_data.IGNORE:
                        name = regions[region_idx]
                        key = name if name in args.focus else "その他"
                        br = by_region.setdefault(label, {}).setdefault(key, {f: {"correct": 0, "wrong": 0, "blank": 0} for f in FIELDS})
                        br[field][outcome] += 1
        result["byWidth"][label] = stats
        result["byRegion"].update(by_region)
    args.out.write_text(json.dumps(result, ensure_ascii=False, indent=1), encoding="utf-8")
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    main()
