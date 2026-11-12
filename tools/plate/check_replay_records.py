"""Cross-check replayed plate records against Lipla teacher labels.

A record is "confirmed" when the teacher read the same serial number in a
frame of the same footage within +-window seconds of the record's first
sighting. Unconfirmed records are either misreads or plates the teacher did
not read (small, blurred, side lanes); the report keeps both visible.

    python tools/plate/check_replay_records.py --records records.json --labels dashcam-eval.jsonl --every 2
"""
from __future__ import annotations

import argparse
import json
import re
from datetime import datetime, timedelta, timezone
from pathlib import Path

JST = timezone(timedelta(hours=9))


def frame_time(file: str, every: float) -> datetime | None:
    m = re.search(r"(\d{8})_(\d{6})[^_]*_(\d{4})\.jpg$", file)
    if not m:
        return None
    start = datetime.strptime(m.group(1) + m.group(2), "%Y%m%d%H%M%S").replace(tzinfo=JST)
    return start + timedelta(seconds=(int(m.group(3)) - 1) * every)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--records", type=Path, nargs="+", required=True, help="GET /api/v1/plate/records responses")
    ap.add_argument("--labels", type=Path, required=True)
    ap.add_argument("--every", type=float, default=2.0, help="seconds between extracted frames")
    ap.add_argument("--window", type=float, default=60)
    args = ap.parse_args()
    teacher = []
    for line in args.labels.read_text(encoding="utf-8").splitlines():
        d = json.loads(line)
        t = frame_time(d["file"], args.every)
        for p in d.get("plates", []):
            if not p.get("unread") and p["score"] >= 0.6 and p["numberScore"] >= 0.8 and t:
                teacher.append((t, p["number"], p))
    rows = []
    for f in args.records:
        rows += [r["record"] for r in json.loads(f.read_text(encoding="utf-8"))["records"]]
    confirmed, fields, field_ok = 0, 0, 0
    report = []
    for r in rows:
        at = datetime.fromisoformat(r["firstSeen"])
        near = [p for t, n, p in teacher if abs((t - at).total_seconds()) <= args.window and n == r["reading"]["number"]]
        ok = bool(near)
        confirmed += ok
        if ok:
            p = max(near, key=lambda x: x["score"])
            for field, key, score in (("region", "area", "areaScore"), ("class", "classNumber", "classScore"), ("kana", "kana", "kanaScore")):
                if r["reading"][field] and p[score] >= 0.85:
                    fields += 1
                    field_ok += r["reading"][field] == p[key]
        report.append({"firstSeen": r["firstSeen"], "display": r["display"], "confidence": round(r["reading"]["confidence"], 3),
                       "matches": [m["ruleName"] for m in r["matches"]], "confirmedByTeacher": ok})
    summary = {"records": len(rows), "confirmedByTeacher": confirmed, "confirmedRate": round(confirmed / max(1, len(rows)), 3),
               "fieldsCheckedOnConfirmed": fields, "fieldAccuracyOnConfirmed": round(field_ok / max(1, fields), 3)}
    print(json.dumps(summary, ensure_ascii=False))
    return report, summary


if __name__ == "__main__":
    main()
