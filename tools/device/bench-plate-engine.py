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
from bench_inference import Device, STOCK_UNITS, compare_stock, stock_snapshot

INSTALLED_MODELS = "/overlay/owner-apps/np1-control/plate/models"
OWNER_PLATE = "np1-control-plate.service"
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
    names = {"detector.tflite", "ocr.tflite", "plate-model.json"}
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


def service_scope(enabled, memory_mb, cpu_quota):
    if enabled:
        return f"systemd-run --scope --quiet -p CPUQuota={cpu_quota}% -p MemoryLimit={memory_mb}M "
    return ""


def run_engine(device, adb, path, models, runtime, options, *, frames=None, service_limits=False,
               memory_mb=384, cpu_quota=150):
    args = [
        path, "--detector", models + "/detector.tflite", "--ocr", models + "/ocr.tflite",
        "--config", models + "/plate-model.json", *options,
    ]
    log = runtime + "/engine.log"
    scope = service_scope(service_limits, memory_mb, cpu_quota)
    timer_ok, _ = device.shell("test -x /usr/bin/time && /usr/bin/time -f '' /bin/true 2>/dev/null")
    timer = "/usr/bin/time -f 'peak-rss-kb %M' " if not timer_ok else ""
    compressed = frames is not None and frames.endswith(".gz")
    command = (f"LD_LIBRARY_PATH={shlex.quote(runtime)} exec {scope}nice -n 10 {timer}"
               f"{shlex.join(args)} 2> {shlex.quote(log)}")
    if compressed:
        command = f"nice -n 10 gzip -dc {shlex.quote(frames)} | {command}"
    else:
        command += f" < {shlex.quote(frames or '/dev/null')}"
    try:
        result = subprocess.run([*adb, "exec-out", command], capture_output=True, timeout=180)
    except subprocess.TimeoutExpired:
        raise RuntimeError("Engine test timed out") from None
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


def check_fixture_headroom(device, memory_mb, fixture_bytes=0, *, wait_seconds=0):
    fixture_mb = (fixture_bytes + (1 << 20) - 1) >> 20
    reserved_mb = memory_mb + 352 + (fixture_mb + 35 if fixture_bytes else 0)
    deadline = time.monotonic() + wait_seconds
    while True:
        status, output = device.shell("cat /proc/meminfo; df -m /run")
        available = re.search(r"^MemAvailable:\s+(\d+)", output, re.MULTILINE)
        run_free = re.search(r"\d+\s+\d+\s+(\d+)\s+\d+%\s+/run\s*$", output, re.MULTILINE)
        if status or available is None or run_free is None:
            raise RuntimeError("Could not measure NP1 RAM and runtime filesystem headroom")
        available_mb = int(available[1]) // 1024
        if available_mb >= reserved_mb and int(run_free[1]) >= fixture_mb + 75:
            return available_mb
        if time.monotonic() >= deadline:
            raise RuntimeError(f"Insufficient NP1 test headroom: {available_mb}/{reserved_mb} MiB RAM, "
                               f"{run_free[1]}/{fixture_mb + 75} MiB on the runtime filesystem")
        time.sleep(1)


def stage_frame_fixture(device, frames, parent, report, *, compressed=False, storage_format=None):
    report.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=report.parent) as local:
        if storage_format == "indexed-gzip":
            packets = Path(local) / "frames.npg"
            packet_size = FRAME_BYTES + 24
            members = [gzip.compress(frames[i:i + packet_size], compresslevel=4, mtime=0)
                       for i in range(0, len(frames), packet_size)]
            with packets.open("wb") as data:
                data.write(struct.pack("<4sI", b"NPG1", len(members)))
                for member in members:
                    data.write(struct.pack("<I", len(member)))
                data.writelines(members)
        elif compressed:
            packets = Path(local) / "frames.npf.gz"
            with gzip.open(packets, "wb", compresslevel=4) as data:
                data.write(frames)
        else:
            packets = Path(local) / "frames.npf"
            packets.write_bytes(frames)
        if storage_format:
            code, output = device.shell("df -m /overlay")
            available = re.search(r"\d+\s+\d+\s+(\d+)\s+\d+%\s+/overlay\s*$", output, re.MULTILINE)
            size_mib = (packets.stat().st_size + (1 << 20) - 1) >> 20
            # Cache preparation has already consumed its actual disk usage.
            if code or not available or int(available[1]) < size_mib + 32:
                space = available[1] if available else "unknown"
                raise RuntimeError(f"Owner overlay has {space} MiB after cache preparation; need {size_mib + 32} MiB")
        path = parent + "/" + packets.name
        device.push(packets, path)
    return path


def arm_owner_plate_restore(device, timer):
    code, state = device.shell(f"systemctl is-active {OWNER_PLATE}")
    if code or state.strip() != "active":
        raise RuntimeError("The installed plate sensor must be active before an isolated measurement")
    code, _ = device.shell(f"systemd-run --quiet --unit={shlex.quote(timer)} --on-active=6min "
                           f"/bin/systemctl start {OWNER_PLATE}")
    if code:
        raise RuntimeError("Could not schedule restoration of the installed plate sensor")
    return timer


def restore_owner_plate(device, timer):
    command = (f"systemctl start {OWNER_PLATE} && "
               f"systemctl is-active --quiet {OWNER_PLATE} && "
               "for i in 1 2 3 4 5 6 7 8 9 10; do "
               "test ! -S /run/np1-control-plate/sensor.sock || break; sleep 1; done; "
               f"test -S /run/np1-control-plate/sensor.sock && systemctl stop {shlex.quote(timer + '.timer')}")
    try:
        return device.shell(command, timeout=25)[0] == 0
    except (OSError, subprocess.SubprocessError):
        return False


def run_sensor_test(adb, device, runtime, models, cache, test, extra_env, *, cpu_only, service_limits,
                    memory_mb, cpu_quota):
    sensor = runtime + "/sensor-test"
    if device.shell(f"chmod 0700 {shlex.quote(sensor)}")[0]:
        raise RuntimeError("Could not prepare the sensor test")
    env = {
        "LD_LIBRARY_PATH": runtime, "TMPDIR": runtime,
        "NCC_PLATE_TEST_ENGINE": runtime + "/candidate", "NCC_PLATE_TEST_MODELS": models,
        "NCC_PLATE_TEST_CACHE": cache, **extra_env,
    }
    if cpu_only:
        env["NCC_PLATE_BACKEND"] = "cpu"
    settings = " ".join(f"{key}={shlex.quote(value)}" for key, value in env.items())
    scope = service_scope(service_limits, memory_mb, cpu_quota)
    command = (f"{settings} exec {scope}nice -n 10 {shlex.quote(sensor)} "
               f"-test.run {shlex.quote('^' + test + '$')} -test.v -test.timeout 150s")
    try:
        return subprocess.run([*adb, "exec-out", command], capture_output=True, timeout=165)
    except subprocess.TimeoutExpired:
        raise RuntimeError("Sensor test timed out") from None


def check_live_sensor(adb, device, runtime, models, cache, seconds, *, cpu_only, memory_mb, cpu_quota):
    completed = run_sensor_test(adb, device, runtime, models, cache, "TestDevicePlateLive",
                                {"NCC_PLATE_TEST_LIVE_SECONDS": str(seconds)}, cpu_only=cpu_only,
                                service_limits=True, memory_mb=memory_mb, cpu_quota=cpu_quota)
    metrics = re.search(rb"live camera frames: (\d+); fps: ([\d.]+); max gap ms: (\d+)", completed.stdout)
    if completed.returncode or not metrics or b"--- PASS: TestDevicePlateLive" not in completed.stdout:
        stage = "after engine startup" if b"live camera engine ready" in completed.stdout else "before engine startup"
        raise RuntimeError(f"Live camera sensor test failed {stage}")
    result = {"durationSeconds": seconds, "framesProcessed": int(metrics[1]),
              "fps": float(metrics[2]), "maxGapMs": int(metrics[3])}
    if transport := re.search(rb"shared frames: (true|false)", completed.stdout):
        result["sharedFrames"] = transport[1] == b"true"
    if rates := re.search(rb"live camera seconds below 6 fps: (\d+); lowest five-second fps: ([\d.]+)",
                          completed.stdout):
        result["secondsBelow6Fps"] = int(rates[1])
        result["lowestFiveSecondFps"] = float(rates[2])
    return result


def check_sensor_pipeline(adb, device, runtime, models, frames, cache, expected, *,
                          cpu_only, service_limits, memory_mb, cpu_quota=150, paced=False, repeat=1):
    expected_file = runtime + "/expected.json"
    env = {"NCC_PLATE_TEST_FRAMES": frames, "NCC_PLATE_TEST_EXPECTED": expected_file}
    if paced:
        check_fixture_headroom(device, memory_mb, wait_seconds=15)
        env["NCC_PLATE_TEST_PACED"] = "1"
        if repeat > 1:
            env["NCC_PLATE_TEST_PACED_REPEAT"] = str(repeat)
    completed = run_sensor_test(adb, device, runtime, models, cache, "TestDevicePlatePipeline", env,
                                cpu_only=cpu_only, service_limits=service_limits, memory_mb=memory_mb, cpu_quota=cpu_quota)
    if paced:
        planned_frames = expected["frames"] * repeat
        counts = re.search(rb"paced camera offered (\d+) frames; processed (\d+) frames", completed.stdout)
        records = re.search(rb"paced expected record count: (\d+)", completed.stdout)
        if (completed.returncode or not counts or not records or b"--- PASS: TestDevicePlatePipeline" not in completed.stdout
                or int(counts[1]) != planned_frames or not 0 < int(counts[2]) <= planned_frames):
            messages = (
                rb"invalid device replay reference",
                rb"invalid paced replay repetitions",
                rb"paced replay requires a complete fixture",
                rb"the configured GPU detector was unavailable",
                rb"engine ready for the combined check",
                rb"paced acquisition stopped before the reference duration",
                rb"paced replay had a failed inference",
                rb"paced camera offered \d+ frames; processed \d+ frames",
                rb"expected vehicle was missing its tracked interval",
                rb"(?:compressed|replay) source missed the planned input cadence: offered \d+; max delay ms: \d+",
            )
            details = [m[0].decode() for pattern in messages if (m := re.search(pattern, completed.stdout))]
            suffix = ", ".join(details) or "no sensor test progress received"
            raise RuntimeError(f"Paced sensor replay did not finish correctly (exit {completed.returncode}): {suffix}")
        result = {"offeredAtFPS": 8, "framesOffered": int(counts[1]), "framesProcessed": int(counts[2]),
                  "expectedRecords": int(records[1]), "trackedRecordVerified": repeat == 1 and int(records[1]) == 1,
                  "durationSeconds": planned_frames / 8, "fixtureRepetitions": repeat}
        result["effectiveFps"] = round(8 * result["framesProcessed"] / result["framesOffered"], 3)
        if transport := re.search(rb"shared frames: (true|false)", completed.stdout):
            result["sharedFrames"] = transport[1] == b"true"
        if processing := re.search(rb"paced process mean us: (\d+); engine mean ms: ([\d.]+)", completed.stdout):
            result["processMeanMs"] = int(processing[1]) / 1000
            result["engineMeanMs"] = float(processing[2])
        if gap := re.search(rb"paced max acquisition gap us: (\d+)", completed.stdout):
            result["maxAcquisitionGapMs"] = int(gap[1]) / 1000
        if reading := re.search(rb"paced frame read mean us: (\d+); max us: (\d+)", completed.stdout):
            result["fixtureReadMeanMs"] = int(reading[1]) / 1000
            result["fixtureReadMaxMs"] = int(reading[2]) / 1000
        elif frames.endswith(".npg"):
            raise RuntimeError("The compressed fixture test did not report its read time")
        if delay := re.search(rb"max offer delay us: (\d+)", completed.stdout):
            result["fixtureMaxOfferDelayMs"] = int(delay[1]) / 1000
        if indexes := re.search(rb"paced input frame indexes: \[([\d ]+)\]", completed.stdout):
            source_frames = [int(frame) for frame in indexes[1].split()]
            if (len(source_frames) != result["framesProcessed"]
                    or not 1 <= source_frames[0] <= source_frames[-1] <= planned_frames
                    or source_frames != sorted(set(source_frames))):
                raise RuntimeError("The sensor replay returned invalid frame indexes")
            result["sourceFrameIndexes"] = source_frames
        return result
    success = f"combined OCR and sensor read-back passed on {expected['frames']} frames".encode()
    if completed.returncode or success not in completed.stdout or b"--- PASS: TestDevicePlatePipeline" not in completed.stdout:
        messages = (
            rb"the configured GPU detector was unavailable",
            rb"engine ready for the combined check",
            rb"recognition processed \d+ frames",
            rb"expected vehicle was missing its tracked interval",
            rb"expected one matching record, got \d+ of \d+ records",
        )
        details = [m[0].decode() for pattern in messages if (m := re.search(pattern, completed.stdout))]
        suffix = ": " + ", ".join(details) if details else " before engine read-back"
        raise RuntimeError("Combined recognition and storage check failed" + suffix)
    return {"frames": expected["frames"], "trackedRecordVerified": True}


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
        try:
            code, _ = device.shell("rm -rf -- " + shlex.quote(path))
        except (OSError, subprocess.SubprocessError):
            problems.append("Staging cleanup could not contact the device")
            continue
        if code:
            problems.append("Staging cleanup failed")
    return problems


def main_go_evidence(argv):
    """Check tracking and persistent readback on ARM64 using two captured OCR replies."""
    parser = argparse.ArgumentParser(description=main_go_evidence.__doc__)
    parser.add_argument("--serial", required=True)
    parser.add_argument("--evidence-dir", required=True, type=Path,
                        help="Directory with sensor-test, images.nv12, and fixture.private.json")
    parser.add_argument("--report", required=True, type=Path)
    args = parser.parse_args(argv)
    files = {name: args.evidence_dir / name for name in ("sensor-test", "images.nv12", "fixture.private.json")}
    if not all(path.is_file() for path in files.values()):
        parser.error("The evidence directory is incomplete")
    with files["sensor-test"].open("rb") as program:
        header = program.read(20)
    if header[:6] != b"\x7fELF\x02\x01" or header[18:20] != b"\xb7\x00":
        parser.error("The sensor test must be a Linux ARM64 executable")
    if files["images.nv12"].stat().st_size != 2 * FRAME_BYTES:
        parser.error("Go evidence replay requires exactly two 1920x1080 NV12 frames")
    staged_bytes = sum(path.stat().st_size for path in files.values())
    if staged_bytes > 24 * (1 << 20):
        parser.error("Go evidence replay staging is limited to 24 MiB")

    memory_mb = 64
    # There is no detector or OCR process in this test scope. Include the
    # staged files and leave memory for the stock recorder and camera.
    reserve_mb = memory_mb + 160 + math.ceil(staged_bytes / (1 << 20))
    device = Device(args.serial)
    code, memory = device.shell("cat /proc/meminfo; df -m /run")
    available = re.search(r"^MemAvailable:\s+(\d+)", memory, re.MULTILINE)
    run_free = re.search(r"\d+\s+\d+\s+(\d+)\s+\d+%\s+/run\s*$", memory, re.MULTILINE)
    if code or not available or not run_free or int(available[1]) // 1024 < reserve_mb or int(run_free[1]) < reserve_mb:
        raise SystemExit("Insufficient headroom for the bounded Go evidence replay")
    before = stock_snapshot(device)
    if not before["frontExportUpdating"] or not before["latestRecording"] or any(
            before[unit].get("ActiveState") != "active" for unit in STOCK_UNITS):
        raise SystemExit("Stock camera or recording is not ready for the evidence replay")
    owner_status = f"systemctl show -p ActiveState -p MainPID -p NRestarts {shlex.quote(OWNER_PLATE)}"
    code, owner_before = device.shell(owner_status)
    if code or "ActiveState=active" not in owner_before.splitlines():
        raise SystemExit("Installed plate sensor health could not be verified before replay")
    runtime = make_device_dir(device, "/run/np1-plate-bench-XXXXXXXX")
    timer = Path(runtime).name + "-cleanup"
    report = {"phase": "staging", "memoryLimitMiB": memory_mb, "cpuQuotaPercent": 50,
              "availableMiB": int(available[1]) // 1024, "verifiedFiles": 0, "stockProblems": []}
    try:
        if device.shell(f"systemd-run --quiet --unit={shlex.quote(timer)} --on-active=3min "
                        f"/bin/rm -rf -- {shlex.quote(runtime)}")[0]:
            raise RuntimeError("Could not arm cleanup of the Go evidence fixtures")
        for name, local in files.items():
            remote = runtime + "/" + name
            with local.open("rb") as stream:
                expected = hashlib.file_digest(stream, "sha256").hexdigest()
            device.push(local, remote)
            code, actual = device.shell("sha256sum " + shlex.quote(remote))
            if code or not actual.split() or actual.split()[0] != expected:
                raise RuntimeError("Staged Go evidence failed checksum readback")
            report["verifiedFiles"] += 1
        report["phase"] = "replay"
        run = run_sensor_test(["adb", "-s", args.serial], device, runtime, runtime, "", "TestDevicePlateGoEvidenceReplay",
                              {"NCC_DEVICE_EVIDENCE_WORK": runtime, "GOMAXPROCS": "1"}, cpu_only=True,
                              service_limits=True, memory_mb=memory_mb, cpu_quota=50)
        proof = b"Native Go evidence replay: checked 2 real frames, 1 stored serial with both timestamps, 1 full-frame JPEG"
        if run.returncode or proof not in run.stdout or b"--- PASS: TestDevicePlateGoEvidenceReplay" not in run.stdout:
            raise RuntimeError("Native Go evidence replay failed")
        report["phase"] = "checked"
    finally:
        report["stockProblems"].extend(cleanup_staging(device, [runtime]))
        report["stagingRemoved"] = False
        try:
            code, out = device.shell(f"test ! -e {shlex.quote(runtime)} && echo removed")
            report["stagingRemoved"] = code == 0 and out.strip() == "removed"
            if report["stagingRemoved"]:
                device.shell("systemctl stop " + shlex.quote(timer + ".timer"))
            report["stockProblems"].extend(compare_stock(before, stock_snapshot(device)))
            code, owner_after = device.shell(owner_status)
            report["ownerPlateUnchanged"] = code == 0 and owner_after == owner_before
            if not report["ownerPlateUnchanged"]:
                report["stockProblems"].append("Installed plate sensor changed during replay")
        except (OSError, subprocess.SubprocessError):
            report["stockProblems"].append("Stock health or cleanup readback failed")
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if not report["stagingRemoved"] or report["stockProblems"]:
        raise SystemExit("Staging cleanup or stock health verification failed")
    print("Native ARM64 Go tracking, JPEG and storage readback passed")


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
    p.add_argument("--frames-in-ram", action="store_true",
                   help="Stage the frame fixture in /run under the service memory limits")
    p.add_argument("--isolate-owner-plate", action="store_true",
                   help="Pause the installed plate sensor before cache preparation and restore it after the measurement")
    p.add_argument("--runs", type=int, choices=range(1, 61), default=8, metavar="1..60")
    p.add_argument("--detector-backend", action="append", choices=("cpu", "gpu"))
    p.add_argument("--cache-detector", action="store_true", help="Compare GPU detection from its program cache")
    p.add_argument("--installed-cache", action="store_true", help="Compare only the installed detector cache")
    p.add_argument("--warm-installed-cache", action="store_true", help="Prepare extra model caches in a temporary copy")
    p.add_argument("--frames-only", action="store_true", help="Skip the synthetic benchmark")
    p.add_argument("--cpu-only", action="store_true", help="Run baseline and candidate with CPU backends")
    p.add_argument("--service-limits", action="store_true", help="Use the plate worker's CPU and memory limits")
    p.add_argument("--cpu-quota", type=int, choices=range(100, 201), default=150, metavar="100..200",
                   help="CPU quota percent for an isolated test scope; normal worker uses 150")
    p.add_argument("--memory-mb", type=int, choices=range(128, 385), default=384, metavar="128..384",
                   help="Lower the test scope's memory ceiling while sensors are running")
    p.add_argument("--min-confidence", type=float, help="Use the same OCR threshold for every engine")
    p.add_argument("--reuse-installed-models", action="store_true", help="Link byte-identical installed models into staging")
    p.add_argument("--report", type=Path, required=True)
    p.add_argument("--sensor-test", type=Path, help="Run the Linux/arm64 sensor test in place of engine-only timing")
    p.add_argument("--sensor-expected", type=Path, help="Private JSON reference for the sensor test")
    p.add_argument("--sensor-paced", action="store_true",
                   help="Measure 8 fps sensor acquisition and storage using a RAM-backed frame fixture")
    p.add_argument("--sensor-paced-storage", nargs="?", const="indexed-gzip", choices=("indexed-gzip", "raw"),
                   help="Stage paced frames on the owner overlay after cache preparation")
    p.add_argument("--sensor-paced-repeat", type=int, default=1,
                   help="Reuse the fixture for a sustained throughput test, up to 60 seconds at 8 fps")
    p.add_argument("--sensor-live-seconds", type=int, choices=range(10, 91), metavar="10..90",
                   help="Run the staged sensor on the stock live export for this many seconds")
    args = p.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9._:-]+", args.serial):
        p.error("Invalid serial")
    frames, count = make_frames(args.frames)
    if args.frames_only and not count:
        p.error("--frames-only requires --frames")
    if args.compress_frames and not count:
        p.error("--compress-frames requires --frames")
    if args.frames_in_ram and (not count or not args.service_limits):
        p.error("--frames-in-ram requires --frames and --service-limits")
    if args.isolate_owner_plate and not args.service_limits:
        p.error("--isolate-owner-plate requires --service-limits")
    if args.cpu_quota != 150 and not args.isolate_owner_plate:
        p.error("Changing the test CPU quota requires --isolate-owner-plate")
    sensor_expected = None
    if args.sensor_live_seconds:
        if (count or not args.sensor_test or not args.sensor_test.is_file() or args.sensor_expected or args.baseline
                or not args.isolate_owner_plate or not (args.installed_cache or args.cpu_only)):
            p.error("Live camera testing requires an isolated candidate sensor with either CPU or the installed GPU cache")
    elif args.sensor_test or args.sensor_expected:
        if not count or not args.sensor_test or not args.sensor_expected or args.baseline or not (args.installed_cache or args.cpu_only):
            p.error("The sensor test requires a candidate-only run with frames and either CPU or the installed GPU cache")
        if not args.sensor_test.is_file() or not args.sensor_expected.is_file():
            p.error("The sensor test executable or reference is missing")
        try:
            sensor_expected = json.loads(args.sensor_expected.read_text(encoding="utf-8"))
            if sensor_expected["frames"] != count or not re.fullmatch(r"[1-9][0-9]{0,3}", sensor_expected["number"]) or not (
                2 <= sensor_expected["minSightings"] <= count
            ):
                raise ValueError
        except (ValueError, KeyError, TypeError):
            p.error("The sensor reference must match the frame count and contain a serial and minimum sightings")
    if args.sensor_paced and (sensor_expected is None or args.compress_frames or not args.service_limits):
        p.error("--sensor-paced requires a sensor test, an uncompressed fixture, and --service-limits")
    if args.sensor_paced_storage and (not args.sensor_paced or not args.isolate_owner_plate or args.frames_in_ram):
        p.error("--sensor-paced-storage requires an isolated --sensor-paced test without --frames-in-ram")
    if args.sensor_paced_repeat != 1 and (not args.sensor_paced or args.sensor_paced_repeat < 2
                                          or count * args.sensor_paced_repeat > 480):
        p.error("--sensor-paced-repeat requires --sensor-paced and at most 60 seconds of input")
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
    frames_in_ram = (args.sensor_paced and not args.sensor_paced_storage) or args.frames_in_ram
    if frames_in_ram:
        # No candidate runs during upload. An isolated run reserves the full
        # inference budget after the installed worker has released its memory.
        staging_budget = 0 if args.isolate_owner_plate else args.memory_mb
        check_fixture_headroom(device, staging_budget, len(frames))
    if args.compress_frames and device.shell("command -v gzip")[0]:
        raise SystemExit("The device has no gzip decoder")
    report = {"cases": [], "stockProblems": [], "stagedPaths": [], "stockBefore": before}
    if args.service_limits:
        report["scopeLimits"] = {"cpuQuotaPercent": args.cpu_quota, "memoryMiB": args.memory_mb}
    if args.compress_frames:
        report["compressedFrameInput"] = True
    if args.sensor_paced:
        report["pacedFixtureStorage"] = f"owner-overlay-{args.sensor_paced_storage}" if args.sensor_paced_storage else "tmpfs"
    reference = None
    reference_recording = None
    cache = ""
    stage = report["stagedPaths"]
    restore_timer = ""
    limits = {"service_limits": args.service_limits, "memory_mb": args.memory_mb, "cpu_quota": args.cpu_quota}
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
        if count and not args.sensor_paced_storage:
            packed_frames = stage_frame_fixture(device, frames, runtime if frames_in_ram else models,
                                                args.report, compressed=args.compress_frames)
        if args.baseline:
            device.push(args.baseline, runtime + "/baseline")
        code, _ = device.shell(f"chmod 0755 {runtime}/candidate" + (f" {runtime}/baseline" if args.baseline else ""))
        if code:
            raise RuntimeError("Could not prepare engine executables")
        if args.sensor_test is not None:
            device.push(args.sensor_test, runtime + "/sensor-test")
        if sensor_expected is not None:
            device.push(args.sensor_expected, runtime + "/expected.json")
            code, _ = device.shell(f"chmod 0600 {shlex.quote(runtime + '/expected.json')}")
            if code:
                raise RuntimeError(f"Could not protect the staged sensor reference (exit {code})")
        if args.isolate_owner_plate:
            restore_timer = arm_owner_plate_restore(device, Path(runtime).name + "-restore")
            code, _ = device.shell(f"systemctl stop {OWNER_PLATE} && "
                                   f"test \"$(systemctl show -p ActiveState --value {OWNER_PLATE})\" = inactive")
            if code:
                raise RuntimeError("Could not stop the installed plate sensor for the isolated measurement")
            report["ownerPlateIsolated"] = True
            # Cache preparation loads the new models just as a measurement does.
            # Check headroom with the fixture staged and the installed worker stopped.
            report["availableMiBAfterIsolation"] = check_fixture_headroom(device, args.memory_mb, wait_seconds=15)
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
                                    ["--prepare-gpu-cache", cache], **limits)
            report["cachePreparationMs"] = info["initMs"]
            print("Detector cache preparation passed", flush=True)
            cases.append(("gpu-ocr/cached-detector", runtime + "/candidate", ["--gpu-cache", cache]))
        if args.installed_cache:
            cache = models + "/opencl-cache"
            status, result = device.shell("cp -R /overlay/owner-apps/np1-control/plate/opencl-cache " +
                                          shlex.quote(cache) + " 2>&1 && echo copied")
            if status or result.strip().splitlines()[-1:] != ["copied"]:
                raise RuntimeError("Could not copy installed detector cache: " + result.strip()[-400:])
            if args.warm_installed_cache:
                info, _, _ = run_engine(device, adb, runtime + "/candidate", models, runtime,
                                        ["--prepare-gpu-cache", cache], **limits)
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
        if args.sensor_test is not None:
            cases = []
        if args.sensor_paced_storage:
            packed_frames = stage_frame_fixture(device, frames, models, args.report,
                                                storage_format=args.sensor_paced_storage)
        for name, path, options in cases:
            row = {"case": name}
            package = baseline_models if name == "baseline" else models
            if args.min_confidence is not None:
                options = [*options, "--min-confidence", str(args.min_confidence)]
            if not args.frames_only:
                info, rows, diagnostics = run_engine(device, adb, path, package, runtime,
                                                     [*options, "--bench", str(args.runs)], **limits)
                if len(rows) != 1 or "bench" not in rows[0]:
                    raise RuntimeError("Engine returned no benchmark timings")
                row.update(rows[0]["bench"])
                row["backend"] = info["backend"]
                if "initMs" in info:
                    row["initMs"] = round(info["initMs"], 2)
                row["diagnostics"] = diagnostics.splitlines()[-35:]
            if count:
                info, rows, diagnostics = run_engine(device, adb, path, package, runtime,
                                                    [*options, "--profile", "1"], frames=packed_frames, **limits)
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
        if args.sensor_live_seconds:
            report["live"] = check_live_sensor(adb, device, runtime, models, cache, args.sensor_live_seconds,
                                              cpu_only=args.cpu_only, memory_mb=args.memory_mb, cpu_quota=args.cpu_quota)
            print(json.dumps(report["live"]), flush=True)
        elif sensor_expected is not None:
            report["pipeline"] = {"frames": sensor_expected["frames"], "trackedRecordVerified": False}
            report["pipeline"] = check_sensor_pipeline(adb, device, runtime, models, packed_frames, cache, sensor_expected,
                                                       cpu_only=args.cpu_only, paced=args.sensor_paced,
                                                       repeat=args.sensor_paced_repeat, **limits)
            if args.sensor_paced:
                print(json.dumps(report["pipeline"]), flush=True)
            else:
                print("Combined recognition and storage check passed", flush=True)
        if restore_timer:
            _, state = device.shell(f"systemctl show -p ActiveState --value {OWNER_PLATE}")
            if state.strip() != "inactive":
                report["stockProblems"].append("installed plate sensor resumed before measurement completed")
    finally:
        if restore_timer:
            report["ownerPlateRestored"] = restore_owner_plate(device, restore_timer)
            if not report["ownerPlateRestored"]:
                report["stockProblems"].append("installed plate sensor restoration could not be verified")
        report["stockProblems"].extend(cleanup_staging(device, stage))
        time.sleep(2)
        try:
            report["stockAfter"] = stock_snapshot(device)
            report["stockProblems"].extend(compare_stock(before, report["stockAfter"]))
        except (OSError, subprocess.SubprocessError):
            report["stockProblems"].append("Stock health could not be verified after measurement")
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if report["stockProblems"]:
        raise SystemExit("Stock health comparison failed")
    print("Stock health check passed")


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "go-evidence":
        main_go_evidence(sys.argv[2:])
    else:
        main()
