"""Train the plate detector and export it to ONNX.

Data: Open Images plates (fetch_openimages.py). About half of the real plate
boxes are replaced by rendered Japanese plates matched to local lighting, and
images are rescaled so plates appear as small as on a dash camera.

Model: ImageNet MobileNetV2 (torchvision, BSD) + small FPN + stride-8 head.
The exported graph decodes boxes itself and outputs [1, 5, N] =
(cx, cy, w, h, score) normalized to the input, which the device engine reads.

    python tools/plate/train/train_detector.py --data O:/NP1_plate_data/openimages --out O:/NP1_plate_data/detector-v1
"""
from __future__ import annotations

import argparse
import json
import math
import random
import sys
import time
from pathlib import Path

import cv2
import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F
import torchvision
from torch.utils.data import DataLoader, Dataset

sys.path.insert(0, str(Path(__file__).resolve().parent))
import plates  # noqa: E402

IN_H, IN_W, STRIDE = 384, 640, 8
GH, GW = IN_H // STRIDE, IN_W // STRIDE
MAX_WH = 0.5  # largest box side, as a fraction of the input


def paste_japanese(img: np.ndarray, box, rng: random.Random) -> None:
    """Replace a real plate (box in pixels) with a rendered Japanese plate."""
    x0, y0, x1, y1 = [int(round(v)) for v in box]
    w, h = x1 - x0, y1 - y0
    if w < 16 or h < 8 or x0 < 0 or y0 < 0 or x1 > img.shape[1] or y1 > img.shape[0]:
        return
    text = plates.random_text(rng)
    plate, _ = plates.render(text, rng, scale=min(2.0, max(0.6, w * 2 / 330)))
    region = img[y0:y1, x0:x1]
    # Keep the plate aspect: fit 2:1 inside the box, centered.
    pw, ph = (w, int(w / 2)) if w / 2 <= h * 1.15 else (int(h * 2), h)
    pw, ph = max(8, min(pw, w)), max(4, min(ph, h))
    small = cv2.resize(plate, (pw, ph), interpolation=cv2.INTER_AREA).astype(np.float32)
    lum = region.astype(np.float32).mean() / 255
    small *= np.clip(0.45 + lum, 0.35, 1.25)
    ox, oy = x0 + (w - pw) // 2, y0 + (h - ph) // 2
    img[oy:oy + ph, ox:ox + pw] = np.clip(small, 0, 255).astype(np.uint8)
    box[:] = [ox, oy, ox + pw, oy + ph]


class PlateDetection(Dataset):
    def __init__(self, root: Path, ids: list[str], index: dict, train: bool, seed: int = 0):
        self.root, self.ids, self.index, self.train, self.seed = root, ids, index, train, seed

    def __len__(self):
        return len(self.ids)

    def __getitem__(self, i):
        rng = random.Random(hash((self.seed, i, time.time_ns())) if self.train else self.seed * 1_000_003 + i)
        image_id = self.ids[i]
        img = cv2.imread(str(self.root / "images" / f"{image_id}.jpg"))
        if img is None:
            img = np.zeros((IN_H, IN_W, 3), np.uint8)
            boxes = np.zeros((0, 4), np.float32)
        else:
            img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
            H, W = img.shape[:2]
            boxes = np.array([[b[0] * W, b[1] * H, b[2] * W, b[3] * H] for b in self.index[image_id]["plates"]], np.float32)
            p_jp = 0.55 if self.train else 0.5
            for b in boxes:
                if rng.random() < p_jp:
                    paste_japanese(img, b, rng)
        img, boxes = self.frame(img, boxes, rng)
        if self.train:
            img = self.photometric(img, rng)
        target = np.zeros((32, 4), np.float32)
        n = min(len(boxes), 32)
        target[:n] = boxes[:n]
        return torch.from_numpy(img).permute(2, 0, 1), torch.from_numpy(target), n

    def frame(self, img, boxes, rng):
        """Scale/crop to the 5:3 input so plates are dash-camera sized."""
        H, W = img.shape[:2]
        if self.train and len(boxes):
            # Choose a scale that makes a random plate 14..110 px wide in the input.
            ref = boxes[rng.randrange(len(boxes))]
            target_w = math.exp(rng.uniform(math.log(14), math.log(110)))
            s = target_w / max(1.0, ref[2] - ref[0])
            s = min(max(s, IN_W / W * 0.6), 3.0)
        else:
            s = min(IN_W / W, IN_H / H)
        img = cv2.resize(img, (max(1, int(W * s)), max(1, int(H * s))), interpolation=cv2.INTER_AREA if s < 1 else cv2.INTER_LINEAR)
        boxes = boxes * s
        H, W = img.shape[:2]
        canvas = np.full((IN_H, IN_W, 3), 114, np.uint8)
        if self.train:
            ox = rng.randint(min(0, IN_W - W), max(0, IN_W - W))
            oy = rng.randint(min(0, IN_H - H), max(0, IN_H - H))
            if len(boxes) and rng.random() < 0.8:  # keep a plate in view
                b = boxes[rng.randrange(len(boxes))]
                ox = int(np.clip(IN_W / 2 - (b[0] + b[2]) / 2 + rng.uniform(-IN_W * 0.35, IN_W * 0.35), min(0, IN_W - W), max(0, IN_W - W)))
                oy = int(np.clip(IN_H / 2 - (b[1] + b[3]) / 2 + rng.uniform(-IN_H * 0.35, IN_H * 0.35), min(0, IN_H - H), max(0, IN_H - H)))
        else:
            ox, oy = (IN_W - W) // 2, (IN_H - H) // 2
        sx0, sy0 = max(0, -ox), max(0, -oy)
        dx0, dy0 = max(0, ox), max(0, oy)
        cw, ch = min(W - sx0, IN_W - dx0), min(H - sy0, IN_H - dy0)
        canvas[dy0:dy0 + ch, dx0:dx0 + cw] = img[sy0:sy0 + ch, sx0:sx0 + cw]
        boxes = boxes + np.array([ox, oy, ox, oy], np.float32)
        if len(boxes):
            clipped = boxes.copy()
            clipped[:, [0, 2]] = clipped[:, [0, 2]].clip(0, IN_W)
            clipped[:, [1, 3]] = clipped[:, [1, 3]].clip(0, IN_H)
            area = (boxes[:, 2] - boxes[:, 0]) * (boxes[:, 3] - boxes[:, 1])
            keep = ((clipped[:, 2] - clipped[:, 0]) * (clipped[:, 3] - clipped[:, 1]) > 0.6 * area) & ((clipped[:, 2] - clipped[:, 0]) >= 6)
            boxes = clipped[keep]
        if self.train and rng.random() < 0.5:
            canvas = canvas[:, ::-1].copy()  # plates are symmetric enough for detection
            boxes = boxes.copy()
            boxes[:, [0, 2]] = IN_W - boxes[:, [2, 0]]
        return canvas, boxes

    def photometric(self, img, rng):
        a = img.astype(np.float32)
        gain = rng.uniform(0.5, 1.35) * (rng.uniform(0.25, 0.6) if rng.random() < 0.15 else 1)
        a = a * gain + rng.uniform(-25, 25)
        a = 255 * np.clip(a / 255, 0, 1) ** rng.uniform(0.75, 1.35)
        if rng.random() < 0.3:
            a = cv2.GaussianBlur(a, (0, 0), rng.uniform(0.4, 1.3))
        if rng.random() < 0.4:
            a = a + np.random.default_rng(rng.randrange(1 << 30)).normal(0, rng.uniform(2, 10), a.shape)
        return np.clip(a, 0, 255).astype(np.uint8)


class Detector(nn.Module):
    def __init__(self, pretrained=True):
        super().__init__()
        weights = torchvision.models.MobileNet_V2_Weights.IMAGENET1K_V1 if pretrained else None
        f = torchvision.models.mobilenet_v2(weights=weights).features
        self.c3, self.c4, self.c5 = f[:7], f[7:14], f[14:18]  # stride 8 / 16 / 32: 32, 96, 320 ch
        self.l5, self.l4, self.l3 = nn.Conv2d(320, 96, 1), nn.Conv2d(96, 96, 1), nn.Conv2d(32, 96, 1)
        def block(c):
            return nn.Sequential(nn.Conv2d(c, c, 3, 1, 1, groups=c, bias=False), nn.BatchNorm2d(c), nn.ReLU6(inplace=True),
                                 nn.Conv2d(c, c, 1, bias=False), nn.BatchNorm2d(c), nn.ReLU6(inplace=True))
        self.smooth4, self.smooth3 = block(96), block(96)
        self.head = nn.Sequential(block(96), block(96), nn.Conv2d(96, 5, 1))
        nn.init.constant_(self.head[-1].bias, 0)
        self.head[-1].bias.data[4] = -4.6  # prior p=0.01 for objectness
        gy, gx = torch.meshgrid(torch.arange(GH), torch.arange(GW), indexing="ij")
        self.register_buffer("gx", gx.float().view(1, 1, GH, GW), persistent=False)
        self.register_buffer("gy", gy.float().view(1, 1, GH, GW), persistent=False)
        mean = torch.tensor([0.485, 0.456, 0.406]).view(1, 3, 1, 1)
        std = torch.tensor([0.229, 0.224, 0.225]).view(1, 3, 1, 1)
        self.register_buffer("mean", mean, persistent=False)
        self.register_buffer("std", std, persistent=False)

    def raw(self, x):  # x: N,3,H,W in [0,1]
        x = (x - self.mean) / self.std
        c3 = self.c3(x)
        c4 = self.c4(c3)
        c5 = self.c5(c4)
        p5 = self.l5(c5)
        p4 = self.smooth4(self.l4(c4) + F.interpolate(p5, scale_factor=2.0, mode="nearest"))
        p3 = self.smooth3(self.l3(c3) + F.interpolate(p4, scale_factor=2.0, mode="nearest"))
        return self.head(p3)  # N,5,GH,GW: tx, ty, tw, th, obj

    @staticmethod
    def decode(raw, gx, gy):
        cx = (gx + torch.sigmoid(raw[:, 0:1]) * 2 - 0.5) / GW
        cy = (gy + torch.sigmoid(raw[:, 1:2]) * 2 - 0.5) / GH
        w = torch.sigmoid(raw[:, 2:3]) * MAX_WH
        h = torch.sigmoid(raw[:, 3:4]) * MAX_WH
        return cx, cy, w, h

    def forward(self, x):  # export path: [N, 5, GH*GW]
        r = self.raw(x)
        cx, cy, w, h = self.decode(r, self.gx, self.gy)
        out = torch.cat([cx, cy, w, h, torch.sigmoid(r[:, 4:5])], 1)
        return out.reshape(out.shape[0], 5, GH * GW)


def build_targets(boxes, counts):
    """YOLOv5-style assignment: the center cell plus the two nearest
    neighbours. Returns (batch, gy, gx, box_xyxy_normalized) tensors."""
    b_idx, ys, xs, tb = [], [], [], []
    for b in range(boxes.shape[0]):
        for k in range(int(counts[b])):
            x0, y0, x1, y1 = boxes[b, k].tolist()
            cx, cy = (x0 + x1) / 2 / STRIDE, (y0 + y1) / 2 / STRIDE
            gx, gy = int(cx), int(cy)
            cells = {(gx, gy), (gx + (1 if cx - gx > 0.5 else -1), gy), (gx, gy + (1 if cy - gy > 0.5 else -1))}
            for x, y in cells:
                if 0 <= x < GW and 0 <= y < GH:
                    b_idx.append(b); xs.append(x); ys.append(y)
                    tb.append([x0 / IN_W, y0 / IN_H, x1 / IN_W, y1 / IN_H])
    dev = boxes.device
    return (torch.tensor(b_idx, device=dev, dtype=torch.long), torch.tensor(ys, device=dev, dtype=torch.long),
            torch.tensor(xs, device=dev, dtype=torch.long), torch.tensor(tb, device=dev, dtype=torch.float32).view(-1, 4))


def giou(a, b):
    ix0, iy0 = torch.max(a[:, 0], b[:, 0]), torch.max(a[:, 1], b[:, 1])
    ix1, iy1 = torch.min(a[:, 2], b[:, 2]), torch.min(a[:, 3], b[:, 3])
    inter = (ix1 - ix0).clamp(0) * (iy1 - iy0).clamp(0)
    area = lambda t: (t[:, 2] - t[:, 0]).clamp(0) * (t[:, 3] - t[:, 1]).clamp(0)
    union = area(a) + area(b) - inter + 1e-9
    cx0, cy0 = torch.min(a[:, 0], b[:, 0]), torch.min(a[:, 1], b[:, 1])
    cx1, cy1 = torch.max(a[:, 2], b[:, 2]), torch.max(a[:, 3], b[:, 3])
    hull = (cx1 - cx0) * (cy1 - cy0) + 1e-9
    iou = inter / union
    return iou - (hull - union) / hull, iou


def loss_fn(model, raw, boxes, counts):
    b, ys, xs, tb = build_targets(boxes, counts)
    obj_logit = raw[:, 4]
    obj_target = torch.zeros_like(obj_logit)
    box_loss = raw.sum() * 0
    if len(b):
        cx, cy, w, h = Detector.decode(raw, model.gx, model.gy)
        pred = torch.stack([cx[b, 0, ys, xs] - w[b, 0, ys, xs] / 2, cy[b, 0, ys, xs] - h[b, 0, ys, xs] / 2,
                            cx[b, 0, ys, xs] + w[b, 0, ys, xs] / 2, cy[b, 0, ys, xs] + h[b, 0, ys, xs] / 2], 1)
        g, iou = giou(pred, tb)
        box_loss = (1 - g).mean()
        obj_target[b, ys, xs] = iou.detach().clamp(0.3, 1)  # IoU-aware objectness
    p = torch.sigmoid(obj_logit)
    bce = F.binary_cross_entropy_with_logits(obj_logit, obj_target, reduction="none")
    focal = bce * ((obj_target - p).abs() ** 2)  # quality-focal style
    obj_loss = focal.sum() / max(1, len(b))
    return obj_loss + 5 * box_loss, float(box_loss)


@torch.no_grad()
def evaluate(model, loader, device, thr=0.35):
    model.eval()
    tp = fp = fn = 0
    small_tp = small_n = 0
    for x, boxes, counts in loader:
        out = model(x.to(device).float() / 255).cpu()
        for i in range(out.shape[0]):
            o = out[i].T  # N,5
            keep = o[:, 4] > thr
            o = o[keep]
            pred = torch.stack([(o[:, 0] - o[:, 2] / 2) * IN_W, (o[:, 1] - o[:, 3] / 2) * IN_H, (o[:, 0] + o[:, 2] / 2) * IN_W, (o[:, 1] + o[:, 3] / 2) * IN_H], 1)
            order = o[:, 4].argsort(descending=True)
            pred = pred[order]
            kept = torchvision.ops.nms(pred, o[order, 4], 0.5) if len(pred) else torch.zeros(0, dtype=torch.long)
            pred = pred[kept]
            gt = boxes[i, : int(counts[i])]
            matched = set()
            if len(gt) and len(pred):
                ious = torchvision.ops.box_iou(pred, gt)
                for pi in range(len(pred)):
                    j = int(ious[pi].argmax())
                    if ious[pi, j] >= 0.5 and j not in matched:
                        matched.add(j); tp += 1
                    else:
                        fp += 1
            else:
                fp += len(pred)
            fn += len(gt) - len(matched)
            for j, g in enumerate(gt):
                if g[2] - g[0] < 40:
                    small_n += 1
                    small_tp += j in matched
    model.train()
    precision, recall = tp / max(1, tp + fp), tp / max(1, tp + fn)
    return {"precision": precision, "recall": recall, "f1": 2 * precision * recall / max(1e-9, precision + recall), "smallRecall": small_tp / max(1, small_n), "smallN": small_n}


def collate(batch):
    x = torch.stack([b[0] for b in batch])
    t = torch.stack([b[1] for b in batch])
    n = torch.tensor([b[2] for b in batch])
    return x, t, n


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--data", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--epochs", type=int, default=60)
    ap.add_argument("--batch", type=int, default=48)
    ap.add_argument("--lr", type=float, default=2e-3)
    ap.add_argument("--workers", type=int, default=16)
    ap.add_argument("--export-only", action="store_true", help="export --out/detector.pt without training")
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    index = json.loads((args.data / "index.json").read_text())
    ids = sorted(i for i in index if (args.data / "images" / f"{i}.jpg").exists())
    random.Random(7).shuffle(ids)
    n_val = max(200, len(ids) // 20)
    val_ids, train_ids = ids[:n_val], ids[n_val:]
    device = "cuda"
    model = Detector().to(device)
    train = DataLoader(PlateDetection(args.data, train_ids, index, True), batch_size=args.batch, shuffle=True, num_workers=args.workers, collate_fn=collate, persistent_workers=True, drop_last=True)
    val = DataLoader(PlateDetection(args.data, val_ids, index, False), batch_size=32, num_workers=8, collate_fn=collate)
    if args.export_only:
        export(model, args.out, val)
        return
    opt = torch.optim.AdamW(model.parameters(), lr=args.lr, weight_decay=5e-4)
    total = args.epochs * len(train)
    sched = torch.optim.lr_scheduler.OneCycleLR(opt, max_lr=args.lr, total_steps=total, pct_start=0.05)
    scaler = torch.amp.GradScaler()
    history, best, t0 = [], -1.0, time.time()
    print(f"train {len(train_ids)} / val {len(val_ids)} images", flush=True)
    for epoch in range(1, args.epochs + 1):
        for x, boxes, counts in train:
            x, boxes = x.to(device, non_blocking=True).float() / 255, boxes.to(device)
            with torch.autocast("cuda", dtype=torch.bfloat16):
                raw = model.raw(x)
            loss, box = loss_fn(model, raw.float(), boxes, counts)
            opt.zero_grad(set_to_none=True)
            scaler.scale(loss).backward()
            scaler.step(opt)
            scaler.update()
            sched.step()
        m = evaluate(model, val, device)
        m.update(epoch=epoch, loss=float(loss), box=box, minutes=round((time.time() - t0) / 60, 1))
        history.append(m)
        print(json.dumps(m), flush=True)
        if m["f1"] > best:
            best = m["f1"]
            torch.save(model.state_dict(), args.out / "detector.pt")
    (args.out / "metrics.json").write_text(json.dumps({"best_f1": best, "history": history}, indent=2))
    export(model, args.out, val)
    print("best f1", best)


def export(model, out: Path, val):
    model.load_state_dict(torch.load(out / "detector.pt"))
    model.eval().cpu()
    calib = []
    for x, _, _ in val:
        calib.append((x.float() / 255).permute(0, 2, 3, 1).numpy())
        if sum(len(c) for c in calib) >= 200:
            break
    np.save(out / "calibration.npy", np.concatenate(calib)[:200].astype(np.float32))
    torch.onnx.export(model, torch.zeros(1, 3, IN_H, IN_W), out / "detector.onnx", input_names=["image"], output_names=["boxes"], opset_version=13, dynamo=False)


if __name__ == "__main__":
    main()
