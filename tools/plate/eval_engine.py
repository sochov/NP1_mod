"""End-to-end evaluation of the plate engine on still photos.

Each photo is placed into a 1920x1080 frame at several scales (to emulate
distance), converted to video-range NV12 exactly like the NP1 camera export,
and sent through the engine protocol. With a labels file the script reports
detection recall and serial-number accuracy per scale; it always writes
annotated JPEGs for visual review.

    python tools/plate/eval_engine.py --package O:/NP1_plate_data/package-v1 \
        --images O:/NP1_plate_data/commons/images --labels O:/NP1_plate_data/commons/labels.json \
        --out O:/NP1_plate_data/eval-v1

Labels JSON: {"<file stem>": [{"number": "1234", "region": "品川", "class": "300", "kana": "あ"}, ...]}
Only "number" is required; other fields are compared when present.
"""
from __future__ import annotations

import argparse
import json
import struct
import subprocess
import sys
from pathlib import Path

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[2]
W, H = 1920, 1080


def wsl_path(p: Path) -> str:
    p = p.resolve()
    return "/mnt/" + p.drive[0].lower() + p.as_posix()[2:]


def rgb_to_nv12(rgb: np.ndarray) -> bytes:
    """Full-range RGB -> video-range BT.601 NV12 (inverse of the engine)."""
    f = rgb.astype(np.float32)
    r, g, b = f[..., 0], f[..., 1], f[..., 2]
    y = 16 + (65.738 * r + 129.057 * g + 25.064 * b) / 256
    u = 128 + (-37.945 * r - 74.494 * g + 112.439 * b) / 256
    v = 128 + (112.439 * r - 94.154 * g - 18.285 * b) / 256
    y = np.clip(np.rint(y), 0, 255).astype(np.uint8)
    uv = np.stack([u.reshape(H // 2, 2, W // 2, 2).mean((1, 3)), v.reshape(H // 2, 2, W // 2, 2).mean((1, 3))], -1)
    return y.tobytes() + np.clip(np.rint(uv), 0, 255).astype(np.uint8).tobytes()


def place(img: np.ndarray, scale: float) -> tuple[np.ndarray, float, int, int]:
    """Fit the photo into the frame, then shrink by `scale` around the center."""
    h, w = img.shape[:2]
    s = min(W / w, H / h) * scale
    small = cv2.resize(img, (max(2, int(w * s)), max(2, int(h * s))), interpolation=cv2.INTER_AREA)
    frame = np.full((H, W, 3), 96, np.uint8)
    ox, oy = (W - small.shape[1]) // 2, (H - small.shape[0]) // 2
    frame[oy:oy + small.shape[0], ox:ox + small.shape[1]] = small
    return frame, s, ox, oy


class EngineProcess:
    def __init__(self, engine: Path, package: Path, config: Path, host: bool):
        args = ["--detector", package / "detector.tflite", "--ocr", package / "ocr.tflite", "--config", config, "--threads", "4"]
        if host:
            cmd = ["wsl.exe", "-d", "Ubuntu", "--", wsl_path(engine)] + [wsl_path(a) if isinstance(a, Path) else a for a in args]
        else:
            cmd = [str(engine)] + [str(a) for a in args]
        self.p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE)
        ready = json.loads(self.p.stdout.readline())
        if not ready.get("ready"):
            raise SystemExit(f"engine not ready: {ready}")
        self.info = ready
        self.seq = 0

    def run(self, nv12: bytes) -> dict:
        self.seq += 1
        self.p.stdin.write(b"NPF1" + struct.pack("<IIIQ", W, H, 1, self.seq) + nv12)
        self.p.stdin.flush()
        reply = json.loads(self.p.stdout.readline())
        assert reply["seq"] == self.seq, reply
        return reply

    def close(self):
        self.p.stdin.close()
        self.p.wait(timeout=10)


def annotate(frame: np.ndarray, reply: dict, path: Path):
    img = Image.fromarray(frame)
    d = ImageDraw.Draw(img)
    try:
        font = ImageFont.truetype("C:/Windows/Fonts/YuGothB.ttc", 26)
    except Exception:
        font = ImageFont.load_default()
    for p in reply["plates"]:
        x, y, w, h = p["box"]
        r = p["reading"]
        d.rectangle((x, y, x + w, y + h), outline=(255, 64, 0), width=3)
        label = f'{r["region"]} {r["class"]} {r["kana"]} {r["number"]} ({r["confidence"]:.2f})'
        d.text((x, max(0, y - 32)), label, fill=(255, 255, 0), font=font, stroke_width=2, stroke_fill=(0, 0, 0))
    img.save(path, quality=85)


def box_iou(a, b):
    ix = max(0.0, min(a[2], b[2]) - max(a[0], b[0]))
    iy = max(0.0, min(a[3], b[3]) - max(a[1], b[1]))
    inter = ix * iy
    return inter / ((a[2] - a[0]) * (a[3] - a[1]) + (b[2] - b[0]) * (b[3] - b[1]) - inter + 1e-6)


def evaluate_frames(engine, labels: Path, root: Path, out: Path, limit: int, roi) -> dict:
    """Dash-camera frames (already 1920x1080) against Lipla teacher labels.
    Only confident teacher plates inside the engine ROI count as references."""
    bins = [(0, 40), (40, 70), (70, 110), (110, 10000)]
    stats = {f"{lo}-{hi if hi < 10000 else ''}px": {"refs": 0, "detected": 0, "number": 0, "fields": 0, "fieldRefs": 0} for lo, hi in bins}
    unmatched, frames, ms, examples = 0, 0, [], []
    # Per field: correct / wrong / blank (unread) on detected teacher plates.
    per_field = {f: {"correct": 0, "wrong": 0, "blank": 0} for f in ("number", "region", "class", "kana")}
    lines = [json.loads(l) for l in labels.read_text(encoding="utf-8").splitlines() if l.strip()]
    lines = [l for l in lines if "plates" in l][: limit or None]
    for item in lines:
        rgb = cv2.cvtColor(cv2.imread(str(root / item["file"])), cv2.COLOR_BGR2RGB)
        if rgb.shape[:2] != (H, W):
            rgb = cv2.resize(rgb, (W, H), interpolation=cv2.INTER_AREA)
        reply = engine.run(rgb_to_nv12(rgb))
        frames += 1
        ms.append(reply.get("ms", 0))
        reads = [(p, [p["box"][0], p["box"][1], p["box"][0] + p["box"][2], p["box"][1] + p["box"][3]]) for p in reply["plates"]]
        all_teacher = []
        for t in item["plates"]:
            q = np.asarray(t["quad"], np.float32)
            all_teacher.append([*q.min(0), *q.max(0)])
        used = set()
        for t, tb in zip(item["plates"], all_teacher):
            if t.get("unread") or t["score"] < 0.75 or t["numberScore"] < 0.9:
                continue
            cx, cy = (tb[0] + tb[2]) / 2, (tb[1] + tb[3]) / 2
            if not (roi[0] <= cx <= roi[0] + roi[2] and roi[1] <= cy <= roi[1] + roi[3]):
                continue
            width = tb[2] - tb[0]
            key = next(k for (lo, hi), k in zip(bins, stats) if lo <= width < hi)
            st = stats[key]
            st["refs"] += 1
            best = max(range(len(reads)), key=lambda i: box_iou(reads[i][1], tb), default=None)
            if best is None or box_iou(reads[best][1], tb) < 0.3:
                continue
            used.add(best)
            st["detected"] += 1
            r = reads[best][0]["reading"]
            pf = per_field["number"]
            pf["correct" if r["number"] == t["number"] else ("blank" if not r["number"] else "wrong")] += 1
            for field, key2, score in (("region", "area", "areaScore"), ("class", "classNumber", "classScore"), ("kana", "kana", "kanaScore")):
                if t[score] >= 0.85:
                    pf = per_field[field]
                    pf["blank" if not r[field] else ("correct" if r[field] == t[key2] else "wrong")] += 1
            if r["number"] == t["number"]:
                st["number"] += 1
                if len(examples) < 40:
                    examples.append({"file": item["file"], "teacher": t["number"], "engine": r})
            for field, key2, score in (("region", "area", "areaScore"), ("class", "classNumber", "classScore"), ("kana", "kana", "kanaScore")):
                if t[score] >= 0.85 and r[field]:
                    st["fieldRefs"] += 1
                    st["fields"] += r[field] == t[key2]
        for i, (_, rb) in enumerate(reads):
            if i not in used and all(box_iou(rb, tb) < 0.3 for tb in all_teacher):
                unmatched += 1
        if reads:
            annotate(rgb, reply, out / f"{Path(item['file']).stem}.jpg")
    total = {k: sum(v[k] for v in stats.values()) for k in ("refs", "detected", "number", "fields", "fieldRefs")}
    rate = lambda a, b: round(a / b, 3) if b else None
    summary = {"frames": frames, "avgMs": float(np.mean(ms)) if ms else None, "unmatchedReads": unmatched,
               "overall": {**total, "detectionRate": rate(total["detected"], total["refs"]), "numberAccuracyOfRefs": rate(total["number"], total["refs"]),
                           "numberAccuracyOfDetected": rate(total["number"], total["detected"]), "readFieldAccuracy": rate(total["fields"], total["fieldRefs"])},
               "byPlateWidth": {k: {**v, "detectionRate": rate(v["detected"], v["refs"]), "numberAccuracyOfRefs": rate(v["number"], v["refs"])} for k, v in stats.items()},
               "perField": per_field, "examples": examples}
    return summary


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--package", type=Path, required=True)
    ap.add_argument("--images", type=Path, required=True)
    ap.add_argument("--labels", type=Path)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--scales", type=float, nargs="+", default=[1.0, 0.5, 0.3])
    ap.add_argument("--engine", type=Path, default=ROOT / "artifacts/plate/plate-engine-host")
    ap.add_argument("--limit", type=int, default=0)
    ap.add_argument("--lipla-labels", type=Path, help="evaluate dash-camera frames (--images is their root) against these teacher labels")
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    labels = json.loads(args.labels.read_text(encoding="utf-8")) if args.labels else {}
    config = json.loads((args.package / "plate-model.json").read_text(encoding="utf-8"))
    roi = config["detector"]["roi"]
    if not args.lipla_labels:
        config["detector"]["roi"] = [0, 0, W, H]  # photos: plates can be anywhere
    cfg_path = args.out / "eval-config.json"
    cfg_path.write_text(json.dumps(config, ensure_ascii=False), encoding="utf-8")
    engine = EngineProcess(args.engine, args.package, cfg_path, host=args.engine.name.endswith("-host"))
    if args.lipla_labels:
        summary = evaluate_frames(engine, args.lipla_labels, args.images, args.out, args.limit, roi)
        engine.close()
        (args.out / "frames-summary.json").write_text(json.dumps({"engine": engine.info, **summary}, ensure_ascii=False, indent=1), encoding="utf-8")
        print(json.dumps({k: v for k, v in summary.items() if k != "examples"}, ensure_ascii=False, indent=1))
        return
    files = sorted(p for p in args.images.iterdir() if p.suffix.lower() in (".jpg", ".jpeg", ".png"))
    if labels:
        files = [p for p in files if p.stem in labels]
    if args.limit:
        files = files[: args.limit]
    stats = {s: {"plates": 0, "detected": 0, "number": 0, "all": 0, "falseReads": 0, "ms": []} for s in args.scales}
    results = {}
    for f in files:
        rgb = cv2.cvtColor(cv2.imread(str(f)), cv2.COLOR_BGR2RGB)
        for s in args.scales:
            frame, *_ = place(rgb, s)
            reply = engine.run(rgb_to_nv12(frame))
            st = stats[s]
            st["ms"].append(reply.get("ms", 0))
            results.setdefault(f.stem, {})[str(s)] = reply["plates"]
            if s == args.scales[0] or labels:
                annotate(frame, reply, args.out / f"{f.stem}-{s}.jpg")
            if f.stem in labels:
                truth = labels[f.stem]
                st["plates"] += len(truth)
                reads = [p["reading"] for p in reply["plates"]]
                st["detected"] += min(len(truth), len(reads))
                used = set()
                for t in truth:
                    for i, r in enumerate(reads):
                        if i not in used and r["number"] == t["number"]:
                            used.add(i)
                            st["number"] += 1
                            st["all"] += all(r.get(k) == t[k] for k in ("region", "class", "kana") if k in t)
                            break
                st["falseReads"] += len(reads) - len(used)
    engine.close()
    summary = {}
    for s, st in stats.items():
        n = max(1, st["plates"])
        summary[str(s)] = {"plates": st["plates"], "detectionRate": st["detected"] / n, "numberAccuracy": st["number"] / n,
                           "allFieldsAccuracy": st["all"] / n, "falseReads": st["falseReads"], "avgMs": float(np.mean(st["ms"])) if st["ms"] else None}
    (args.out / "results.json").write_text(json.dumps({"engine": engine.info, "summary": summary, "results": results}, ensure_ascii=False, indent=1), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
