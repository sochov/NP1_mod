"""Capture stock ImageRecog NV12 exports for plate-recognition research.

Read-only: only `cat`s the fixed export file over ADB. Never opens camera
devices or touches stock services. Frames are private and stay in ignored tmp/.
"""
import argparse
import datetime
import pathlib
import subprocess
import time

import cv2
import numpy as np

ROOT = pathlib.Path(__file__).resolve().parents[2]
WIDTH, HEIGHT = 1920, 1080
FRAME_BYTES = WIDTH * HEIGHT * 3 // 2
SOURCES = {'front': 'frontcamera', 'in': 'incamera'}


def export_path(source):
    return f'/tmp/application/imagerecog/imagedata/{SOURCES[source]}/imagedata/value'


def nv12_to_bgr(data):
    if len(data) < FRAME_BYTES:
        raise ValueError(f'incomplete frame: {len(data)} < {FRAME_BYTES}')
    yuv = np.frombuffer(data[:FRAME_BYTES], dtype=np.uint8).reshape(HEIGHT * 3 // 2, WIDTH)
    return cv2.cvtColor(yuv, cv2.COLOR_YUV2BGR_NV12)


def read_export(serial, source):
    result = subprocess.run(['adb', '-s', serial, 'exec-out', 'cat', export_path(source)],
                            capture_output=True, timeout=30)
    if result.returncode:
        raise RuntimeError(result.stderr.decode(errors='replace').strip() or 'adb failed')
    return result.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    parser.add_argument('--source', choices=sorted(SOURCES), default='front')
    parser.add_argument('--count', type=int, default=1)
    parser.add_argument('--interval', type=float, default=0.5, help='seconds between captures')
    parser.add_argument('--out', type=pathlib.Path, default=ROOT / 'tmp' / 'plate' / 'frames')
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    for index in range(args.count):
        data = read_export(args.serial, args.source)
        stamp = datetime.datetime.now().strftime('%Y%m%d-%H%M%S-%f')
        image = nv12_to_bgr(data)
        target = args.out / f'{args.source}-{stamp}.png'
        cv2.imwrite(str(target), image)
        print(f'{index + 1}/{args.count} saved {target.relative_to(ROOT)}')
        if index + 1 < args.count:
            time.sleep(args.interval)


if __name__ == '__main__':
    main()
