#!/usr/bin/env python3
"""Score plate records against independently reviewed vehicle intervals.

References are a JSON array of {clip, vehicle, lighting, number}. The id
(clip, vehicle) represents a physical vehicle during one interval. Each audited
result file is a JSON array of {clip, id, number, verdict, vehicle?, mixed?}.
Verdicts are linked (requires a reference vehicle), false_detection, or unknown.
Reviewers flag mixed tracks after checking the contributing frames.
"""

import argparse
from collections import Counter
import json
import math
from pathlib import Path
import re


LIGHTING = ('day', 'night')
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
    aggregate, day, night = (results[k] for k in ('all', *LIGHTING))
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
    args = parser.parse_args()
    try:
        references, lighting = read_references(args.references)
        reports = {}
        for path in args.audits:
            results = score(references, lighting, load_rows(path))
            checks = gate(results)
            reports[str(path)] = {'passed': all(checks.values()), 'checks': checks, 'results': results}
    except (OSError, ValueError) as error:
        parser.exit(2, f'{error}\n')
    print(json.dumps(reports, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
