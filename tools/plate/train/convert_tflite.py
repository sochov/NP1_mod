"""Convert trained ONNX models to int8 TFLite and assemble the model package.

Run with the conversion venv (TensorFlow 2.16.1, matching the engine runtime):

    O:/NP1_plate_data/venv-convert/Scripts/python.exe tools/plate/train/convert_tflite.py \
        --reader O:/NP1_plate_data/reader-v1 --detector O:/NP1_plate_data/detector-v1 \
        --out O:/NP1_plate_data/package-v1

The package directory then contains detector.tflite, ocr.tflite and
plate-model.json, ready for /overlay/owner-apps/np1-control/plate/models.
Quantization error is measured against ONNX Runtime and written to
conversion-report.json; a large drop fails the conversion.
"""
from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
from pathlib import Path

import numpy as np


def onnx2tf(onnx_path: Path, calib: Path, work: Path, channels: int, precision: str = "int8") -> Path:
    if work.exists():
        shutil.rmtree(work)
    mean = json.dumps([[[[0.0] * channels]]])
    std = json.dumps([[[[1.0] * channels]]])
    cmd = [sys.executable, "-m", "onnx2tf", "-i", str(onnx_path), "-o", str(work), "-oiqt", "-qt", "per-channel",
           "-cind", "image", str(calib), mean, std, "-n"]
    subprocess.run(cmd, check=True)
    out = work / (f"{onnx_path.stem}_integer_quant.tflite" if precision == "int8" else f"{onnx_path.stem}_float32.tflite")
    if not out.exists():
        raise SystemExit(f"onnx2tf did not produce {out}")
    return out


def run_tflite(path: Path, batch: np.ndarray) -> np.ndarray:
    import tensorflow as tf
    interp = tf.lite.Interpreter(model_path=str(path))
    interp.allocate_tensors()
    inp, out = interp.get_input_details()[0], interp.get_output_details()[0]
    results = []
    for x in batch:
        interp.set_tensor(inp["index"], x[None].astype(inp["dtype"]))
        interp.invoke()
        results.append(interp.get_tensor(out["index"])[0].copy())
    return np.stack(results)


def run_onnx(path: Path, batch_nhwc: np.ndarray) -> np.ndarray:
    import onnxruntime as ort
    sess = ort.InferenceSession(str(path), providers=["CPUExecutionProvider"])
    name = sess.get_inputs()[0].name
    return np.concatenate([sess.run(None, {name: x[None].transpose(0, 3, 1, 2)})[0] for x in batch_nhwc])


def reader_agreement(onnx_out: np.ndarray, tfl_out: np.ndarray, heads: list[dict]) -> dict:
    offset, agree, number_agree = 0, [], np.ones(len(onnx_out), bool)
    for h in heads:
        n = len(h["labels"])
        same = onnx_out[:, offset:offset + n].argmax(1) == tfl_out[:, offset:offset + n].argmax(1)
        agree.append(float(same.mean()))
        if h["field"] == "number":
            number_agree &= same
        offset += n
    return {"minHeadAgreement": min(agree), "numberAgreement": float(number_agree.mean())}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--reader", type=Path, required=True)
    ap.add_argument("--detector", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--roi", type=int, nargs=4, default=[320, 240, 1280, 768], help="detector ROI x y w h in the 1920x1080 frame")
    ap.add_argument("--score", type=float, default=0.4)
    ap.add_argument("--reader-precision", choices=["float32", "int8"], default="float32",
                    help="int8 lost up to ~10%% head agreement on the probe model; accuracy first")
    ap.add_argument("--detector-precision", choices=["float32", "int8"], default="int8")
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    work = args.out / "work"
    report = {}

    heads = json.loads((args.reader / "heads.json").read_text(encoding="utf-8"))
    r_calib = np.load(args.reader / "calibration.npy")
    r_tfl = onnx2tf(args.reader / "reader.onnx", args.reader / "calibration.npy", work / "reader", 1, args.reader_precision)
    agreement = reader_agreement(run_onnx(args.reader / "reader.onnx", r_calib[:300]), run_tflite(r_tfl, r_calib[:300]).reshape(300, -1), heads)
    report["reader"] = agreement
    report["reader"]["precision"] = args.reader_precision
    if agreement["numberAgreement"] < 0.97:
        raise SystemExit(f"reader quantization drop too large: {agreement}")

    d_calib = np.load(args.detector / "calibration.npy")
    d_tfl = onnx2tf(args.detector / "detector.onnx", args.detector / "calibration.npy", work / "detector", 3, args.detector_precision)
    o, t = run_onnx(args.detector / "detector.onnx", d_calib[:60]), run_tflite(d_tfl, d_calib[:60])
    if t.shape != o.shape:
        t = t.reshape(o.shape) if t.size == o.size else t
    o_score, t_score = o[:, 4], t[:, 4]
    hot = o_score > args.score
    report["detector"] = {
        "scoreMAE": float(np.abs(o_score - t_score).mean()),
        "positiveCells": int(hot.sum()),
        "positiveAgreement": float(((t_score > args.score) == hot)[hot].mean()) if hot.any() else None,
        "precision": args.detector_precision,
        "boxMAEOnPositives": float(np.abs(o[:, :4] - t[:, :4]).transpose(0, 2, 1)[hot].mean()) if hot.any() else None,
    }

    shutil.copy(d_tfl, args.out / "detector.tflite")
    shutil.copy(r_tfl, args.out / "ocr.tflite")
    config = {
        "version": 1,
        "detector": {"scoreThreshold": args.score, "iouThreshold": 0.5, "maxPlates": 4, "minWidth": 24, "roi": args.roi},
        "ocr": {"margin": 0.06, "fieldThreshold": 0.6, "heads": heads},
    }
    (args.out / "plate-model.json").write_text(json.dumps(config, ensure_ascii=False), encoding="utf-8")
    (args.out / "conversion-report.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
    shutil.rmtree(work, ignore_errors=True)
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
