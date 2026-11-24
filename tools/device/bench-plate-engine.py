"""Compare NP1 plate backends on the same inputs and verify stock health.

Models are staged on the owner overlay and executables in /run. Both staging
directories are unique to this run and removed on exit. The report contains
timings and agreement counts only. Raw inference output stays in memory.
"""
import argparse
import filecmp
import gzip
import hashlib
import json
import math
import re
import shlex
import statistics
import struct
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools" / "plate"))
from bench_inference import Device, compare_stock, stock_snapshot

MODEL_FILES = (
    "detector.tflite", "ocr.tflite", "ocr-kana.tflite", "ocr-classification.tflite",
    "ppocrv6_rec.onnx", "ppocr-dictionary.json", "plate-model.json",
)
FAST_TEXT_MODEL = "ppocrv6_rec_np1_int8.onnx"
GPU_TEXT_MODEL = "ppocrv6_rec_np1_w8.mnn"
OPTIONAL_MODELS = (FAST_TEXT_MODEL, GPU_TEXT_MODEL)
INSTALLED_MODELS = "/overlay/owner-apps/np1-control/plate/models"
WIDTH, HEIGHT = 1920, 1080
FRAME_BYTES = WIDTH * HEIGHT * 3 // 2


def make_frames(path):
    if path is None:
        return b"", 0
    data = path.read_bytes()
    if not data or len(data) % FRAME_BYTES:
        raise ValueError("Input must contain complete 1920x1080 NV12 frames")
    count = len(data) // FRAME_BYTES
    if count > 64:
        raise ValueError("Use at most 64 frames per run")
    packets = []
    for seq in range(count):
        packets.append(struct.pack("<4sIIIQ", b"NPF1", WIDTH, HEIGHT, 1, seq + 1))
        packets.append(data[seq * FRAME_BYTES:(seq + 1) * FRAME_BYTES])
    return b"".join(packets), count


def model_files(local):
    """Include the files referenced by text OCR and numeric verification."""
    names = set(MODEL_FILES)
    names.update(name for name in OPTIONAL_MODELS if (local / name).is_file())
    config = json.loads((local / "plate-model.json").read_text(encoding="utf-8"))

    def add_paths(value):
        if isinstance(value, dict):
            for item in value.values():
                add_paths(item)
        elif isinstance(value, list):
            for item in value:
                add_paths(item)
        elif isinstance(value, str) and Path(value).suffix in (".tflite", ".onnx", ".mnn", ".json"):
            if not re.fullmatch(r"[A-Za-z0-9_.-]+", value):
                raise ValueError(f"Use a bundle-relative model filename: {value}")
            names.add(value)

    add_paths(config)
    return sorted(names)


def stage_models(device, local, remote, *, shared=None, reuse_installed=False):
    for name in model_files(local):
        target = remote + "/" + name
        if name != "plate-model.json" and reuse_installed:
            source = INSTALLED_MODELS + "/" + name
            _, digest = device.shell("sha256sum " + shlex.quote(source) + " 2>/dev/null")
            local_hash = hashlib.sha256()
            with (local / name).open("rb") as stream:
                for chunk in iter(lambda: stream.read(1 << 20), b""):
                    local_hash.update(chunk)
            if digest.split()[:1] == [local_hash.hexdigest()]:
                code, _ = device.shell(shlex.join(["ln", "-s", source, target]))
                if not code:
                    continue
        if shared and (shared[0] / name).is_file() and filecmp.cmp(local / name, shared[0] / name, shallow=False):
            command = shlex.join(["ln", shared[1] + "/" + name, target])
            _, result = device.shell(command + " && echo linked")
            if result.strip() == "linked":
                continue
        device.push(local / name, target)


def run_engine(device, adb, path, models, runtime, options, *, frames=None, service_limits=False, memory_mb=384):
    args = [
        path, "--detector", models + "/detector.tflite", "--ocr", models + "/ocr.tflite",
        "--config", models + "/plate-model.json", *options,
    ]
    log = runtime + "/engine.log"
    scope = f"systemd-run --scope --quiet -p CPUQuota=150% -p MemoryLimit={memory_mb}M " if service_limits else ""
    timer_ok, _ = device.shell("test -x /usr/bin/time && /usr/bin/time -f '' /bin/true 2>/dev/null")
    timer = "/usr/bin/time -f 'peak-rss-kb %M' " if not timer_ok else ""
    compressed = frames is not None and frames.endswith(".gz")
    command = (f"LD_LIBRARY_PATH={shlex.quote(runtime)} exec {scope}nice -n 10 {timer}"
               f"{shlex.join(args)} 2> {shlex.quote(log)}")
    if compressed:
        command = f"nice -n 10 gzip -dc {shlex.quote(frames)} | {command}"
    else:
        command += f" < {shlex.quote(frames or '/dev/null')}"
    result = subprocess.run(
        [*adb, "exec-out", command], capture_output=True, timeout=180,
    )
    _, detail = device.shell("cat " + shlex.quote(log))
    try:
        rows = [json.loads(line) for line in result.stdout.splitlines()]
    except (ValueError, UnicodeError) as exc:
        raise RuntimeError("Invalid engine JSON stream") from exc
    if result.returncode or not rows or not rows[0].get("ready"):
        message = rows[0].get("error", "") if rows else ""
        transport = result.stderr.decode(errors="replace").replace(device.serial, "[device]").strip()
        diagnostic = detail[-2400:].strip() or transport
        raise RuntimeError(f"Engine did not start ({result.returncode}): {message}\n{diagnostic}")
    return rows[0], rows[1:], detail


def summarize_frames(rows, count):
    if len(rows) != count or any(r.get("seq") != i or r.get("error") for i, r in enumerate(rows, 1)):
        raise RuntimeError("Engine did not finish the complete frame sequence")
    times = [r["ms"] for r in rows]
    readings, recording = [], []
    for row in rows:
        frame_readings, frame_recording = [], []
        for plate in row["plates"]:
            reading = plate["reading"]
            fields = tuple(reading.get(key, "") for key in ("region", "class", "kana", "number"))
            frame_readings.append(fields)
            confidence = reading.get("confidence", 0)
            if fields[3] and confidence >= .6:
                immediate = confidence >= .7 and plate["box"][2] >= 45
                frame_recording.append((*fields, immediate))
        readings.append(sorted(frame_readings))
        recording.append(sorted(frame_recording))
    data = {
        "frames": count, "positiveFrames": sum(bool(r) for r in readings),
        "readableFrames": sum(any(reading[3] for reading in frame) for frame in readings),
        "medianMs": round(statistics.median(times), 2),
        "meanMs": round(statistics.fmean(times), 2),
        "p95Ms": round(sorted(times)[math.ceil(.95 * len(times)) - 1], 2),
        "positiveMedianMs": round(statistics.median(
            row["ms"] for row in rows if row["plates"]), 2) if any(readings) else None,
        "maxMs": round(max(times), 2),
        "runtimeBackends": sorted({r["backend"] for r in rows if "backend" in r}),
    }
    profiles = [row["profile"] for row in rows if row["plates"] and "profile" in row]
    if profiles:
        data["positiveProfileMedian"] = {
            name: round(statistics.median(profile[name] for profile in profiles), 2)
            for name in profiles[0]
        }
    return data, readings, recording


def make_device_dir(device, template):
    code, out = device.shell("mktemp -d " + shlex.quote(template))
    path = out.strip()
    if code or not re.fullmatch(re.escape(template[:-8]) + r"[A-Za-z0-9]{8}", path):
        raise RuntimeError("Could not allocate device staging directory")
    return path


def cleanup_staging(device, paths):
    problems = []
    for path in reversed(paths):
        if not re.fullmatch(r"(?:/run/np1-plate-bench-|/overlay/owner-apps/np1-control/\.plate-bench-)[A-Za-z0-9]{8}", path):
            problems.append("Unexpected staging path")
            continue
        code, _ = device.shell("rm -rf -- " + shlex.quote(path))
        if code:
            problems.append("Staging cleanup failed")
    return problems


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--serial", required=True)
    p.add_argument("--engine", type=Path, required=True)
    p.add_argument("--baseline", type=Path)
    p.add_argument("--baseline-models", type=Path, help="Use a separate model bundle for the baseline")
    p.add_argument("--runtime", type=Path, default=ROOT / "prebuilt/plate-engine/libonnxruntime.so.1.30.0")
    p.add_argument("--models", type=Path, default=ROOT / "models/plate")
    p.add_argument("--frames", type=Path, help="A sequence of raw 1920x1080 NV12 frames")
    p.add_argument("--compress-frames", action="store_true",
                   help="Stage a compressed frame fixture and stream it through the engine")
    p.add_argument("--runs", type=int, choices=range(1, 61), default=8, metavar="1..60")
    p.add_argument("--detector-backend", action="append", choices=("cpu", "gpu"))
    p.add_argument("--cache-detector", action="store_true", help="Compare GPU detection from its program cache")
    p.add_argument("--installed-cache", action="store_true", help="Compare only the installed detector cache")
    p.add_argument("--warm-installed-cache", action="store_true", help="Prepare extra model caches in a temporary copy")
    p.add_argument("--frames-only", action="store_true", help="Skip the synthetic benchmark")
    p.add_argument("--cpu-only", action="store_true", help="Run baseline and candidate with CPU backends")
    p.add_argument("--service-limits", action="store_true", help="Use the plate worker's CPU and memory limits")
    p.add_argument("--memory-mb", type=int, choices=range(128, 385), default=384, metavar="128..384",
                   help="Lower the test scope's memory ceiling while sensors are running")
    p.add_argument("--min-confidence", type=float, help="Use the same OCR threshold for every engine")
    p.add_argument("--reuse-installed-models", action="store_true", help="Link byte-identical installed models into staging")
    p.add_argument("--report", type=Path, required=True)
    args = p.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9._:-]+", args.serial):
        p.error("Invalid serial")
    frames, count = make_frames(args.frames)
    if args.frames_only and not count:
        p.error("--frames-only requires --frames")
    if args.compress_frames and not count:
        p.error("--compress-frames requires --frames")
    if (args.installed_cache or args.cpu_only) and (args.cache_detector or args.detector_backend):
        p.error("--installed-cache and --cpu-only select the detector backend")
    if args.warm_installed_cache and not args.installed_cache:
        p.error("--warm-installed-cache requires --installed-cache")
    if args.installed_cache and args.cpu_only:
        p.error("--installed-cache and --cpu-only cannot be combined")
    if args.baseline_models and not args.baseline:
        p.error("--baseline-models requires --baseline")
    if args.min_confidence is not None and not 0 <= args.min_confidence <= .99:
        p.error("--min-confidence must be between 0 and 0.99")
    local_files = [args.engine, args.runtime, *(args.models / f for f in model_files(args.models))]
    if args.baseline:
        local_files.append(args.baseline)
    if args.baseline_models:
        local_files.extend(args.baseline_models / f for f in model_files(args.baseline_models))
    if not all(f.is_file() for f in local_files):
        p.error("Engine, runtime or model file is missing")

    adb = ["adb", "-s", args.serial]
    device = Device(args.serial)
    before = stock_snapshot(device)
    if not before["frontExportUpdating"]:
        raise SystemExit("Stock front camera export is stale")
    if args.compress_frames and device.shell("command -v gzip")[0]:
        raise SystemExit("The device has no gzip decoder")
    report = {"cases": [], "stockProblems": [], "stagedPaths": [], "stockBefore": before}
    if args.compress_frames:
        report["compressedFrameInput"] = True
    reference = None
    reference_recording = None
    stage = report["stagedPaths"]
    try:
        models = make_device_dir(device, "/overlay/owner-apps/np1-control/.plate-bench-XXXXXXXX")
        stage.append(models)
        runtime = make_device_dir(device, "/run/np1-plate-bench-XXXXXXXX")
        stage.append(runtime)
        stage_models(device, args.models, models, reuse_installed=args.reuse_installed_models)
        baseline_models = models
        if args.baseline_models:
            baseline_models = models + "/baseline-models"
            _, made = device.shell("mkdir " + shlex.quote(baseline_models) + " && echo created")
            if made.strip() != "created":
                raise RuntimeError("Could not stage the baseline model directory")
            stage_models(device, args.baseline_models, baseline_models, shared=(args.models, models),
                         reuse_installed=args.reuse_installed_models)
        device.push(args.engine, runtime + "/candidate")
        device.push(args.runtime, runtime + "/libonnxruntime.so.1")
        packed_frames = None
        if count:
            args.report.parent.mkdir(parents=True, exist_ok=True)
            with tempfile.TemporaryDirectory(dir=args.report.parent) as local:
                packets = Path(local) / "frames.npf"
                if args.compress_frames:
                    packets = packets.with_suffix(".npf.gz")
                    with gzip.open(packets, "wb", compresslevel=4) as data:
                        data.write(frames)
                else:
                    packets.write_bytes(frames)
                packed_frames = models + "/" + packets.name
                device.push(packets, packed_frames)
        if args.baseline:
            device.push(args.baseline, runtime + "/baseline")
        code, _ = device.shell(f"chmod 0755 {runtime}/candidate" + (f" {runtime}/baseline" if args.baseline else ""))
        if code:
            raise RuntimeError("Could not prepare engine executables")
        cases = [("candidate-cpu", runtime + "/candidate", ["--backend", "cpu"])]
        if args.baseline:
            cases.insert(0, ("baseline", runtime + "/baseline", []))
        for backend in args.detector_backend or ["cpu"]:
            cases.append(("gpu-ocr/" + backend, runtime + "/candidate",
                          ["--detector-backend", backend]))
        if args.cache_detector:
            cache = models + "/opencl-cache"
            code, _ = device.shell("mkdir " + shlex.quote(cache))
            if code:
                raise RuntimeError("Could not create OpenCL test cache")
            info, _, _ = run_engine(device, adb, runtime + "/candidate", models, runtime,
                                    ["--prepare-gpu-cache", cache])
            report["cachePreparationMs"] = info["initMs"]
            print("Detector cache preparation passed", flush=True)
            cases.append(("gpu-ocr/cached-detector", runtime + "/candidate", ["--gpu-cache", cache]))
        if args.installed_cache:
            cache = models + "/opencl-cache"
            _, result = device.shell("cp -R /overlay/owner-apps/np1-control/plate/opencl-cache " +
                                     shlex.quote(cache) + " && echo copied")
            if result.strip() != "copied":
                raise RuntimeError("Could not copy installed detector cache")
            if args.warm_installed_cache:
                info, _, _ = run_engine(device, adb, runtime + "/candidate", models, runtime,
                                        ["--prepare-gpu-cache", cache],
                                        service_limits=args.service_limits, memory_mb=args.memory_mb)
                report["cachePreparationMs"] = info["initMs"]
                print("GPU cache preparation passed", flush=True)
            cases = [(name, path, ["--gpu-cache", cache])
                     for name, path in [("baseline", runtime + "/baseline"),
                                        ("candidate", runtime + "/candidate")]
                     if name != "baseline" or args.baseline]
        if args.cpu_only:
            cases = [(name, path, ["--backend", "cpu"])
                     for name, path in [("baseline", runtime + "/baseline"),
                                        ("candidate", runtime + "/candidate")]
                     if name != "baseline" or args.baseline]
        for name, path, options in cases:
            row = {"case": name}
            package = baseline_models if name == "baseline" else models
            if args.min_confidence is not None:
                options = [*options, "--min-confidence", str(args.min_confidence)]
            if not args.frames_only:
                info, rows, diagnostics = run_engine(device, adb, path, package, runtime,
                                                     [*options, "--bench", str(args.runs)],
                                                     service_limits=args.service_limits, memory_mb=args.memory_mb)
                if len(rows) != 1 or "bench" not in rows[0]:
                    raise RuntimeError("Engine returned no benchmark timings")
                row.update(rows[0]["bench"])
                row["backend"] = info["backend"]
                if "initMs" in info:
                    row["initMs"] = round(info["initMs"], 2)
                row["diagnostics"] = diagnostics.splitlines()[-35:]
            if count:
                info, rows, diagnostics = run_engine(device, adb, path, package, runtime,
                                                    [*options, "--profile", "1"], frames=packed_frames,
                                                    service_limits=args.service_limits, memory_mb=args.memory_mb)
                row["backend"] = info["backend"]
                row["diagnostics"] = diagnostics.splitlines()[-35:]
                if "initMs" in info:
                    row["initMs"] = round(info["initMs"], 2)
                try:
                    data, readings, recording = summarize_frames(rows, count)
                except RuntimeError:
                    row["diagnostics"] = diagnostics.splitlines()[-35:]
                    row["completedFrames"] = len(rows)
                    report["cases"].append(row)
                    raise
                if reference is None:
                    reference = readings
                    reference_recording = recording
                data["equalFrames"] = sum(a == b for a, b in zip(reference, readings))
                data["equalDefaultRecordingFrames"] = sum(a == b for a, b in zip(reference_recording, recording))
                if peak := re.search(r"^peak-rss-kb (\d+)\r?$", diagnostics, re.MULTILINE):
                    data["peakRssKiB"] = int(peak[1])
                row["recognition"] = data
            report["cases"].append(row)
            print(json.dumps({k: v for k, v in row.items() if k != "diagnostics"}), flush=True)
    finally:
        report["stockProblems"].extend(cleanup_staging(device, stage))
        time.sleep(2)
        report["stockAfter"] = stock_snapshot(device)
        report["stockProblems"].extend(compare_stock(before, report["stockAfter"]))
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if report["stockProblems"]:
        raise SystemExit("Stock health comparison failed")
    print("Stock health check passed")


if __name__ == "__main__":
    main()
