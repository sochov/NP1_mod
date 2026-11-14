"""Summarize hand-verified evaluations over several datasets (evaluation kit layout).

    KIT/datasets/<dataset>/gt/candidates.json, verdicts.json   (gt_candidates.py + eye)
    KIT/engine-outputs/<dataset>/<model>.jsonl                 (eval_video.py run)

    python tools/plate/gt_report.py --kit DATA/eval-kit --models v1b v6b

Prints Markdown tables (per dataset and summed) of one-shot reading precision and of
records under each confirmation policy, and writes KIT/results/report.json and .md.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from gt_score import score  # noqa: E402

READS = ["conf>=0.6", "conf>=0.7", "conf>=0.8", "conf>=0.7 w>=45"]
POLICIES = ["min2", "min1", "min2+instant0.7", "min2+instant0.8"]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--kit", type=Path, required=True)
    ap.add_argument("--models", nargs="+", required=True)
    ap.add_argument("--datasets", nargs="*", help="default: every folder in KIT/datasets")
    a = ap.parse_args()
    datasets = a.datasets or sorted(p.name for p in (a.kit / "datasets").iterdir() if (p / "gt" / "verdicts.json").exists())
    res, cars = {}, {}
    for ds in datasets:
        engines = [a.kit / "engine-outputs" / ds / f"{m}.jsonl" for m in a.models]
        r = score(a.kit / "datasets" / ds / "gt", [str(e) for e in engines], instant=(0.7, 0.8))
        cars[ds] = r["verifiedCorrectGroups"]
        for m, e in zip(a.models, engines):
            res.setdefault(m, {})[ds] = r["runs"][e.stem]
    lines = [f"# 評価結果（{', '.join(datasets)}）", "", f"目視で正しいと確認できた車（候補グループ）：{sum(cars.values())}（" + "、".join(f"{d} {n}" for d, n in cars.items()) + "）", ""]

    def total(m, section, key):
        t = {}
        for ds in datasets:
            for k, v in res[m][ds][section][key].items() if isinstance(res[m][ds][section][key], dict) else [("n", res[m][ds][section][key])]:
                if isinstance(v, int):
                    t[k] = t.get(k, 0) + v
        return t

    lines += ["## 1フレームの読み（合計）", "", "| モデル | " + " | ".join(READS) + " |", "| --- |" + " --- |" * len(READS)]
    for m in a.models:
        cells = []
        for k in READS:
            t = total(m, "oneShotReads", k)
            cells.append(f"{t['ok'] / max(1, t['ok'] + t['bad']):.1%}（正{t['ok']}・誤{t['bad']}・車{total(m, 'oneShotCars', k)['n']}）")
        lines.append(f"| {m} | " + " | ".join(cells) + " |")
    lines += ["", "## 記録（合計。本体の記録規則を再現）", "", "| モデル | " + " | ".join(POLICIES) + " |", "| --- |" + " --- |" * len(POLICIES)]
    for m in a.models:
        cells = []
        for k in POLICIES:
            t = total(m, "records", k)
            cells.append(f"{t['ok'] / max(1, t['ok'] + t['bad']):.1%}（正{t['ok']}・誤{t['bad']}・車{t['correctCars']}）")
        lines.append(f"| {m} | " + " | ".join(cells) + " |")
    lines += ["", "## データセット別（記録：min2+instant0.7）", "", "| モデル | " + " | ".join(datasets) + " |", "| --- |" + " --- |" * len(datasets)]
    for m in a.models:
        cells = []
        for ds in datasets:
            r = res[m][ds]["records"]["min2+instant0.7"]
            cells.append(f"{r['precision']:.1%}（正{r['ok']}・誤{r['bad']}・車{r['correctCars']}）")
        lines.append(f"| {m} | " + " | ".join(cells) + " |")
    lines += ["", "- 正答率は 正 /（正＋誤）。判読できない（unk）と未判定（unverified）は数えない。",
              "- 車＝目視で正しいと確認できた候補グループのうち、そのモデル・方式で1回以上正しく読めた（記録できた）数。",
              "- min2：2回検出で確定。min1：1回でも記録。min2+instantX：2回検出、または信頼度X以上・写り幅45px以上なら1回で確定。"]
    text = "\n".join(lines) + "\n"
    print(text)
    out = a.kit / "results"
    out.mkdir(parents=True, exist_ok=True)
    (out / "report.md").write_text(text, encoding="utf-8")
    (out / "report.json").write_text(json.dumps({"datasets": datasets, "verifiedCorrectGroups": cars, "models": res}, ensure_ascii=False, indent=1), encoding="utf-8")


if __name__ == "__main__":
    main()
