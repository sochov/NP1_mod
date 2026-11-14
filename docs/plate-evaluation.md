# ナンバー認識の評価方法

ナンバー認識のモデルや記録の規則を、走行映像で比べるための手順です。2026-11-14にplate-v2を選んだときの評価（[モデルの説明](../models/plate/README.md#精度学習に使っていない映像目視で確認した正解)）は、この手順で行いました。

評価に使う映像・フレーム・正解表には他人のナンバーが写るため、リポジトリには入れません。各自の環境に「評価キット」として置き、外部に出さないでください。

## 考え方

- **学習に使っていない映像で評価します。** 録画を2フレーム/秒（本体の処理と同じ間隔）で切り出し、本体と同じ推論エンジン（PC版）に流します。
- **正解は目視で付けます。** 教師モデル（Lipla-jp）は夜間の黄色ナンバーなどで誤ることがあり、1台が長く写ると一致率が大きく偏ります。そこで、どれかのモデルが読んだ番号を「番号＋時間（60秒）」でまとめた候補にし、代表の写りを人が見て `ok`（正しい）／`bad`（誤り）／`unk`（判読できない）に分けます。
- **二つの単位で比べます。**
  - 1フレームの読み（ワンショット）：エンジンが1フレームで出した読みの正答率を、信頼度・写り幅のしきい値ごとに数えます。
  - 記録：本体の記録規則（読み取り可能・信頼度0.6以上・同じ番号を30秒以内は同じ車）を再現し、確定方式（2回検出、1回でも記録、高信頼なら1回など）ごとに、記録の正答率と正しく記録できた車の数を数えます。
- 正答率は ok／（ok＋bad）で、`unk` は数えません。「車」はモデル間の比較用で、どのモデルも読まなかった車は含まれません。

## キットの構成

```text
<キット>/
├─ datasets/<名前>/frames/<クリップ>/f_00001.jpg …   評価用フレーム
├─ datasets/<名前>/sources.json                      元動画と切り出しコマンド
├─ datasets/<名前>/gt/candidates.json, verdicts.json  候補と目視の判定
├─ engine-outputs/<名前>/<モデル>.jsonl              エンジン出力
├─ packages/<モデル>/                                detector.tflite・ocr.tflite・plate-model.json
└─ results/report.md, report.json                    採点結果
```

## 手順

| 段階 | コマンド |
| --- | --- |
| フレームの切り出し | `ffmpeg -i <動画> -map 0:v:0 -vf "fps=2" -q:v 2 frames/<クリップ>/f_%05d.jpg`（クリップ名に `_YYMMDD_HHMMSS` を含めると、時刻を比べられます。1080pでない映像は `scale=1920:1080:flags=area` を追加） |
| エンジンに流す | `python tools/plate/eval_video.py run --frames <frames> --package <packages/モデル> --out <engine-outputs/名前/モデル.jsonl>`（x86_64版エンジン `artifacts/plate/plate-engine-host` をWSLで実行。`bash tools/plate/build_engine.sh host` で作成） |
| 候補と確認用画像 | `python tools/plate/gt_candidates.py --frames <frames> --teacher <教師ラベル。無ければ存在しないパス> --engine <engine-outputs/名前/*.jsonl> --out <gt> --audit`。新しい候補は `todo-*.jpg`／`todo.json`、全候補と判定は `audit-*.jpg` |
| 判定 | `todo-*.jpg` を見て、`gt/verdicts.json` に `{"<id>": {"verdict": "ok", "number": "1234"}}` の形で追加 |
| 採点 | `python tools/plate/gt_report.py --kit <キット> --models v1b v6b`（表を `results/report.md` に出力）。1データだけなら `tools/plate/gt_score.py` |

そのほか、`tools/plate/eval_video.py score` は教師ラベルとの一致と、記録規則の再現による指標を出します（参考用）。`tools/plate/review_records.py` は、記録だけを確認用の画像にします。

## 判定のこつ

- 4桁（「・」は空き桁）が一致すれば `ok`。分類番号・地名は判定に含めません。
- 暗い・小さい写りで、同じ車に矛盾する読みが並ぶときは、`audit-*.jpg` から同じ車がはっきり写った候補を探して正解の番号を決め、それ以外を `bad` にします。それでも決められなければ `unk` にします。
- 看板・建物・道路標示などの誤検出は `bad` です。
