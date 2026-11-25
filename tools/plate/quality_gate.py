#!/usr/bin/env python3
"""Score plate records against independently reviewed vehicle intervals.

References are a JSON array of {clip, vehicle, lighting, number}. The id
(clip, vehicle) represents a physical vehicle during one interval. Each audited
result file is a JSON array of {clip, id, number, verdict, vehicle?, mixed?}.
Twilight contributes to overall results and is reported separately from day
and night, which each have their own minimum sample size and capture rate.
Verdicts are linked (requires a reference vehicle), readback (requires the
visually reviewed serial in actual), false_detection, or unknown. Readback
checks precision for outputs outside the fixed recall reference intervals.
Reviewers flag mixed tracks after checking the contributing frames.
Use --records once per audit to check that every saved record from that
replay is included, with the original clip, record ID, and serial.
Use originalClip when an audit links a saved record to a reference from a
different clip of the same encounter.
Use --cohort for an independent run registered with the freeze-cohort command.
"""

import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import math
from pathlib import Path
import re
import sys


LIGHTING = ('day', 'night', 'twilight')
SERIAL = re.compile(r'[1-9][0-9]{0,3}\Z')
SHA256 = re.compile(r'[0-9a-f]{64}\Z', re.IGNORECASE)


def load_rows(path):
    rows = json.loads(path.read_text(encoding='utf-8-sig'))
    if not isinstance(rows, list) or any(not isinstance(row, dict) for row in rows):
        raise ValueError(f'{path}: expected an array of objects')
    return rows


def nonempty(row, key):
    value = row.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f'Review row requires a non-empty {key}')
    return value


def read_references(path):
    references = {}
    lighting = {}
    for row in load_rows(path):
        key = nonempty(row, 'clip'), nonempty(row, 'vehicle')
        group, number = nonempty(row, 'lighting'), nonempty(row, 'number')
        if key in references or group not in LIGHTING or not SERIAL.fullmatch(number):
            raise ValueError(f'{path}: invalid or duplicate reference for {key}')
        if lighting.setdefault(key[0], group) != group:
            raise ValueError(f'{path}: conflicting lighting for {key[0]}')
        references[key] = number
    return references, lighting


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def timestamp(value):
    if not isinstance(value, str) or 'T' not in value:
        raise ValueError('Expected an ISO 8601 recording time with a timezone')
    at = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if at.utcoffset() is None:
        raise ValueError('Recording time must include a timezone')
    return at.astimezone(timezone.utc)


def interval(row):
    start, end = (timestamp(row.get(k)) for k in ('from', 'to'))
    if start >= end:
        raise ValueError('Recording span must have a positive duration')
    return start, end


def source_digest(row):
    value = nonempty(row, 'sha256').lower()
    if not SHA256.fullmatch(value):
        raise ValueError('Invalid recording SHA-256')
    return value


def cohort_clips(rows):
    if not isinstance(rows, list) or not rows:
        raise ValueError('A cohort requires at least one recording clip')
    clips = {}
    timeline = []
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError('A recording clip must be an object')
        clip, group = nonempty(row, 'clip'), nonempty(row, 'lighting')
        if clip in clips or group not in LIGHTING:
            raise ValueError(f'Invalid or duplicate cohort clip {clip}')
        parts = row.get('parts')
        if not isinstance(parts, list) or not parts or any(not isinstance(p, dict) for p in parts):
            raise ValueError(f'Cohort clip {clip} requires recording spans')
        spans = [interval(part) for part in parts]
        for part in parts:
            source_digest(part)
        clips[clip] = group, spans
        timeline.extend((start, end, clip) for start, end in spans)
    ordered = sorted(timeline)
    for previous, following in zip(ordered, ordered[1:]):
        if following[0] < previous[1]:
            raise ValueError(f'Overlapping cohort spans in {previous[2]} and {following[2]}')
    return clips


def check_cohort_references(clips, refs_path):
    refs, lighting = read_references(refs_path)
    encounters = set()
    for clip, vehicle in refs:
        if clip not in clips or clips[clip][0] != lighting[clip]:
            raise ValueError(f'Reference lighting or clip differs from the frozen plan: {clip}')
        if vehicle in encounters:
            raise ValueError(f'Encounter {vehicle} appears in more than one reference')
        encounters.add(vehicle)


def freeze_cohort(references_path, plan_path, excluded_path, output):
    excluded = load_rows(excluded_path)
    if not excluded:
        raise ValueError('A cohort requires the development recording ledger')
    excluded_spans = [interval(row) for row in excluded]
    excluded_hashes = set()
    excluded_paths = {}
    for row in excluded:
        if ('sha256' in row) == ('source' in row):
            raise ValueError('Development recording requires either sha256 or source')
        if 'sha256' in row:
            excluded_hashes.add(source_digest(row))
        else:
            path = (excluded_path.parent / nonempty(row, 'source')).resolve()
            excluded_paths[path] = path.stat().st_size
    plan = load_rows(plan_path)

    hashes = {}
    checked_sizes = set()
    frozen = []
    for clip in plan:
        name = nonempty(clip, 'clip')
        parts = clip.get('parts')
        if not isinstance(parts, list) or not parts or any(not isinstance(p, dict) for p in parts):
            raise ValueError(f'Cohort clip {name} requires recording spans')
        spans = []
        for part in parts:
            start, end = interval(part)
            if any(start < used_end and used_start < end for used_start, used_end in excluded_spans):
                raise ValueError(f'Cohort clip {name} overlaps development recording time')
            path = (plan_path.parent / nonempty(part, 'source')).resolve()
            if path not in hashes:
                size = path.stat().st_size
                if size == 0:
                    raise ValueError(f'Cohort clip {name} has an empty source recording')
                # Files of different lengths cannot have identical bytes.
                if size not in checked_sizes:
                    excluded_hashes.update(sha256(p) for p, length in excluded_paths.items() if length == size)
                    checked_sizes.add(size)
                hashes[path] = sha256(path)
            digest = hashes[path]
            if digest in excluded_hashes:
                raise ValueError(f'Cohort clip {name} uses a development recording')
            spans.append({'sha256': digest, 'from': start.isoformat(), 'to': end.isoformat()})
        frozen.append({'clip': name, 'lighting': nonempty(clip, 'lighting'), 'parts': spans})
    clips = cohort_clips(frozen)
    check_cohort_references(clips, references_path)
    manifest = {'version': 1, 'frozenAt': datetime.now(timezone.utc).isoformat(),
                'referencesSha256': sha256(references_path), 'excludedSourcesSha256': sha256(excluded_path),
                'clips': frozen}
    with output.open('x', encoding='utf-8') as file:
        json.dump(manifest, file, ensure_ascii=False, indent=2)
        file.write('\n')


def read_cohort(path, references_path):
    data = json.loads(path.read_text(encoding='utf-8-sig'))
    if not isinstance(data, dict) or data.get('version') != 1:
        raise ValueError(f'{path}: unsupported cohort manifest')
    timestamp(data.get('frozenAt'))
    excluded = data.get('excludedSourcesSha256')
    if not isinstance(excluded, str) or not SHA256.fullmatch(excluded):
        raise ValueError(f'{path}: missing development ledger SHA-256')
    if data.get('referencesSha256') != sha256(references_path):
        raise ValueError(f'{path}: reference file changed since the cohort was frozen')
    clips = cohort_clips(data.get('clips'))
    check_cohort_references(clips, references_path)
    return clips


def check_audit_coverage(replay_path, audits, cohort=None):
    saved = {}
    clips = set()
    for scene in load_rows(replay_path):
        clip = nonempty(scene, 'clip')
        records = scene.get('records')
        if clip in clips or 'records' not in scene or (records is not None and not isinstance(records, list)):
            raise ValueError(f'{replay_path}: invalid or duplicate replay clip {clip}')
        clips.add(clip)
        for record in records or []:
            if not isinstance(record, dict) or not isinstance(record.get('reading'), dict):
                raise ValueError(f'{replay_path}: malformed saved record in {clip}')
            key = clip, nonempty(record, 'id')
            if key in saved:
                raise ValueError(f'{replay_path}: duplicate saved record ID in {clip}')
            if cohort is not None and clip in cohort:
                first, last = (timestamp(record.get(k)) for k in ('firstSeen', 'lastSeen'))
                spans = cohort[clip][1]
                if first > last or any(not any(start <= at < end for start, end in spans) for at in (first, last)):
                    raise ValueError(f'{replay_path}: saved interval falls outside frozen clip {clip}')
            saved[key] = nonempty(record['reading'], 'number')

    if cohort is not None and clips != cohort.keys():
        missing, extra = set(cohort) - clips, clips - cohort.keys()
        raise ValueError(f'{replay_path}: replay differs from frozen clips ({len(missing)} missing, {len(extra)} extra)')

    audited = {}
    for row in audits:
        clip_key = 'originalClip' if 'originalClip' in row else 'clip'
        key = nonempty(row, clip_key), nonempty(row, 'id')
        if key in audited:
            raise ValueError(f'{replay_path}: duplicate audited record ID in {key[0]}')
        if cohort is not None and 'originalClip' in row:
            linked_clip = nonempty(row, 'clip')
            if linked_clip not in cohort or key[0] not in cohort or cohort[linked_clip][0] != cohort[key[0]][0]:
                raise ValueError(f'{replay_path}: linked audit crosses lighting or leaves the frozen cohort')
        audited[key] = row.get('number')
    missing = saved.keys() - audited.keys()
    extra = audited.keys() - saved.keys()
    changed = sum(saved[key] != audited[key] for key in saved.keys() & audited.keys())
    if missing or extra or changed:
        raise ValueError(
            f'{replay_path}: audit disagrees with saved results '
            f'({len(missing)} missing, {len(extra)} extra, {changed} serial mismatches)'
        )
    return len(saved)


def wilson_lower(good, total):
    if not total:
        return 0.0
    z = 1.959963984540054
    p = good / total
    return (p + z*z/(2*total) - z*math.sqrt((p*(1-p) + z*z/(4*total))/total)) / (1 + z*z/total)


def score(references, lighting, rows):
    counts = {group: Counter() for group in ('all', *LIGHTING)}
    captured = Counter()
    seen = set()
    for row in rows:
        clip, record = nonempty(row, 'clip'), nonempty(row, 'id')
        if (clip, record) in seen:
            raise ValueError(f'Duplicate audited record {(clip, record)}')
        seen.add((clip, record))
        group = row.get('lighting', lighting.get(clip))
        if group not in LIGHTING or (clip in lighting and lighting[clip] != group):
            raise ValueError(f'Audited record {record} requires the lighting for {clip}')
        number = row.get('number')
        if not isinstance(number, str) or type(row.get('mixed', False)) is not bool:
            raise ValueError(f'Audited record {record} has invalid number or mixed fields')
        verdict = row.get('verdict')
        if verdict == 'linked':
            vehicle = clip, nonempty(row, 'vehicle')
            if vehicle not in references:
                raise ValueError(f'Audited record {record} links to absent reference {vehicle}')
            verdict = 'correct' if number == references[vehicle] else 'wrong'
            if verdict == 'correct':
                captured[vehicle] += 1
        elif verdict == 'readback':
            actual = nonempty(row, 'actual')
            if not SERIAL.fullmatch(actual):
                raise ValueError(f'Audited record {record} has an invalid visually reviewed serial')
            verdict = 'correct' if number == actual else 'wrong'
        elif verdict == 'false_detection':
            verdict = 'wrong'
        elif verdict != 'unknown':
            raise ValueError(f'Audited record {record} has invalid verdict')
        for total in (counts['all'], counts[group]):
            total[verdict] += 1
            total['mixed'] += int(row.get('mixed', False))

    for key in references:
        for total in (counts['all'], counts[lighting[key[0]]]):
            total['references'] += 1
            total['captured'] += int(captured[key] > 0)
            total['duplicates'] += max(0, captured[key]-1)

    results = {}
    for group, total in counts.items():
        if group == 'twilight' and not total:
            continue
        known = total['correct'] + total['wrong']
        results[group] = {
            **{key: total[key] for key in ('references', 'captured', 'correct', 'wrong', 'unknown', 'duplicates', 'mixed')},
            'precision': total['correct']/known if known else 0.0,
            'precisionWilson95Lower': wilson_lower(total['correct'], known),
            'recall': total['captured']/total['references'] if total['references'] else 0.0,
            'duplicateRate': total['duplicates']/total['captured'] if total['captured'] else 0.0,
        }
    return results


def gate(results):
    aggregate, day, night = (results[k] for k in ('all', 'day', 'night'))
    return {
        'sampleSize': aggregate['references'] >= 120 and day['references'] >= 40 and night['references'] >= 40,
        'precision': aggregate['precision'] >= .98 and aggregate['precisionWilson95Lower'] >= .95,
        'recall': aggregate['recall'] >= .70 and day['recall'] >= .75 and night['recall'] >= .65,
        'duplicates': aggregate['duplicateRate'] <= .10,
        'mixed': aggregate['mixed'] == 0,
    }


def main():
    if sys.argv[1:2] == ['freeze-cohort']:
        parser = argparse.ArgumentParser(description='Register a plate evaluation cohort before replay.')
        parser.add_argument('references', type=Path)
        parser.add_argument('plan', type=Path)
        parser.add_argument('excluded', type=Path)
        parser.add_argument('output', type=Path)
        args = parser.parse_args(sys.argv[2:])
        try:
            freeze_cohort(args.references, args.plan, args.excluded, args.output)
        except (OSError, ValueError) as error:
            parser.exit(2, f'{error}\n')
        return

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('references', type=Path)
    parser.add_argument('audits', type=Path, nargs='+')
    parser.add_argument('--records', action='append', type=Path, metavar='REPLAY_JSON',
                        help='Saved replay results, in the same order as the audit files')
    parser.add_argument('--cohort', type=Path, help='Frozen independent cohort manifest')
    args = parser.parse_args()
    if args.cohort is not None and args.records is None:
        parser.error('--cohort requires one --records file for every audit file')
    if args.records is not None and len(args.records) != len(args.audits):
        parser.error('provide one --records file for every audit file')
    try:
        references, lighting = read_references(args.references)
        cohort = read_cohort(args.cohort, args.references) if args.cohort is not None else None
        if cohort is not None:
            lighting = {clip: values[0] for clip, values in cohort.items()}
        cohort_digest = sha256(args.cohort) if cohort is not None else None
        reports = {}
        for i, path in enumerate(args.audits):
            audit = load_rows(path)
            checked = check_audit_coverage(args.records[i], audit, cohort) if args.records is not None else None
            results = score(references, lighting, audit)
            checks = gate(results)
            report = {'passed': all(checks.values()), 'checks': checks, 'results': results}
            if checked is not None:
                report['auditedSavedRecords'] = checked
            if cohort is not None:
                report['auditedClips'] = len(cohort)
                report['cohortSha256'] = cohort_digest
            reports[str(path)] = report
    except (OSError, ValueError) as error:
        parser.exit(2, f'{error}\n')
    print(json.dumps(reports, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
