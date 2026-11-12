"""Fetch real Japanese licence-plate photos from Wikimedia Commons for evaluation.

Walks Category:License plates of Japan (and subcategories), skips pixelated
plate categories, and saves images (thumbnailed) with their license and
author metadata to meta.json. These photos are used to measure real-world
accuracy; they are not shipped with the project.

    python tools/plate/train/fetch_commons.py --out DATA/commons --limit 600
"""
from __future__ import annotations

import argparse
import json
import time
import urllib.parse
import urllib.request
from pathlib import Path

API = "https://commons.wikimedia.org/w/api.php"
UA = {"User-Agent": "np1-plate-eval/1.0 (dataset evaluation script)"}
ROOT = "Category:License plates of Japan"
SKIP = ("pixelization", "pixelisation", "blurred", "diplomatic", "map of", "logo", "design")


def api(params: dict) -> dict:
    params = {**params, "format": "json"}
    req = urllib.request.Request(API + "?" + urllib.parse.urlencode(params), headers=UA)
    last = None
    for attempt in range(6):
        time.sleep(0.5)  # stay well under the API rate limit
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                return json.load(r)
        except Exception as e:
            last = e
            retry = getattr(e, "headers", None) and e.headers.get("Retry-After")
            time.sleep(int(retry) if retry and str(retry).isdigit() else 5 * (attempt + 1))
    raise RuntimeError(f"Commons API unavailable: {last}")


def members(category: str, kind: str):
    cont = {}
    while True:
        data = api({"action": "query", "list": "categorymembers", "cmtitle": category, "cmtype": kind, "cmlimit": "500", **cont})
        yield from data["query"]["categorymembers"]
        if "continue" not in data:
            return
        cont = data["continue"]


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--limit", type=int, default=600)
    ap.add_argument("--depth", type=int, default=3)
    ap.add_argument("--width", type=int, default=1600)
    args = ap.parse_args()
    (args.out / "images").mkdir(parents=True, exist_ok=True)
    seen_cats, files, queue = set(), {}, [(ROOT, 0)]
    while queue and len(files) < args.limit * 2:
        cat, depth = queue.pop(0)
        if cat in seen_cats or any(s in cat.lower() for s in SKIP):
            continue
        seen_cats.add(cat)
        for m in members(cat, "file"):
            if m["title"].lower().endswith((".jpg", ".jpeg", ".png")):
                files.setdefault(m["pageid"], m["title"])
        if depth < args.depth:
            queue += [(m["title"], depth + 1) for m in members(cat, "subcat")]
    print(f"{len(files)} files in {len(seen_cats)} categories", flush=True)
    meta_path = args.out / "meta.json"
    meta = json.loads(meta_path.read_text(encoding="utf-8")) if meta_path.exists() else {}
    ids = list(files.items())[: args.limit]
    for i in range(0, len(ids), 40):
        chunk = ids[i:i + 40]
        data = api({"action": "query", "pageids": "|".join(str(p) for p, _ in chunk), "prop": "imageinfo",
                    "iiprop": "url|extmetadata", "iiurlwidth": str(args.width)})
        for pid, page in data["query"]["pages"].items():
            info = (page.get("imageinfo") or [{}])[0]
            ext = info.get("extmetadata", {})
            url = info.get("thumburl") or info.get("url")
            target = args.out / "images" / f"{pid}.jpg"
            if not url:
                continue
            if not target.exists():
                try:
                    with urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=60) as r:
                        target.write_bytes(r.read())
                except Exception:
                    continue
                time.sleep(0.2)
            meta[pid] = {"title": page.get("title"), "license": ext.get("LicenseShortName", {}).get("value"),
                         "artist": ext.get("Artist", {}).get("value"), "source": info.get("descriptionurl")}
        meta_path.write_text(json.dumps(meta, ensure_ascii=False, indent=1), encoding="utf-8")
        print(f"{len(meta)} images", flush=True)


if __name__ == "__main__":
    main()
