"""Train the plate detector and export it to ONNX.

Data: Open Images plates (fetch_openimages.py). About half of the real plate
boxes are replaced by rendered Japanese plates matched to local lighting, and
images are rescaled so plates appear as small as on a dash camera.

Model: ImageNet MobileNetV2 (torchvision, BSD) + small FPN + stride-8 head.
The exported graph decodes boxes itself and outputs [1, 5, N] =
(cx, cy, w, h, score) normalized to the input, which the device engine reads.

    python tools/plate/train/train_detector.py --data DATA/openimages --out DATA/detector-v1
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import random
import re
import sys
import time
from dataclasses import dataclass
from pathlib import Path

import cv2
import numpy as np

# Data-loader workers run in parallel; OpenCV must not spawn its own thread
# pool in each of them (it starved the other training job of CPU).
cv2.setNumThreads(1)
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


ROI = (320, 240, 1280, 768)  # engine default detector ROI in the 1920x1080 frame
MAX_BOXES = 48


@dataclass
class Sample:
    path: str
    boxes: np.ndarray   # (N,4) xyxy; normalized when `normalized`
    ignore: np.ndarray  # (M,4) xyxy uncertain plates: neither positive nor background
    domain: str         # openimages | photo | dashcam
    normalized: bool = False
    group: str = ""     # clip / file used for leakage-free splits


def openimages_samples(root: Path) -> list[Sample]:
    index = json.loads((root / "index.json").read_text())
    out = []
    for image_id, entry in index.items():
        path = root / "images" / f"{image_id}.jpg"
        if path.exists():
            out.append(Sample(str(path), np.array(entry["plates"], np.float32).reshape(-1, 4), np.zeros((0, 4), np.float32), "openimages", True, image_id))
    return out


def jsonl_samples(labels: Path, root: Path, domain: str) -> list[Sample]:
    """Lipla teacher labels: confident plates are positives, weak/unread ones are ignored."""
    out = []
    for line in labels.read_text(encoding="utf-8").splitlines():
        item = json.loads(line)
        if "plates" not in item:
            continue
        pos, ign = [], []
        for p in item["plates"]:
            q = np.asarray(p["quad"], np.float32)
            box = [*q.min(0), *q.max(0)]
            (ign if p.get("unread") or p["score"] < 0.5 else pos).append(box)
        rel = item.get("image", item["file"])
        group = rel.split("/")[0] + "/" + re.sub(r"_\d{4}\.jpg$", "", Path(rel).name)
        out.append(Sample(str(root / rel), np.array(pos, np.float32).reshape(-1, 4), np.array(ign, np.float32).reshape(-1, 4), domain, False, group))
    return out


def split_groups(samples: list[Sample], fraction: float) -> tuple[list[Sample], list[Sample]]:
    def is_val(s: Sample) -> bool:
        return int(hashlib.sha1(s.group.encode()).hexdigest()[:8], 16) / 0xFFFFFFFF < fraction
    return [s for s in samples if not is_val(s)], [s for s in samples if is_val(s)]


def clip_boxes(boxes: np.ndarray, min_keep: float = 0.6) -> np.ndarray:
    if not len(boxes):
        return boxes
    clipped = boxes.copy()
    clipped[:, [0, 2]] = clipped[:, [0, 2]].clip(0, IN_W)
    clipped[:, [1, 3]] = clipped[:, [1, 3]].clip(0, IN_H)
    area = (boxes[:, 2] - boxes[:, 0]) * (boxes[:, 3] - boxes[:, 1])
    keep = ((clipped[:, 2] - clipped[:, 0]) * (clipped[:, 3] - clipped[:, 1]) > min_keep * area) & ((clipped[:, 2] - clipped[:, 0]) >= 6)
    return clipped[keep]


class PlateDetection(Dataset):
    def __init__(self, samples: list[Sample], train: bool, seed: int = 0):
        self.samples, self.train, self.seed = samples, train, seed

    def __len__(self):
        return len(self.samples)

    def __getitem__(self, i):
        rng = random.Random(hash((self.seed, i, time.time_ns())) if self.train else self.seed * 1_000_003 + i)
        s = self.samples[i]
        img = cv2.imdecode(np.fromfile(s.path, np.uint8), cv2.IMREAD_COLOR) if Path(s.path).exists() else None  # non-ASCII safe
        if img is None:
            img, boxes, ignore = np.zeros((IN_H, IN_W, 3), np.uint8), np.zeros((0, 4), np.float32), np.zeros((0, 4), np.float32)
        else:
            img = cv2.cvtColor(img, cv2.COLOR_BGR2RGB)
            H, W = img.shape[:2]
            scale = np.array([W, H, W, H], np.float32) if s.normalized else 1
            boxes, ignore = s.boxes * scale, s.ignore * scale
            if s.domain == "openimages":
                for b in boxes:
                    if rng.random() < (0.55 if self.train else 0.5):
                        paste_japanese(img, b, rng)
            if s.domain == "dashcam":
                img, boxes, ignore = self.frame_roi(img, boxes, ignore, rng)
            else:
                img, boxes, ignore = self.frame(img, boxes, ignore, rng)
        if self.train:
            if rng.random() < 0.5:
                img = img[:, ::-1].copy()  # plates are symmetric enough for detection
                boxes, ignore = boxes.copy(), ignore.copy()
                boxes[:, [0, 2]] = IN_W - boxes[:, [2, 0]]
                ignore[:, [0, 2]] = IN_W - ignore[:, [2, 0]]
            img = self.photometric(img, rng)
        target, ign = np.zeros((MAX_BOXES, 4), np.float32), np.zeros((MAX_BOXES, 4), np.float32)
        n, m = min(len(boxes), MAX_BOXES), min(len(ignore), MAX_BOXES)
        target[:n], ign[:m] = boxes[:n], ignore[:m]
        return torch.from_numpy(np.ascontiguousarray(img)).permute(2, 0, 1), torch.from_numpy(target), n, torch.from_numpy(ign), m

    def frame_roi(self, img, boxes, ignore, rng):
        """Dash-camera frames: crop the engine ROI (jittered when training) and
        scale it to the input exactly as plate_engine.cc does."""
        x, y, w, h = ROI
        if self.train:
            f = rng.uniform(0.8, 1.25)
            cx, cy = x + w / 2 + rng.uniform(-0.12, 0.12) * w, y + h / 2 + rng.uniform(-0.1, 0.1) * h
            w, h = w * f, h * f
            x, y = cx - w / 2, cy - h / 2
        s = min(IN_W / w, IN_H / h)
        M = np.array([[s, 0, -x * s], [0, s, -y * s]], np.float32)
        out = cv2.warpAffine(img, M, (IN_W, IN_H), flags=cv2.INTER_AREA if s < 1 else cv2.INTER_LINEAR, borderValue=(114, 114, 114))
        offset = np.array([x, y, x, y], np.float32)
        return out, clip_boxes((boxes - offset) * s), clip_boxes((ignore - offset) * s, 0.2)

    def frame(self, img, boxes, ignore, rng):
        """Photos: scale so a plate is 14..110 px wide in the input, then crop."""
        H, W = img.shape[:2]
        if self.train and len(boxes):
            ref = boxes[rng.randrange(len(boxes))]
            target_w = math.exp(rng.uniform(math.log(14), math.log(110)))
            s = target_w / max(1.0, ref[2] - ref[0])
            s = min(max(s, IN_W / W * 0.6), 3.0)
        else:
            s = min(IN_W / W, IN_H / H)
        img = cv2.resize(img, (max(1, int(W * s)), max(1, int(H * s))), interpolation=cv2.INTER_AREA if s < 1 else cv2.INTER_LINEAR)
        boxes, ignore = boxes * s, ignore * s
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
        off = np.array([ox, oy, ox, oy], np.float32)
        return canvas, clip_boxes(boxes + off), clip_boxes(ignore + off, 0.2)

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

    def forward(self, x):  # export path: [N, GH*GW, 5] = (cx, cy, w, h, score)
        # Flatten in NHWC order first so the converter needs no layout guesses.
        r = self.raw(x).permute(0, 2, 3, 1).reshape(x.shape[0], GH * GW, 5)
        s = torch.sigmoid(r)
        gx, gy = self.gx.reshape(1, GH * GW, 1), self.gy.reshape(1, GH * GW, 1)
        cx = (gx + s[..., 0:1] * 2 - 0.5) / GW
        cy = (gy + s[..., 1:2] * 2 - 0.5) / GH
        return torch.cat([cx, cy, s[..., 2:3] * MAX_WH, s[..., 3:4] * MAX_WH, s[..., 4:5]], 2)


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


def ignore_mask(ignore, ign_counts, like):
    """True where a cell center lies inside an uncertain plate box (no objectness loss)."""
    mask = torch.zeros_like(like, dtype=torch.bool)
    ys = (torch.arange(GH, device=like.device).float() + 0.5) * STRIDE
    xs = (torch.arange(GW, device=like.device).float() + 0.5) * STRIDE
    for b in range(ignore.shape[0]):
        for k in range(int(ign_counts[b])):
            x0, y0, x1, y1 = ignore[b, k]
            mask[b] |= ((ys[:, None] >= y0) & (ys[:, None] <= y1)) & ((xs[None, :] >= x0) & (xs[None, :] <= x1))
    return mask


def loss_fn(model, raw, boxes, counts, ignore=None, ign_counts=None):
    b, ys, xs, tb = build_targets(boxes, counts)
    obj_logit = raw[:, 4]
    obj_target = torch.zeros_like(obj_logit)
    weight = torch.ones_like(obj_logit)
    if ignore is not None:
        weight[ignore_mask(ignore, ign_counts, obj_logit)] = 0
    box_loss = raw.sum() * 0
    if len(b):
        cx, cy, w, h = Detector.decode(raw, model.gx, model.gy)
        pred = torch.stack([cx[b, 0, ys, xs] - w[b, 0, ys, xs] / 2, cy[b, 0, ys, xs] - h[b, 0, ys, xs] / 2,
                            cx[b, 0, ys, xs] + w[b, 0, ys, xs] / 2, cy[b, 0, ys, xs] + h[b, 0, ys, xs] / 2], 1)
        g, iou = giou(pred, tb)
        box_loss = (1 - g).mean()
        obj_target[b, ys, xs] = iou.detach().clamp(0.3, 1)  # IoU-aware objectness
        weight[b, ys, xs] = 1
    p = torch.sigmoid(obj_logit)
    bce = F.binary_cross_entropy_with_logits(obj_logit, obj_target, reduction="none")
    focal = bce * ((obj_target - p).abs() ** 2) * weight  # quality-focal style
    obj_loss = focal.sum() / max(1, len(b))
    return obj_loss + 5 * box_loss, float(box_loss)


@torch.no_grad()
def evaluate(model, loader, device, thr=0.35):
    """Precision/recall at IoU 0.5; detections on uncertain (ignored) plates
    are neither true nor false positives."""
    model.eval()
    tp = fp = fn = 0
    small_tp = small_n = 0
    for x, boxes, counts, ignore, ign_counts in loader:
        out = model(x.to(device).float() / 255).cpu()
        for i in range(out.shape[0]):
            o = out[i]
            o = o[o[:, 4] > thr]
            pred = torch.stack([(o[:, 0] - o[:, 2] / 2) * IN_W, (o[:, 1] - o[:, 3] / 2) * IN_H, (o[:, 0] + o[:, 2] / 2) * IN_W, (o[:, 1] + o[:, 3] / 2) * IN_H], 1)
            pred = pred[torchvision.ops.nms(pred, o[:, 4], 0.5)] if len(pred) else pred
            gt = boxes[i, : int(counts[i])]
            ign = ignore[i, : int(ign_counts[i])]
            matched = set()
            for pi in range(len(pred)):
                if len(gt):
                    ious = torchvision.ops.box_iou(pred[pi:pi + 1], gt)[0]
                    j = int(ious.argmax())
                    if ious[j] >= 0.5 and j not in matched:
                        matched.add(j)
                        tp += 1
                        continue
                if len(ign) and torchvision.ops.box_iou(pred[pi:pi + 1], ign).max() >= 0.3:
                    continue
                fp += 1
            fn += len(gt) - len(matched)
            for j, g in enumerate(gt):
                if g[2] - g[0] < 40:
                    small_n += 1
                    small_tp += j in matched
    model.train()
    precision, recall = tp / max(1, tp + fp), tp / max(1, tp + fn)
    return {"precision": precision, "recall": recall, "f1": 2 * precision * recall / max(1e-9, precision + recall), "smallRecall": small_tp / max(1, small_n), "smallN": small_n, "tp": tp, "fp": fp, "fn": fn}


def collate(batch):
    return (torch.stack([b[0] for b in batch]), torch.stack([b[1] for b in batch]), torch.tensor([b[2] for b in batch]),
            torch.stack([b[3] for b in batch]), torch.tensor([b[4] for b in batch]))


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--data", type=Path, required=True, help="Open Images subset (fetch_openimages.py)")
    ap.add_argument("--photos", type=Path, nargs="*", default=[], help="Lipla labels for photos")
    ap.add_argument("--photo-root", type=Path)
    ap.add_argument("--dashcam", type=Path, nargs="*", default=[], help="Lipla labels for dash-camera train frames")
    ap.add_argument("--dashcam-root", type=Path)
    ap.add_argument("--mix", type=float, nargs=3, default=[0.4, 0.25, 0.35], help="sampling share: openimages photo dashcam")
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--epochs", type=int, default=40)
    ap.add_argument("--epoch-size", type=int, default=8000)
    ap.add_argument("--batch", type=int, default=48)
    ap.add_argument("--lr", type=float, default=2e-3)
    ap.add_argument("--workers", type=int, default=16)
    ap.add_argument("--init", type=Path)
    ap.add_argument("--export-only", action="store_true", help="export --out/detector.pt without training")
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    oi_train, oi_val = split_groups(openimages_samples(args.data), 0.05)
    ph = [s for f in args.photos for s in jsonl_samples(f, args.photo_root, "photo")]
    dc = [s for f in args.dashcam for s in jsonl_samples(f, args.dashcam_root, "dashcam")]
    ph_train, ph_val = split_groups(ph, 0.15)
    dc_train, dc_val = split_groups(dc, 0.12)  # split by clip: no shared clips
    train_samples = oi_train + ph_train + dc_train
    shares = dict(zip(("openimages", "photo", "dashcam"), args.mix))
    sizes = {d: sum(s.domain == d for s in train_samples) for d in shares}
    weights = [shares[s.domain] / max(1, sizes[s.domain]) for s in train_samples]
    sampler = torch.utils.data.WeightedRandomSampler(weights, args.epoch_size, replacement=True)
    device = "cuda"
    model = Detector().to(device)
    if args.init:
        model.load_state_dict(torch.load(args.init, map_location=device))
    train = DataLoader(PlateDetection(train_samples, True), batch_size=args.batch, sampler=sampler, num_workers=args.workers, collate_fn=collate, persistent_workers=True, drop_last=True)
    vals = {name: DataLoader(PlateDetection(v, False), batch_size=32, num_workers=6, collate_fn=collate)
            for name, v in (("dashcam", dc_val), ("photo", ph_val), ("openimages", oi_val)) if v}
    primary = "dashcam" if "dashcam" in vals else next(iter(vals))
    if args.export_only:
        export(model, args.out, vals[primary])
        return
    opt = torch.optim.AdamW(model.parameters(), lr=args.lr, weight_decay=5e-4)
    sched = torch.optim.lr_scheduler.OneCycleLR(opt, max_lr=args.lr, total_steps=args.epochs * len(train), pct_start=0.05)
    history, best, t0 = [], -1.0, time.time()
    print(json.dumps({"train": sizes, "val": {k: len(v.dataset) for k, v in vals.items()}}), flush=True)
    for epoch in range(1, args.epochs + 1):
        for x, boxes, counts, ignore, ign_counts in train:
            x, boxes, ignore = x.to(device, non_blocking=True).float() / 255, boxes.to(device), ignore.to(device)
            with torch.autocast("cuda", dtype=torch.bfloat16):
                raw = model.raw(x)
            loss, box = loss_fn(model, raw.float(), boxes, counts, ignore, ign_counts)
            opt.zero_grad(set_to_none=True)
            loss.backward()
            opt.step()
            sched.step()
        m = {name: evaluate(model, v, device) for name, v in vals.items()}
        m.update(epoch=epoch, loss=float(loss), box=box, minutes=round((time.time() - t0) / 60, 1))
        history.append(m)
        print(json.dumps(m), flush=True)
        # Model selection pools photo and dash-camera validation: the dash-camera
        # split alone has few confident teacher plates and counts plates the
        # teacher missed as false positives.
        pool = [m[k] for k in ("photo", "dashcam") if k in m]
        tp, fp, fn = sum(x["tp"] for x in pool), sum(x["fp"] for x in pool), sum(x["fn"] for x in pool)
        score = 2 * tp / max(1, 2 * tp + fp + fn)
        m["selectionF1"] = score
        torch.save(model.state_dict(), args.out / "detector-last.pt")
        if score > best:
            best = score
            torch.save(model.state_dict(), args.out / "detector.pt")
    (args.out / "metrics.json").write_text(json.dumps({"selection": "photo+dashcam pooled F1", "best_f1": best, "history": history}, indent=2))
    export(model, args.out, vals[primary])
    print("best f1", best)


def export(model, out: Path, val):
    model.load_state_dict(torch.load(out / "detector.pt"))
    model.eval().cpu()
    calib = []
    for batch in val:
        calib.append((batch[0].float() / 255).permute(0, 2, 3, 1).numpy())
        if sum(len(c) for c in calib) >= 200:
            break
    np.save(out / "calibration.npy", np.concatenate(calib)[:200].astype(np.float32))
    torch.onnx.export(model, torch.zeros(1, 3, IN_H, IN_W), out / "detector.onnx", input_names=["image"], output_names=["boxes"], opset_version=13, dynamo=False)


if __name__ == "__main__":
    main()
