#!/usr/bin/env python3
"""Find moving NP1 recording windows using their same-name NMEA sidecars."""

import argparse
from collections import Counter
import datetime as dt
import hashlib
import json
import math
from pathlib import Path
import re


JST = dt.timezone(dt.timedelta(hours=9))
NAME = re.compile(r'FILEM_(\d{6})_(\d{6})_\d+\.MP4\Z', re.IGNORECASE)


def coordinate(text, sign):
    split = 2 if sign in ('N', 'S') else 3
    minutes = float(text[split:])
    if sign not in ('N', 'S', 'E', 'W') or not 0 <= minutes < 60:
        raise ValueError('invalid position')
    degrees = int(text[:split])
    if degrees < 0:
        raise ValueError('invalid position')
    result = degrees + minutes / 60
    bound = 90 if split == 2 else 180
    if result > bound:
        raise ValueError('invalid position')
    return -result if sign in ('S', 'W') else result


def fixes(path, origin):
    by_second = {}
    for line in path.read_bytes().decode('ascii', errors='ignore').splitlines():
        body, mark, check = line.strip().partition('*')
        if not body.startswith('$') or not mark or len(check) != 2:
            continue
        fields = body[1:].split(',')
        if len(fields) < 10 or not fields[0].endswith('RMC') or fields[2] != 'A':
            continue
        checksum = 0
        for c in body[1:]:
            checksum ^= ord(c)
        try:
            if checksum != int(check, 16):
                continue
            clock = dt.datetime.strptime(fields[9] + fields[1][:6], '%d%m%y%H%M%S').replace(tzinfo=dt.timezone.utc)
            speed = 1.852 * float(fields[7])
            lat, lon = coordinate(fields[3], fields[4]), coordinate(fields[5], fields[6])
            if 0 <= speed <= 190 and (lat or lon):
                by_second[int((clock - origin).total_seconds())] = (lat, lon, speed)
        except (ValueError, IndexError):
            continue
    return by_second


def distance(a, b):
    lat = math.radians((a[0] + b[0]) / 2)
    return 111_195 * math.hypot(b[0] - a[0], (b[1] - a[1]) * math.cos(lat))


def motion(points, offset, seconds):
    observed = [(t, points[t]) for t in range(offset, offset + seconds) if t in points]
    if len(observed) < .8 * seconds:
        return None
    moving = sum(p[2] >= 8 for _, p in observed)
    displacement = distance(observed[0][1], observed[-1][1])
    travelled = sum(distance(p1, p2) for (t1, p1), (t2, p2) in zip(observed, observed[1:]) if t2-t1 <= 3)
    if moving < .55 * seconds or travelled < 2 * seconds or displacement < seconds:
        return None
    return {'fixes': len(observed), 'movingSeconds': moving, 'distanceM': round(travelled), 'displacementM': round(displacement)}


def lighting(time, night_start):
    if 8 <= time.hour <= 16:
        return 'day'
    if time.hour >= night_start or time.hour <= 5:
        return 'night'
    return None


def select(folder, excluded, seconds, night_start=18):
    clips = []
    totals = Counter()
    sidecars = {(p.parent, p.stem.casefold()): p for p in folder.rglob('*') if p.suffix.casefold() == '.nmea'}
    for video in sorted(folder.rglob('*')):
        match = NAME.fullmatch(video.name)
        if not match or not video.is_file():
            continue
        totals['videos'] += 1
        if video.name.casefold() in excluded:
            totals['excludedDuplicateName'] += 1
            continue
        sidecar = sidecars.get((video.parent, video.stem.casefold()))
        if not sidecar:
            totals['missingSidecar'] += 1
            continue
        try:
            start = dt.datetime.strptime(''.join(match.groups()), '%y%m%d%H%M%S').replace(tzinfo=JST)
        except ValueError:
            totals['invalidTimestamp'] += 1
            continue
        points = fixes(sidecar, start)
        if not points:
            totals['noValidFix'] += 1
            continue
        group = lighting(start, night_start)
        if not group:
            totals['twilightExcluded'] += 1
            continue
        candidates = []
        for offset in range(8, min(175, max(points) + 1) - seconds + 1, seconds):
            measurement = motion(points, offset, seconds)
            if measurement:
                candidates.append((offset, measurement))
        if not candidates:
            totals['noMovingWindow'] += 1
            continue
        totals[f'{group}Files'] += 1
        for offset, measurement in candidates:
            actual = start + dt.timedelta(seconds=offset)
            order = hashlib.sha256(f'{video.name}:{offset}'.encode()).hexdigest()
            clips.append({'lighting': group, 'file': str(video.relative_to(folder)), 'offset': offset, 'seconds': seconds,
                          'start': actual.isoformat(), 'motion': measurement, 'order': order})
            totals[f'{group}Windows'] += 1
    clips.sort(key=lambda clip: clip.pop('order'))
    for i, clip in enumerate(clips, 1):
        clip['id'] = f'X{i:03}'
    return clips, totals


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('input', type=Path)
    parser.add_argument('output', type=Path)
    parser.add_argument('--exclude', type=Path, action='append', default=[], help='Folder of already used recordings')
    parser.add_argument('--seconds', type=int, default=45)
    parser.add_argument('--night-start', type=int, default=18, choices=(17, 18),
                        help='Earliest hour for night candidates, subject to visual review')
    args = parser.parse_args()
    if not args.input.is_dir() or not 20 <= args.seconds <= 120:
        parser.error('input must be a directory and the window must be 20 to 120 seconds')
    seen = {p.name.casefold() for folder in args.exclude for p in folder.rglob('*') if p.suffix.casefold() == '.mp4'}
    clips, totals = select(args.input, seen, args.seconds, args.night_start)
    result = {'fps': 4, 'source': str(args.input.resolve()), 'clips': clips, 'counts': dict(totals)}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(totals))


if __name__ == '__main__':
    main()
