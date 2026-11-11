"""Train the plate reader on synthetic plates and export it to ONNX.

    python tools/plate/train/train_reader.py --out tmp/plate-train/reader --steps 20000

Outputs (in --out): reader.pt, reader.onnx, heads.json, calibration.npy
(representative inputs for int8 quantization) and metrics.json.
"""
from __future__ import annotations

import argparse
import json
import math
import random
import sys
import time
from pathlib import Path

import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F
from torch.utils.data import DataLoader, IterableDataset

sys.path.insert(0, str(Path(__file__).resolve().parent))
import plates  # noqa: E402

IN_H, IN_W = 96, 192


class SyntheticPlates(IterableDataset):
    def __init__(self, seed: int):
        self.seed = seed

    def __iter__(self):
        info = torch.utils.data.get_worker_info()
        rng = random.Random(self.seed * 1000 + (info.id if info else 0))
        while True:
            img, target, _ = plates.sample(rng, IN_W, IN_H)
            yield torch.from_numpy(img).unsqueeze(0), torch.tensor(target, dtype=torch.long)


def fixed_set(seed: int, n: int):
    rng = random.Random(seed)
    xs, ys, widths = [], [], []
    for _ in range(n):
        text = plates.random_text(rng)
        px = int(math.exp(rng.uniform(math.log(26), math.log(150))))
        img, _ = plates.render(text, rng, scale=min(2.0, max(0.7, px * 3 / 330)))
        crop = plates.place_with_context(img, rng)
        xs.append(plates.degrade(crop, rng, IN_W, IN_H, px))
        ys.append(text.targets())
        widths.append(px)
    return torch.from_numpy(np.stack(xs)).unsqueeze(1), torch.tensor(ys), np.array(widths)


def cbr(i, o, k=3, s=1, g=1):
    return nn.Sequential(nn.Conv2d(i, o, k, s, k // 2, groups=g, bias=False), nn.BatchNorm2d(o), nn.ReLU(inplace=True))


class Block(nn.Module):
    def __init__(self, i, o, s=1):
        super().__init__()
        self.dw = cbr(i, i, 3, s, g=i)
        self.pw = cbr(i, o, 1)
        self.residual = s == 1 and i == o

    def forward(self, x):
        y = self.pw(self.dw(x))
        return x + y if self.residual else y


class Reader(nn.Module):
    def __init__(self, head_sizes):
        super().__init__()
        self.features = nn.Sequential(
            cbr(1, 48, 3, 2),
            Block(48, 96, 2), Block(96, 96),
            Block(96, 160, 2), Block(160, 160), Block(160, 160),
            Block(160, 256, 2), Block(256, 256), Block(256, 256),
            cbr(256, 96, 1),
        )
        self.fc = nn.Sequential(nn.Flatten(), nn.Linear(96 * (IN_H // 16) * (IN_W // 16), 512), nn.ReLU(inplace=True), nn.Dropout(0.2))
        self.out = nn.Linear(512, sum(head_sizes))
        self.head_sizes = head_sizes

    def forward(self, x):  # x: N,1,H,W in [0,1]
        return self.out(self.fc(self.features(x)))


def split_loss(logits, target, sizes, smoothing=0.05):
    loss, offset = 0.0, 0
    for i, n in enumerate(sizes):
        loss = loss + F.cross_entropy(logits[:, offset:offset + n], target[:, i], label_smoothing=smoothing)
        offset += n
    return loss


@torch.no_grad()
def evaluate(model, xs, ys, widths, sizes, device):
    model.eval()
    preds = []
    for i in range(0, len(xs), 512):
        logits = model(xs[i:i + 512].to(device).float() / 255)
        offset, cols = 0, []
        for n in sizes:
            cols.append(logits[:, offset:offset + n].argmax(1))
            offset += n
        preds.append(torch.stack(cols, 1).cpu())
    pred = torch.cat(preds)
    correct = pred == ys
    names = [f for f, _ in plates.HEADS]
    number = correct[:, [i for i, f in enumerate(names) if f == "number"]].all(1).numpy()
    full = correct.all(1).numpy()
    by_width = {}
    for lo, hi in [(26, 40), (40, 60), (60, 90), (90, 150)]:
        m = (widths >= lo) & (widths < hi)
        by_width[f"{lo}-{hi}px"] = {"number": float(number[m].mean()), "all": float(full[m].mean()), "n": int(m.sum())}
    per_field = {f: float(correct[:, [i for i, g in enumerate(names) if g == f]].all(1).float().mean()) for f in dict.fromkeys(names)}
    model.train()
    return {"number": float(number.mean()), "all": float(full.mean()), "fields": per_field, "byPlateWidth": by_width}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--steps", type=int, default=20000)
    ap.add_argument("--batch", type=int, default=256)
    ap.add_argument("--lr", type=float, default=2e-3)
    ap.add_argument("--workers", type=int, default=12)
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--resume", type=Path)
    ap.add_argument("--export-only", type=Path, help="export an existing reader.pt to --out without training")
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    torch.manual_seed(args.seed)
    device = "cuda" if torch.cuda.is_available() else "cpu"
    sizes = plates.HEAD_SIZES
    model = Reader(sizes).to(device)
    if args.resume:
        model.load_state_dict(torch.load(args.resume, map_location=device))
    opt = torch.optim.AdamW(model.parameters(), lr=args.lr, weight_decay=1e-4)
    sched = torch.optim.lr_scheduler.OneCycleLR(opt, max_lr=args.lr, total_steps=args.steps, pct_start=0.05)
    if args.export_only:
        model.load_state_dict(torch.load(args.export_only, map_location=device))
        vx, vy, vw = fixed_set(12345, 3000)
        final = evaluate(model, vx, vy, vw, sizes, device)
        export(model, args.out, vx, final, [final])
        return
    loader = DataLoader(SyntheticPlates(args.seed), batch_size=args.batch, num_workers=args.workers, persistent_workers=True, prefetch_factor=4)
    print("building validation set...", flush=True)
    vx, vy, vw = fixed_set(12345, 3000)
    t0 = time.time()
    history = []
    for step, (x, y) in enumerate(loader, 1):
        x, y = x.to(device, non_blocking=True).float() / 255, y.to(device, non_blocking=True)
        loss = split_loss(model(x), y, sizes)
        opt.zero_grad(set_to_none=True)
        loss.backward()
        opt.step()
        sched.step()
        if step % 500 == 0 or step == args.steps:
            m = evaluate(model, vx, vy, vw, sizes, device)
            m.update(step=step, loss=float(loss), minutes=round((time.time() - t0) / 60, 1))
            history.append(m)
            print(json.dumps({k: m[k] for k in ("step", "loss", "number", "all", "minutes")}), flush=True)
            torch.save(model.state_dict(), args.out / "reader.pt")
        if step >= args.steps:
            break
    export(model, args.out, vx, history[-1], history)


def export(model, out: Path, vx, final, history):
    (out / "metrics.json").write_text(json.dumps({"final": final, "history": history}, ensure_ascii=False, indent=2), encoding="utf-8")
    heads = [{"field": f, "labels": labels} for f, labels in plates.HEADS]
    (out / "heads.json").write_text(json.dumps(heads, ensure_ascii=False), encoding="utf-8")
    model.eval().cpu()
    torch.onnx.export(model, torch.zeros(1, 1, IN_H, IN_W), out / "reader.onnx", input_names=["image"], output_names=["logits"], opset_version=13, dynamo=False)
    calib = (vx[:400].float() / 255).permute(0, 2, 3, 1).numpy().astype(np.float32)  # NHWC
    np.save(out / "calibration.npy", calib)
    print(json.dumps(final, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
