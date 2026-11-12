"""Build a local HTML report with per-field accuracy and image samples.

The report contains other people's plates and places, so it is written only
to a private directory outside the repository and never to the
repository or an external service.

    python tools/plate/make_report.py --package DATA/package-v1b \
        --frames DATA/dashcam/eval --labels DATA/labels/dashcam-eval.jsonl \
        --out DATA/report [--reader-fields reader-fields.json] [--replay-shots DIR]
"""
from __future__ import annotations

import argparse
import base64
import html
import json
import random
from pathlib import Path

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFont

import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import eval_engine as ev  # noqa: E402

FONT = "C:/Windows/Fonts/YuGothB.ttc"


def b64jpg(img_bgr: np.ndarray, quality: int = 85) -> str:
    ok, buf = cv2.imencode(".jpg", img_bgr, [cv2.IMWRITE_JPEG_QUALITY, quality])
    return "data:image/jpeg;base64," + base64.b64encode(buf.tobytes()).decode()


def crop(frame: np.ndarray, box, scale_w: int = 360) -> np.ndarray:
    x0, y0, x1, y1 = [int(v) for v in box]
    w, h = x1 - x0, y1 - y0
    m = int(max(w, h) * 0.8)
    c = frame[max(0, y0 - m):y1 + m, max(0, x0 - m):x1 + m].copy()
    ox, oy = max(0, x0 - m), max(0, y0 - m)
    cv2.rectangle(c, (x0 - ox, y0 - oy), (x1 - ox, y1 - oy), (0, 140, 255), 2)
    s = scale_w / max(1, c.shape[1])
    return cv2.resize(c, (scale_w, max(1, int(c.shape[0] * s))), interpolation=cv2.INTER_CUBIC if s > 1 else cv2.INTER_AREA)


def thumb(frame: np.ndarray, box, roi) -> np.ndarray:
    t = frame.copy()
    x0, y0, x1, y1 = [int(v) for v in box]
    cv2.rectangle(t, (roi[0], roi[1]), (roi[0] + roi[2], roi[1] + roi[3]), (80, 200, 80), 3)
    cv2.rectangle(t, (x0 - 6, y0 - 6), (x1 + 6, y1 + 6), (0, 140, 255), 5)
    return cv2.resize(t, (480, 270), interpolation=cv2.INTER_AREA)


def fmt_reading(r: dict) -> str:
    return " ".join(x for x in (r.get("region"), r.get("class"), r.get("kana"), r.get("number")) if x) or "(なし)"


def fmt_teacher(t: dict) -> str:
    parts = []
    if t["areaScore"] >= 0.85:
        parts.append(t["area"])
    if t["classScore"] >= 0.85:
        parts.append(t["classNumber"])
    if t["kanaScore"] >= 0.85:
        parts.append(t["kana"])
    parts.append(t["number"])
    return " ".join(parts)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--package", type=Path, required=True)
    ap.add_argument("--frames", type=Path, required=True)
    ap.add_argument("--labels", type=Path, required=True)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--reader-fields", type=Path, help="JSON from reader field evaluation")
    ap.add_argument("--replay-shots", type=Path, nargs="*", default=[], help="UI screenshots to embed")
    ap.add_argument("--replay-summary", type=Path, help="JSON lines of replay comparison")
    ap.add_argument("--per-kind", type=int, default=12)
    ap.add_argument("--reader-model", type=Path, help="reader .pt for photo samples")
    ap.add_argument("--real-cache", type=Path, help="real plate cache (validation split is shown)")
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    cfg = json.loads((args.package / "plate-model.json").read_text(encoding="utf-8"))
    roi = cfg["detector"]["roi"]
    cfg_path = args.out / "engine-config.json"
    cfg_path.write_text(json.dumps(cfg, ensure_ascii=False), encoding="utf-8")
    engine = ev.EngineProcess(ev.ROOT / "artifacts/plate/plate-engine-host", args.package, cfg_path, True)
    kinds = {k: [] for k in ("correct", "number_wrong", "field_wrong", "unmatched", "missed")}
    per_field = {f: {"correct": 0, "wrong": 0, "blank": 0} for f in ("number", "region", "class", "kana")}
    # The same, only for reads the server would record (reader confidence >= 0.6).
    recorded = {f: {"correct": 0, "wrong": 0, "blank": 0} for f in ("number", "region", "class", "kana")}
    lines = [json.loads(l) for l in args.labels.read_text(encoding="utf-8").splitlines() if l.strip()]
    for item in lines:
        if "plates" not in item:
            continue
        bgr = cv2.imread(str(args.frames / item["file"]))
        if bgr is None:
            continue
        if bgr.shape[:2] != (ev.H, ev.W):
            bgr = cv2.resize(bgr, (ev.W, ev.H), interpolation=cv2.INTER_AREA)
        reply = engine.run(ev.rgb_to_nv12(cv2.cvtColor(bgr, cv2.COLOR_BGR2RGB)))
        reads = [(p, [p["box"][0], p["box"][1], p["box"][0] + p["box"][2], p["box"][1] + p["box"][3]]) for p in reply["plates"]]
        teacher = [(t, [*np.asarray(t["quad"]).min(0), *np.asarray(t["quad"]).max(0)]) for t in item["plates"]]
        used = set()
        for t, tb in teacher:
            # Metrics use confident teacher plates only; samples also show
            # less certain ones so more distinct vehicles appear.
            if t.get("unread") or t["score"] < 0.6 or t["numberScore"] < 0.8:
                continue
            strict = t["score"] >= 0.75 and t["numberScore"] >= 0.9
            cx, cy = (tb[0] + tb[2]) / 2, (tb[1] + tb[3]) / 2
            if not (roi[0] <= cx <= roi[0] + roi[2] and roi[1] <= cy <= roi[1] + roi[3]):
                continue
            best = max(range(len(reads)), key=lambda i: ev.box_iou(reads[i][1], tb), default=None)
            case = {"file": item["file"], "teacher": fmt_teacher(t) + ("" if strict else f"（Lipla確信度 {t['score']:.2f}）"), "width": int(tb[2] - tb[0]), "key": t["number"]}
            if best is None or ev.box_iou(reads[best][1], tb) < 0.3:
                kinds["missed"].append({**case, "crop": crop(bgr, tb), "thumb": thumb(bgr, tb, roi), "engine": "(検出なし)", "conf": None})
                continue
            used.add(best)
            r = reads[best][0]["reading"]
            num_ok = r["number"] == t["number"]
            tables = ([per_field] + ([recorded] if r["confidence"] >= 0.6 else [])) if strict else []
            for tbl in tables:
                tbl["number"]["correct" if num_ok else ("blank" if not r["number"] else "wrong")] += 1
            field_bad = []
            for field, key, score in (("region", "area", "areaScore"), ("class", "classNumber", "classScore"), ("kana", "kana", "kanaScore")):
                if t[score] >= 0.85:
                    outcome = "blank" if not r[field] else ("correct" if r[field] == t[key] else "wrong")
                    for tbl in tables:
                        tbl[field][outcome] += 1
                    if outcome == "wrong":
                        field_bad.append(field)
            case.update(engine=fmt_reading(r), conf=r["confidence"], crop=crop(bgr, reads[best][1]), thumb=thumb(bgr, reads[best][1], roi))
            kinds["number_wrong" if not num_ok else ("field_wrong" if field_bad else "correct")].append(case)
        for i, (p, rb) in enumerate(reads):
            if i in used or any(ev.box_iou(rb, tb) >= 0.3 for _, tb in teacher):
                continue
            if p["reading"]["confidence"] >= 0.6 and p["reading"]["number"]:
                kinds["unmatched"].append({"file": item["file"], "teacher": "(Liplaのラベルなし)", "engine": fmt_reading(p["reading"]), "key": p["reading"]["number"],
                                           "conf": p["reading"]["confidence"], "width": p["box"][2], "crop": crop(bgr, rb), "thumb": thumb(bgr, rb, roi)})
    engine.close()
    rng = random.Random(7)
    counts = {k: len(v) for k, v in kinds.items()}
    # One sample per serial number: a car followed in a jam appears in dozens
    # of frames and would otherwise fill a whole section.
    distinct = {}
    for k in kinds:
        groups = {}
        for c in kinds[k]:
            groups.setdefault(c["key"], []).append(c)
        distinct[k] = len(groups)
        picks = []
        for key, cases in groups.items():
            c = rng.choice(cases)
            c["frames"] = len(cases)
            picks.append(c)
        rng.shuffle(picks)
        kinds[k] = picks[: args.per_kind]
    counts = {k: f"{counts[k]}件・{distinct[k]}種類の番号" for k in counts}
    reader_fields = json.loads(args.reader_fields.read_text(encoding="utf-8")) if args.reader_fields else None
    photo_cards = photo_samples(args.reader_model, args.real_cache, rng) if args.reader_model and args.real_cache else []
    (args.out / "summary.json").write_text(json.dumps({"perField": per_field, "recorded": recorded, "counts": counts, "readerFields": reader_fields}, ensure_ascii=False, indent=1), encoding="utf-8")
    write_html(args, per_field, counts, kinds, reader_fields, recorded, photo_cards)
    print(json.dumps({"perField": per_field, "recorded": recorded, "counts": counts}, ensure_ascii=False))


TITLES = {
    "correct": ("正しく読めた例", "エンジンの読みが、Liplaのラベル（確信度の高い項目）と一致したもの。"),
    "number_wrong": ("一連番号を誤読した例", "検出はできたが、4桁の一連番号がLiplaと違ったもの。"),
    "field_wrong": ("地名・分類番号・ひらがなを誤読した例", "一連番号は正しいが、地名・分類番号・ひらがなのどれかが、空欄ではなく誤った値になったもの。"),
    "unmatched": ("Liplaのラベルが無い場所での読み（信頼度0.6以上）", "誤検出か、Liplaが見落としたナンバーの正しい読みのどちらか。画像で判断してほしい。"),
    "missed": ("見逃した例", "Liplaが確信を持って読んだのに、エンジンが検出できなかったもの（ROI内）。"),
}
FIELDS = {"number": "一連番号（4桁）", "region": "地名", "class": "分類番号", "kana": "ひらがな"}


def pct(a, b):
    return f"{100 * a / b:.0f}%" if b else "—"


def field_rows(table):
    rows = []
    for f, v in table.items():
        n = sum(v.values())
        read = v["correct"] + v["wrong"]
        rows.append(f"<tr><th>{FIELDS[f]}</th><td>{n}</td><td>{v['correct']}</td><td>{v['wrong']}</td><td>{v['blank']}</td>"
                    f"<td>{pct(v['correct'], n)}</td><td>{pct(v['correct'], read)}</td><td>{pct(v['blank'], n)}</td></tr>")
    return "".join(rows)


def photo_samples(model_path: Path, cache: Path, rng: random.Random, px: int = 45, focus=("相模", "横浜", "湘南"), field_threshold=0.6):
    """Reader results on held-out photo plates shrunk to a dash-camera width."""
    sys.path.insert(0, str(Path(__file__).resolve().parent / "train"))
    import torch
    import plates
    import real_data
    import train_reader as tr
    device = "cuda" if torch.cuda.is_available() else "cpu"
    model = tr.Reader(plates.HEAD_SIZES).to(device)
    model.load_state_dict(torch.load(model_path, map_location=device))
    model.eval()
    real = real_data.RealPlates(cache, "val")
    cases = []
    for i in range(len(real)):
        small, t = real.render(i, random.Random(i), tr.IN_W, tr.IN_H, plate_px=px, augment=False)
        with torch.no_grad():
            logits = model(torch.from_numpy(small)[None, None].to(device).float() / 255)[0]
        text = {"region": "", "class": "", "kana": "", "number": ""}
        truth = {"region": "", "class": "", "kana": "", "number": ""}
        conf = {"region": 1.0, "class": 1.0, "kana": 1.0, "number": 1.0}
        known = {"region": True, "class": True, "kana": True, "number": True}
        offset = 0
        for k, ((field, labels), n) in enumerate(zip(plates.HEADS, plates.HEAD_SIZES)):
            p = torch.softmax(logits[offset:offset + n], 0)
            text[field] += labels[int(p.argmax())]
            conf[field] = min(conf[field], float(p.max()))
            if t[k] == real_data.IGNORE:
                known[field] = False
            else:
                truth[field] += labels[t[k]]
            offset += n
        for f in ("region", "class", "kana"):
            if conf[f] < field_threshold:
                text[f] = ""
        wrong = [f for f in text if known[f] and text[f] and text[f] != truth[f]]
        img = cv2.imdecode(real.crops[i], cv2.IMREAD_COLOR)
        q = real.quads[i].astype(int)
        cv2.polylines(img, [q.reshape(-1, 1, 2)], True, (0, 140, 255), 2)
        view = cv2.resize(cv2.cvtColor(small, cv2.COLOR_GRAY2BGR), (384, 192), interpolation=cv2.INTER_NEAREST)
        s = 384 / img.shape[1]
        cases.append({"engine": fmt_reading(text), "conf": conf["number"], "teacher": " ".join(truth[f] or "?" for f in ("region", "class", "kana", "number")) if known["number"] else "?",
                      "region": truth["region"] if known["region"] else "", "wrong": wrong,
                      "view": view, "orig": cv2.resize(img, (384, max(1, int(img.shape[0] * s))))})
    bad = [c for c in cases if c["wrong"]]
    good = [c for c in cases if not c["wrong"]]
    rng.shuffle(good)
    good.sort(key=lambda c: c["region"] not in focus)  # local regions first, otherwise random
    picked, seen, numbers = [], {}, set()
    for c in good:
        if c["teacher"] in numbers:  # several photos of the same car
            continue
        numbers.add(c["teacher"])
        if seen.get(c["region"], 0) < 4:
            picked.append(c)
            seen[c["region"]] = seen.get(c["region"], 0) + 1
        if len(picked) >= 15:
            break
    return [("写真の読取り例：誤りがあったもの（全件）", bad), ("写真の読取り例：正しく読めたもの（相模・横浜・湘南を優先、地名ごとに最大4件）", picked)]


def write_html(args, per_field, counts, kinds, reader_fields, recorded, photo_cards=()):
    e = html.escape
    head = "<tr><th>項目</th><th>件数</th><th>正しい</th><th>誤り</th><th>空欄</th><th>正答率（全件）</th><th>値を出した時の正答率</th><th>空欄の割合</th></tr>"
    reader_html = ""
    if reader_fields:
        rr = []
        for width, fields in reader_fields["byWidth"].items():
            cells = []
            for f in ("number", "region", "class", "kana"):
                v = fields[f]
                n = v["correct"] + v["wrong"] + v["blank"]
                cells.append(f"<td>{pct(v['correct'], n)}<br><small>読んだ時 {pct(v['correct'], v['correct'] + v['wrong'])}・空欄 {pct(v['blank'], n)}</small></td>")
            rr.append(f"<tr><th>{e(width)}</th>{''.join(cells)}</tr>")
        reader_html = f"""<h2>読取り器単体（学習に使っていない実写 {reader_fields['n']} 件）</h2>
<p>写真から切り出したナンバーを、ドラレコ相当の写り幅に縮めて読ませた結果。上段は全件に対する正答率、下段は「値を出した時の正答率」と「空欄にした割合」（信頼度が{reader_fields['fieldThreshold']}未満の項目は空欄にする設定）。</p>
<table><tr><th>写り幅</th><th>一連番号</th><th>地名</th><th>分類番号</th><th>ひらがな</th></tr>{''.join(rr)}</table>"""
    sections = []
    for k, items in kinds.items():
        title, desc = TITLES[k]
        cards = []
        for c in items:
            conf = "" if c["conf"] is None else f" ・ 信頼度 {c['conf']:.2f}"
            cards.append(f"""<figure><img src="{b64jpg(c['crop'])}" alt=""><img class="thumb" src="{b64jpg(c['thumb'], 70)}" alt="">
<figcaption><b>エンジン：</b>{e(c['engine'])}{conf}<br><b>Lipla：</b>{e(c['teacher'])}<br><small>写り幅 {c['width']}px ・ この番号で同じ種類の例 {c['frames']}フレーム ・ {e(c['file'])}</small></figcaption></figure>""")
        sections.append(f"<h2>{title}（全{counts[k]}から {len(items)}件、番号ごとに1枚）</h2><p>{desc}</p><div class=grid>{''.join(cards) or '<p>該当なし</p>'}</div>")
    photo_html = ""
    for title, items in photo_cards:
        cards = []
        for c in items:
            wrong = f"<br><b style='color:#b5400f'>誤り：</b>{e('・'.join({'region': '地名', 'class': '分類番号', 'kana': 'ひらがな', 'number': '一連番号'}[f] for f in c['wrong']))}" if c["wrong"] else ""
            cards.append(f"""<figure><img src="{b64jpg(c['view'])}" alt=""><img class="thumb" src="{b64jpg(c['orig'], 75)}" alt="">
<figcaption><b>エンジン（45px相当）：</b>{e(c['engine'])} ・ 信頼度 {c['conf']:.2f}<br><b>Lipla：</b>{e(c['teacher'])}{wrong}</figcaption></figure>""")
        photo_html += f"<h2>{title}（{len(items)}件）</h2><div class=grid>{''.join(cards) or '<p>該当なし</p>'}</div>"
    if photo_html:
        photo_html = "<p>あなたの写真のうち学習に使っていない検証分を、ドラレコで45px幅に写った状態まで縮めて読ませた例。上の画像が読取り器への入力（96×192のグレー）、下が元の写真の切り出し。「?」はLiplaが確信を持てず評価から除いた項目。</p>" + photo_html
    shots = "".join(f'<figure class="shot"><img src="data:image/png;base64,{base64.b64encode(Path(p).read_bytes()).decode()}" alt=""><figcaption>{e(Path(p).name)}</figcaption></figure>' for p in args.replay_shots if Path(p).exists())
    replay = ""
    if args.replay_summary and args.replay_summary.exists():
        replay = "<h2>ドラレコ映像の再生による通し検証</h2><pre>" + e(args.replay_summary.read_text(encoding="utf-8")) + "</pre>"
    doc = f"""<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>ナンバー認識 評価レポート</title><style>
body{{font-family:'Yu Gothic UI','Meiryo',sans-serif;margin:24px;max-width:1400px;color:#222;background:#fafaf7}}
h1{{font-size:22px}}h2{{font-size:18px;margin-top:32px;border-bottom:2px solid #ddd;padding-bottom:4px}}
table{{border-collapse:collapse;margin:8px 0}}th,td{{border:1px solid #ccc;padding:6px 10px;text-align:right}}th{{background:#eee}}
.grid{{display:grid;grid-template-columns:repeat(auto-fill,minmax(360px,1fr));gap:14px}}
figure{{margin:0;background:#fff;border:1px solid #ddd;border-radius:6px;padding:8px}}figure img{{width:100%;display:block}}
figure img.thumb{{margin-top:6px;opacity:.9}}figcaption{{font-size:13px;margin-top:6px;line-height:1.6}}
.shot img{{max-width:100%}}.note{{background:#fff7e0;border:1px solid #f0d890;padding:8px 12px;border-radius:6px}}
</style></head><body>
<h1>ナンバー認識 評価レポート</h1>
<p class="note">このレポートには他人のナンバーと位置が含まれます。ローカルでのみ参照し、共有しないでください。正解は Lipla-jp（先生役）のラベルで、Lipla自身の誤りや見落としを含みます。評価用のドラレコ映像は学習に一切使っていません。サンプル画像のオレンジ枠はエンジンの検出位置（見逃しはLiplaの位置）、小さい全体画像の緑枠は端末と同じROIです。</p>
{reader_html}
<h2>エンジン全体（評価用ドラレコ映像 1,303フレーム、ROI内でLiplaが確信を持って読んだナンバー）</h2>
<p>検出できたナンバーについて、項目ごとに「正しい／誤り／空欄」を数えた。地名・分類番号・ひらがなは、Liplaがその項目を確信を持って読んだものだけを数えている。</p>
<h3>検出できたもの全部（信頼度を問わない）</h3><table>{head}{field_rows(per_field)}</table>
<h3>実際に記録されるもの（読取りの信頼度0.6以上）</h3><p>サーバーは信頼度0.6未満の読みを記録しない。さらに、同じ番号を2回以上読めた車だけを記録する（この表には未反映）。</p><table>{head}{field_rows(recorded)}</table>
<p>件数（フレーム単位・番号の種類）：正しく読めた {counts['correct']} ・ 一連番号の誤読 {counts['number_wrong']} ・ その他項目の誤読 {counts['field_wrong']} ・ 見逃し {counts['missed']} ・ Liplaのラベルが無い場所での読み {counts['unmatched']}。同じ車が渋滞などで続けて写るとフレーム数が増えるため、サンプルは番号ごとに1枚にしている。</p>
{''.join(sections)}
{photo_html}
{replay}
{('<h2>画面（実サーバー、再生した映像の記録）</h2>' + shots) if shots else ''}
</body></html>"""
    (args.out / "index.html").write_text(doc, encoding="utf-8")


if __name__ == "__main__":
    main()
