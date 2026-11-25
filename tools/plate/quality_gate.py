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
"""

import argparse
from collections import Counter
import json
import math
from pathlib import Path
import re


LIGHTING = ('day', 'night', 'twilight')
SERIAL = re.compile(r'[1-9][0-9]{0,3}\Z')


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


def check_audit_coverage(replay_path, audits):
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
            saved[key] = nonempty(record['reading'], 'number')

    audited = {}
    for row in audits:
        clip_key = 'originalClip' if 'originalClip' in row else 'clip'
        key = nonempty(row, clip_key), nonempty(row, 'id')
        if key in audited:
            raise ValueError(f'{replay_path}: duplicate audited record ID in {key[0]}')
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
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('references', type=Path)
    parser.add_argument('audits', type=Path, nargs='+')
    parser.add_argument('--records', action='append', type=Path, metavar='REPLAY_JSON',
                        help='Saved replay results, in the same order as the audit files')
    args = parser.parse_args()
    if args.records is not None and len(args.records) != len(args.audits):
        parser.error('provide one --records file for every audit file')
    try:
        references, lighting = read_references(args.references)
        reports = {}
        for i, path in enumerate(args.audits):
            audit = load_rows(path)
            checked = check_audit_coverage(args.records[i], audit) if args.records is not None else None
            results = score(references, lighting, audit)
            checks = gate(results)
            report = {'passed': all(checks.values()), 'checks': checks, 'results': results}
            if checked is not None:
                report['auditedSavedRecords'] = checked
            reports[str(path)] = report
    except (OSError, ValueError) as error:
        parser.exit(2, f'{error}\n')
    print(json.dumps(reports, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
