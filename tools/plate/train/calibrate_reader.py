"""Calibrate the reader's confidences (temperature scaling) and re-export it.

The engine turns each head's logits into probabilities and uses the lowest
serial-digit probability as the record confidence, so thresholds such as
minConfidence and instantConfidence only mean "this likely" when the
probabilities are calibrated on footage like the device's. One temperature per
field (region, class, kana, number) is fitted on held-out real plates by
minimizing the negative log-likelihood, then folded into the last layer, so the
exported graph and the engine stay unchanged.

    python tools/plate/train/calibrate_reader.py --reader DATA/reader-v6 \
        --cache DATA/real-cache-v2.npz --out DATA/reader-v6c
"""
from __future__ import annotations

import argparse
import json
import random
import shutil
import sys
from pathlib import Path

import numpy as np
import torch
import torch.nn.functional as F

sys.path.insert(0, str(Path(__file__).resolve().parent))
import plates  # noqa: E402
import real_data  # noqa: E402
from train_reader import IN_H, IN_W, Reader, export, fixed_set, evaluate  # noqa: E402


def val_batch(cache: Path, domains: list[str], widths=(36, 50, 70, 100, 10000), seed=0):
    real = real_data.RealPlates(cache, "val")
    keep = [i for i in range(len(real)) if not domains or real.domains[i] in domains]
    xs, ys = [], []
    for px in widths:
        rng = random.Random(seed + px)
        for i in keep:
            img, t = real.render(i, rng, IN_W, IN_H, plate_px=px, augment=False)
            xs.append(img)
            ys.append(t)
    return torch.from_numpy(np.stack(xs)).unsqueeze(1).float() / 255, torch.tensor(np.stack(ys)), len(keep)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--reader", type=Path, required=True, help="directory with reader.pt")
    ap.add_argument("--cache", type=Path, required=True)
    ap.add_argument("--domains", nargs="*", default=["dashcam"], help="validation domains used for fitting")
    ap.add_argument("--out", type=Path, required=True)
    a = ap.parse_args()
    sizes = plates.HEAD_SIZES
    names = [f for f, _ in plates.HEADS]
    model = Reader(sizes)
    model.load_state_dict(torch.load(a.reader / "reader.pt", map_location="cpu"))
    model.eval()
    x, y, n = val_batch(a.cache, a.domains)
    with torch.no_grad():
        logits = torch.cat([model(x[i:i + 512]) for i in range(0, len(x), 512)])
    temps, report = {}, {"plates": n, "samples": len(x)}
    for field in dict.fromkeys(names):
        heads = [i for i, f in enumerate(names) if f == field]
        best = (float("inf"), 1.0)
        for T in np.exp(np.linspace(np.log(0.25), np.log(4.0), 81)):
            nll, cnt = 0.0, 0
            for h in heads:
                off = sum(sizes[:h])
                t = y[:, h]
                m = t != real_data.IGNORE
                if m.any():
                    nll += float(F.cross_entropy(logits[m, off:off + sizes[h]] / T, t[m], reduction="sum"))
                    cnt += int(m.sum())
            if cnt and nll / cnt < best[0]:
                best = (nll / cnt, float(T))
        temps[field] = best[1]
        report[field] = {"temperature": round(best[1], 3), "nll": round(best[0], 4)}
    # Fold 1/T into the rows of the last layer that produce each field.
    with torch.no_grad():
        for h, field in enumerate(names):
            off = sum(sizes[:h])
            model.out.weight[off:off + sizes[h]] /= temps[field]
            model.out.bias[off:off + sizes[h]] /= temps[field]
    a.out.mkdir(parents=True, exist_ok=True)
    torch.save(model.state_dict(), a.out / "reader.pt")
    # How the calibrated serial confidence relates to accuracy on the same set.
    with torch.no_grad():
        logits = torch.cat([model(x[i:i + 512]) for i in range(0, len(x), 512)])
    num = [i for i, f in enumerate(names) if f == "number"]
    conf, ok = [], []
    for h in num:
        off = sum(sizes[:h])
        p = logits[:, off:off + sizes[h]].softmax(1)
        conf.append(p.max(1).values)
        ok.append(p.argmax(1) == y[:, h])
    conf = torch.stack(conf, 1).min(1).values.numpy()
    ok = torch.stack(ok, 1).all(1).numpy()
    bins = [0, 0.6, 0.8, 0.9, 0.95, 0.98, 1.01]
    report["serialByConfidence"] = {f"{bins[i]}-{bins[i + 1]}": {"n": int(((conf >= bins[i]) & (conf < bins[i + 1])).sum()),
                                     "acc": round(float(ok[(conf >= bins[i]) & (conf < bins[i + 1])].mean()), 3) if ((conf >= bins[i]) & (conf < bins[i + 1])).any() else None}
                                    for i in range(len(bins) - 1)}
    vx, vy, vw = fixed_set(12345, 3000)
    final = evaluate(model, vx, vy, vw, sizes, "cpu")
    final["calibration"] = report
    export(model, a.out, vx, final, [final])
    for f in ("calibration.npy",):
        if not (a.out / f).exists() and (a.reader / f).exists():
            shutil.copy(a.reader / f, a.out / f)
    print(json.dumps(report, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
