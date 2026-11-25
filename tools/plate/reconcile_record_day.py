#!/usr/bin/env python3
"""Prepare one privately reviewed plate-history replacement from a raw snapshot."""

import argparse
from datetime import date, datetime, timedelta, timezone
from hashlib import sha256
import json
from pathlib import Path
import re


JST = timezone(timedelta(hours=9))
FIELDS = ("region", "class", "kana")


def timestamp(value):
    instant = datetime.fromisoformat(value)
    if instant.utcoffset() is None:
        raise ValueError("a timestamp has no timezone")
    return instant


def prepare(raw, plan):
    day = date.fromisoformat(plan["day"])
    if sha256(raw).hexdigest() != plan["snapshotSHA256"]:
        raise ValueError("the source snapshot does not match the review")
    if not raw.endswith(b"\n"):
        raise ValueError("the last source line is incomplete")

    latest = {}
    lines = []
    for line in raw.splitlines(keepends=True):
        record = json.loads(line)
        if timestamp(record["firstSeen"]).astimezone(JST).date() != day:
            raise ValueError("source contains a different recording day")
        key = record["id"]
        latest[key] = record
        lines.append((key, line))

    touched = set()
    merged = []
    for group in plan["groups"]:
        source_ids = group["sources"]
        if len(set(source_ids)) != len(source_ids) or len(source_ids) < 2 or touched.intersection(source_ids):
            raise ValueError("groups must contain distinct and disjoint source IDs")
        records = sorted((latest[key] for key in source_ids), key=lambda r: timestamp(r["firstSeen"]))
        serial = group["serial"]
        basis = group.get("serialBasis", "optical")
        if basis not in ("optical", "matching-existing") or not re.fullmatch(r"[1-9][0-9]{0,3}", serial) or not group["evidence"].strip():
            raise ValueError("a serial and vehicle evidence are required")
        corrections = {r["id"] for r in records if r["reading"]["number"] != serial}
        if corrections != set(group.get("correctedSources", ())) or basis == "matching-existing" and corrections:
            raise ValueError("every differing source serial needs explicit optical confirmation")
        if not any(r["id"] not in corrections for r in records):
            raise ValueError("one source must already have the confirmed serial")

        end = max(records, key=lambda r: timestamp(r["lastSeen"]))
        overlap = any(timestamp(right["firstSeen"]) <= timestamp(left["lastSeen"])
                      for left, right in zip(records, records[1:]))
        sightings = sum(r["sightings"] for r in records)
        if overlap:
            count = group.get("overlapSightings")
            if type(count) is not int or not max(r["sightings"] for r in records) <= count <= sightings:
                raise ValueError("overlapping detections need an explicit reviewed union count")
            sightings = count

        image = next((r for r in records if r["id"] == group["imageSource"] and r.get("imageId") == r["id"]), None)
        if image is None:
            raise ValueError("the chosen source does not have a stored image")
        fields = group.get("fields", {})
        if set(fields).difference(FIELDS) or any(not f["value"].strip() or not f["evidence"].strip() for f in fields.values()):
            raise ValueError("every completed optional field needs field-specific evidence")
        reading = {field: fields.get(field, {}).get("value", "") for field in FIELDS}
        reading["number"] = serial
        reading["confidence"] = max(r["reading"]["confidence"] for r in records if r["id"] not in corrections)
        number = f"{serial[:2]}-{serial[2:]}" if len(serial) == 4 else "・" * (4 - len(serial)) + serial
        display = " ".join([value for field, value in reading.items() if field in FIELDS and value] + [number])
        # The Web serves a JPEG by record ID, so use the ID of the chosen image.
        result = dict(records[0], id=image["id"], lastSeen=end["lastSeen"], sightings=sightings, reading=reading,
                      display=display, imageId=image["imageId"], box=image["box"])
        result.pop("imageNote", None)
        if image.get("imageNote"):
            result["imageNote"] = image["imageNote"]
        location = next((r["location"] for r in records if r.get("location")), None)
        if location:
            result["location"] = location
        matches = {item["ruleId"]: item for r in reversed(records) for item in r.get("matches") or []}
        result["matches"] = list(matches.values())
        if any(r.get("speech") == "spoken" for r in records):
            result["speech"] = "spoken"
        elif not result.get("speech"):
            speech = next((r["speech"] for r in records if r.get("speech")), None)
            if speech:
                result["speech"] = speech
        merged.append(result)
        touched.update(source_ids)

    if not merged:
        raise ValueError("the review did not authorize a merge")
    replacement = b"".join(line for key, line in lines if key not in touched)
    for record in merged:
        replacement += (json.dumps(record, ensure_ascii=False, separators=(",", ":")) + "\n").encode("utf-8")
    return replacement, {"day": str(day), "previousRecords": len(latest), "resultRecords": len(latest) - len(touched) + len(merged),
                         "sourceSHA256": plan["snapshotSHA256"], "replacementSHA256": sha256(replacement).hexdigest(),
                         "removedIDs": sorted(touched.difference(r["id"] for r in merged)), "merged": merged}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("snapshot", type=Path)
    parser.add_argument("review", type=Path)
    parser.add_argument("outdir", type=Path)
    args = parser.parse_args()
    replacement, result = prepare(args.snapshot.read_bytes(), json.loads(args.review.read_bytes()))
    args.outdir.mkdir(parents=True, exist_ok=True)
    (args.outdir / "replacement.jsonl").write_bytes(replacement)
    (args.outdir / "readback.private.json").write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
    (args.outdir / "source.sha256").write_bytes((result["sourceSHA256"] + "\n").encode("ascii"))
    (args.outdir / "replacement.sha256").write_bytes((result["replacementSHA256"] + "\n").encode("ascii"))
    print(f"{result['day']}: {result['previousRecords']} -> {result['resultRecords']} records; {len(result['merged'])} groups")


if __name__ == "__main__":
    main()
