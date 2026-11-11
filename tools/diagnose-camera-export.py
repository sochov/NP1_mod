"""Read-only temporal comparison of the two fixed stock NV12 exports.

Never opens camera devices, starts producers, or changes stock files/services.
All frame bytes, hashes and device metadata remain in ignored tmp evidence.
Only comparison booleans and capture times are printed.
"""
import argparse
import hashlib
import json
import pathlib
import re
import subprocess
import struct
import time
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[1]
FRAME_BYTES = 1920 * 1080 * 3 // 2
SOURCES = {'front': 'frontcamera', 'in': 'incamera'}


def compare_samples(samples):
    if len(samples) < 2:
        raise ValueError('at least two samples required')
    return {
        'capturedAt': [sample['capturedAt'] for sample in samples],
        'completeFrames': all(sample['size'] >= FRAME_BYTES and sample['readBytes'] >= FRAME_BYTES + 16 for sample in samples),
        'sameInode': len({sample['inode'] for sample in samples}) == 1,
        'sameMtime': len({sample['mtime'] for sample in samples}) == 1,
        'frameChanged': len({sample['frameSHA256'] for sample in samples}) > 1,
        'prefixChanged': len({sample['prefixSHA256'] for sample in samples}) > 1,
        'lumaChanged': len({sample['lumaSHA256'] for sample in samples}) > 1,
        'chromaChanged': len({sample['chromaSHA256'] for sample in samples}) > 1,
        'producerTimestampChanged': len({tuple(sample['producerTimestamp']) for sample in samples}) > 1,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--serial', required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9._:-]+', args.serial):
        parser.error('invalid serial')
    evidence = ROOT / 'tmp/camera-export-evidence' / (time.strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8])
    evidence.mkdir(parents=True, mode=0o700)
    adb = ['adb', '-s', args.serial]
    samples = {source: [] for source in SOURCES}

    def run(arguments):
        result = subprocess.run(adb + arguments, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        with (evidence / 'transport.txt').open('ab') as log:
            log.write(result.stdout)
        if result.returncode:
            raise SystemExit('Read-only transport failed; inspect private tmp camera evidence')
        return result.stdout

    for sample_index in range(3):
        for source, directory in SOURCES.items():
            path = '/tmp/application/imagerecog/imagedata/' + directory + '/imagedata/value'
            metadata = run(['shell', 'stat', '-c', '%i:%s:%Y', path]).decode().strip()
            inode, size, mtime = map(int, metadata.split(':'))
            target = evidence / (source + '-' + str(sample_index) + '.nv12')
            run(['pull', path, str(target)])
            frame = target.read_bytes()
            producer_timestamp = struct.unpack_from('<qq', frame, FRAME_BYTES) if len(frame) >= FRAME_BYTES + 16 else ()
            samples[source].append({
                'capturedAt': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                'inode': inode, 'size': size, 'mtime': mtime, 'readBytes': len(frame),
                'producerTimestamp': producer_timestamp,
                'frameSHA256': hashlib.sha256(frame[:FRAME_BYTES]).hexdigest(),
                'prefixSHA256': hashlib.sha256(frame[:128]).hexdigest(),
                'lumaSHA256': hashlib.sha256(frame[:1920 * 1080]).hexdigest(),
                'chromaSHA256': hashlib.sha256(frame[1920 * 1080:FRAME_BYTES]).hexdigest(),
            })
        if sample_index < 2:
            time.sleep(1)
    report = {source: compare_samples(values) for source, values in samples.items()}
    (evidence / 'samples.json').write_text(json.dumps(samples, indent=2), encoding='utf8')
    (evidence / 'comparison.json').write_text(json.dumps(report, indent=2), encoding='utf8')
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
