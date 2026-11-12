"""Play an announcement WAV on the NP1 speaker the way the server does.

    python tools/device/plate-speech-play.py --serial SERIAL --wav announcement.wav [--volume 70]

The WAV (24 kHz mono 16-bit, e.g. from GET /api/v1/plate/speech/rules/{id}/preview)
is copied to /tmp on the device, played with /usr/bin/pacat into the stock
PulseAudio "primary" sink as the ncc user (same arguments as
platespeech.Pacat), and deleted. No device setting, sink volume or stock file
changes; the stock services' PID/NRestarts are compared before and after.
"""
from __future__ import annotations

import argparse
import struct
import subprocess
import sys
from pathlib import Path

STOCK = ["app-@Communication", "app-@DriveRec", "app-@ImageRecog", "app-@Voice", "qmmf-server", "pulseaudio"]
REMOTE = "/tmp/ncc-plate-speech-play.pcm"


def adb(serial: str, *args: str, check=True) -> str:
    r = subprocess.run(["adb", "-s", serial, *args], capture_output=True)
    if check and r.returncode:
        raise SystemExit(r.stderr.decode("utf-8", "replace"))
    return r.stdout.decode("utf-8", "replace")


def services(serial: str) -> dict:
    out = {}
    for u in STOCK:
        props = adb(serial, "shell", f"systemctl show '{u}.service' -p ActiveState -p MainPID -p NRestarts", check=False)
        out[u] = dict(x.split("=", 1) for x in props.split() if "=" in x)
    return out


def pcm_of(path: Path) -> bytes:
    b = path.read_bytes()
    if b[:4] != b"RIFF" or b[8:12] != b"WAVE":
        raise SystemExit("not a WAV")
    off = 12
    fmt_ok = False
    while off + 8 <= len(b):
        cid, size = b[off:off + 4], struct.unpack("<I", b[off + 4:off + 8])[0]
        body = b[off + 8:off + 8 + size]
        if cid == b"fmt ":
            fmt, ch, rate, _, _, bits = struct.unpack("<HHIIHH", body[:16])
            fmt_ok = (fmt, ch, rate, bits) == (1, 1, 24000, 16)
        elif cid == b"data":
            if not fmt_ok:
                raise SystemExit("WAV must be 24 kHz mono 16-bit")
            return body
        off += 8 + size + (size & 1)
    raise SystemExit("no data chunk")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--serial", required=True)
    ap.add_argument("--wav", type=Path, required=True)
    ap.add_argument("--volume", type=int, default=70)
    a = ap.parse_args()
    if not 10 <= a.volume <= 100:
        raise SystemExit("volume 10-100")
    pcm = pcm_of(a.wav)
    tmp = a.wav.with_suffix(".pcm")
    tmp.write_bytes(pcm)
    before = services(a.serial)
    adb(a.serial, "push", str(tmp), REMOTE)
    tmp.unlink()
    vol = a.volume * 65536 // 100
    cmd = (f"chmod 0644 {REMOTE}; su -s /bin/sh ncc -c '/usr/bin/pacat --playback --raw --server=unix:/run/pulse/native "
           f"--device=primary --client-name=NP1-Plate-Speech --stream-name=Plate-announcement --format=s16le --rate=24000 "
           f"--channels=1 --latency-msec=150 --volume={vol} < {REMOTE}'; echo rc=$?; rm -f {REMOTE}")
    print(adb(a.serial, "shell", cmd).strip(), f"({len(pcm) / 48000:.2f}s)")
    after = services(a.serial)
    changed = [u for u in STOCK if before[u].get("MainPID") != after[u].get("MainPID") or before[u].get("NRestarts") != after[u].get("NRestarts") or after[u].get("ActiveState") != "active"]
    print("stock services", "changed: " + ", ".join(changed) if changed else "unchanged")
    if changed:
        sys.exit(1)


if __name__ == "__main__":
    main()
