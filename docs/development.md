# 開発・検証

## ディレクトリ

| パス | 内容 |
| --- | --- |
| `frontend/` | Vue / TypeScript UIとPlaywrightテスト |
| `backend/` | GoのWeb / broker / worker、単体テスト |
| `systemd/` | 配備する専用unit |
| `tools/` | ビルド・配備・検証、`device/` の実機スクリプト、`native/` の独自ヘルパー |
| `tests/` | ホスト側Pythonテスト |
| `docs/` | 公開用の利用・構成・API文書 |
| `licenses/` | OSSの原文・NOTICEと固定依存の一覧 |
| `models/plate/` | ナンバー認識の学習済みモデル（配備物に含める） |
| `prebuilt/plate-engine/` | ナンバー認識エンジンのビルド済みaarch64バイナリと、静的リンクしたOSSの原文 |
| `artifacts/` | ビルド成果物。Git対象外 |
| `resources/` | 取得資料や端末由来のリンク材料。Git対象外 |
| `tmp/` / `docs_private/` | 生データ、一時出力、内部の作業・調査記録。Git対象外 |

## ホストでの確認

GitHub ActionsではUbuntu 24.04、Go 1.27.4、Node.js 24、Python 3.11でビルド・Go/Pythonテスト・ARM64クロスビルドと公開検査を実行します。解析用テストも実行するため、protobuf 5.29.1をCIに導入します。シェルの検証にはBashとPowerShell（Linuxでは`pwsh`）を使用し、CIの開始時に実行環境を確認します。権限を検査するテストの一時保存先は、ホストのumaskに依存せず0700で用意します。

```powershell
cd frontend
npm ci
npm run build
npx playwright install chromium
npm run test:e2e
cd ../backend
go test ./...
go vet ./...
cd ..
python -m unittest discover -s tests -p "test_*.py" -v
python tools/third-party-licenses.py
```

Playwrightのfixture試験にはローカルのUIが必要です。別ターミナルで `cd frontend` → `npm run dev -- --port 5177` を起動し、試験側の `NCC_BASE_URL` と `NCC_UI_REVIEW_URL` を `http://127.0.0.1:5177/` に設定してください。実機の映像や変更操作を伴う試験は環境変数で明示的に有効化する方式です。各specの先頭で必要条件を確認してください。

GNSSの期限付き診断は `python tools/inspect-gnss.py --help` を参照してください。位置や識別子を含む詳細出力はGit対象外のディレクトリへ保存します。実機で値が得られない機能を、fixtureの合格だけで実機確認済みにはしません。

通常のARM64配備物は `tools/build.ps1` または `tools/build.sh` で生成します。追加の純正ヘルパーを必要とする場合は [導入手順](installation.md) を参照してください。

## ナンバー認識

| 対象 | 手順 |
| --- | --- |
| 推論エンジン | `bash tools/plate/build_engine.sh [aarch64\|host\|all]`（WSL / Linux x86_64、Apple Silicon macOSでは `aarch64` のみ）。zig 0.13.0でglibc 2.28向けにTensorFlow Lite 2.16.1とXNNPACKをビルドし、`artifacts/plate/` へ出力します。`tools/build.*` は `artifacts/plate/plate-engine-aarch64` があればそれを、無ければ [`prebuilt/plate-engine/`](../prebuilt/plate-engine/) を配備物に入れます |
| 学習 | `tools/plate/train/`。合成（`plates.py`）、教師ラベル付け（`lipla_label.py`、Lipla-jpをPCで実行）、読取り器（`train_reader.py`）、検出器（`train_detector.py`）、TFLite変換（`convert_tflite.py`）。学習データは各自で用意し、`NP1_PLATE_DATA` または `--data` で指定します |
| 評価 | `tools/plate/eval_engine.py`（エンジン全体）、`tools/plate/train/eval_reader_fields.py`（項目別）、`tools/plate/make_report.py`（画像付きのローカルレポート。他人のナンバーを含むため、Git外に出力） |
| 映像での評価 | 手順と考え方は [ナンバー認識の評価方法](plate-evaluation.md)。録画を `ffmpeg -vf fps=2` で `<クリップ>/f_%05d.jpg` に切り出し、`tools/plate/eval_video.py run` でエンジンに流します。`score` は教師ラベルとの一致と、本体の記録規則（2回検出・1回で記録する信頼度など）を再現した記録単位の指標を出します。教師も誤るため、最終的な比較は目視の正解で行います：`tools/plate/gt_candidates.py` が読まれた番号を時間ごとにまとめて確認用の画像を作り、`verdicts.json` に正誤を書くと、`tools/plate/gt_score.py` がモデル・確定方式ごとに採点します。`tools/plate/review_records.py` は記録の確認用の画像を作ります（いずれも他人のナンバーを含むため、Git外に出力） |
| 学習の補助 | `tools/plate/train/lipla_label.py --skip/--only`（ラベル済みの画像を飛ばす・対象のフォルダを絞る）、`tools/plate/train/calibrate_reader.py`（読取り器の信頼度を温度で校正）、`tools/plate/train/convert_tflite.py --reuse-detector`（検出器を作り直さずにパッケージを作る） |
| PCでの通し検証 | `ncc --mode plate-replay` に録画済み映像のマニフェスト（`tools/plate/make_replay_manifest.py`）を渡すと、本物の認識プロセス・エンジン・Webに映像を流せます |
| 実機 | `tools/device/bench-plate-engine.py`（同一フレームでのCPU・GPU比較）、`tools/plate/bench_inference.py`（純正ランタイムでの単体モデル計測）、`tools/device/plate-speech-play.py`（本体スピーカーでの再生確認） |

走行試験では、認識エンジンの配備時に `tools/device/deploy-plate-engine.py --fps 8` で画像取得の目標を設定できます。保存されるFPSはセンサーが最後の約10秒間に実際に処理したフレームから算出します。

画面の端へ斜めに加速する車のプレートは、通常の位置割当で追跡が切れる場合があります。連続3回の検出枠から直近の移動を撮影間隔で補正し、既存トラックと次の枠の候補が双方で一つに定まる場合に位置をつなぎます。番号の登録には通常どおり複数フレームの読取根拠や矛盾の有無を使います。

Web/収集ワーカーに `NCC_PLATE_IMAGE_DIR` を設定すると、採用したナンバー記録ごとに代表の前方画像をそのディレクトリに保存し、ナンバーの一覧から開けます。SDを利用する場合は、書き込み可能な専用ディレクトリを先に用意します。これまでの内蔵画像も一覧から参照でき、画像の保存上限と保持日数は両方に適用します。未設定時は、保存を指定した一致ルールの画像を従来の内蔵ディレクトリに保存します。

`NCC_PLATE_DIAGNOSTICS=1` は走行試験用です。`state/plate/health/<UTC日付>.jsonl` に10秒ごとにFPS、画像の更新時刻、認識エンジン、保存・見送り件数、画像取得結果、収集エラーを記録します。ナンバーの文字列や位置は診断ログに書きません。通常運用は `NCC_PLATE_DIAGNOSTICS=0` または未設定で、従来の毎分のヘルスログを記録します。いずれも保持日数は7日です。

学習途中のチェックポイント（PyTorch `.pt`・ONNX・評価結果）、x86_64版エンジン、モデル一式のzipは、GitHubのRelease `plate-models-v1b` に置いています。量子化に使った画像（`calibration.npy`）と学習データは、他人のナンバーを含むため配布しません。

Pythonのテスト（`tests/test_plate_labels.py`）は、OpenCV・NumPy・Pillowが無い環境でも動作します。合成画像の描画を使う場合だけ、これらのパッケージを導入してください。

### textOcr設定

`plate-model.json` の `textOcr` には、次の任意キーを追加できます。省略時は従来の読取りを維持します。矩形は検出枠に対する `[左, 上, 右, 下]` の比率です。

| キー | 内容 |
| --- | --- |
| `numberModel` | 一連番号の確認専用の任意モデル。`file`、`sha256`、`dictionary`、`dictionarySha256` を指定し、ファイルはモデルディレクトリからの相対パスで読み込み、SHA-256を検証します。`fastFile` と `fastSha256` でINT8モデルを指定でき、`fastMaxInputWidth`（既定512、64〜512）で入力幅を制限します。初期化または推論に失敗した場合は通常モデルにフォールバックします。`NCC_PLATE_PPOCR=original` ではINT8を使いません。`sampling` は `"nearest"`（既定）または `"bilinear"`。省略時は通常の認識器を使用します |
| `padWidths` | 昇順の正整数配列（例 `[128, 256, 384]`）。切り出し幅以上の最小幅まで右を0で埋め、最大幅を超える場合は最大幅に縮小します。有効な画像幅に対応するCTCステップだけを使います。省略時は従来どおり可変幅です |
| `gpu` | `padWidths` が必要です。`models` に `width`、モデルディレクトリからの相対 `file`、`sha256` を指定し、任意の `precisionLossAllowed` で精度低下を許可します（既定 `false`）。GPU対応ビルドでは各幅のTFLiteモデルをOpenCLで実行し、未指定幅やdelegate初期化失敗時はONNX CPU、delegate実行失敗時はTFLite CPUを使用します。番号モデルにも独立して `padWidths` と `gpu` を指定できます |
| `numberCheck` | `false` でPP-OCRによる一連番号の確認を省き、9ヘッド読取り器の番号と信頼度を使用します（既定 `true`） |
| `numberScore` | 一連番号を確認する2領域それぞれのOCRスコア下限（既定0.98） |
| `numberOverrideConfidence` | PP-OCRで番号を置換したときの信頼度の上限（0〜1、既定1で従来どおり）。instant閾値より小さくすると置換だけでは即時記録されません |
| `numberVetoScore` | `numberScore` 未満でも、同じ桁数のPP-OCR番号が元番号と異なる場合に信頼度を0にするスコア下限（0〜`numberScore`、既定は `numberScore`）。2領域による確認・置換の閾値は変更しません |
| `numberOverride` | `false` で低信頼の一連番号をOCR結果で置き換える処理を無効にします。高信頼の番号の確認・置換は維持します（既定 `true`） |
| `sampling` | `"bilinear"` でRGBの双線形補間を使用し、矩形が検出枠を越える場合も画像端で制限します。省略または `"nearest"` は従来の最近傍です |
| `kanaRects` | かなのOCR領域の矩形配列。省略時は従来の3領域を使用します |
| `topRow.rects` | 上段全体を読む矩形配列。`topRow` を指定すると上段読取りを有効にします |
| `topRow.posterior` / `gap` / `maxCostPerCharacter` | 地名候補の事後確率の下限（既定0.9）、挿入・削除コスト（4）、1文字当たりの平均コスト上限（4）。上段の地名と従来の地名が食い違う場合は空欄にします |
| `topRow.classMinimum` / `classShare` / `fillClass` | 分類番号の全領域当たり得票下限（既定0.6）、有効票中の得票割合下限（0.8）、未読の分類番号を補完するか（true） |
| `topRow.classOverride` | 有効な分類番号を読んだ全領域で結果が一致し、全領域当たり得票が指定値以上の場合、従来の分類番号を上段の読取りで置き換えます。省略時は無効です |

例えば `topRow.rects` に `[[0.02, -0.02, 0.98, 0.40], [0.05, 0, 0.95, 0.37]]`、`kanaRects` に `[[0, 0.45, 0.23, 0.90], [0.02, 0.50, 0.21, 0.86]]` を指定できます。枠外を含む矩形には `sampling: "bilinear"` を使用します。

`ocr.numericVerifier.retryExpandedLocalized` は、`retryLocalized` を有効にした構成で使う任意設定です（既定 `false`）。狭い検出枠で4桁番号の主OCRと照合が強く一致し、桁位置OCRの裏づけが足りない場合に、最後の再試行を少し広い切り出しに切り替えます。再試行の上限は1枠4回です。番号の候補、追跡に使う検出枠と他の項目は元の読みから取得します。

`ocr.rereadShrinkingDigits` は、数字照合モデルを使う任意設定です（既定 `false`）。35〜44 pxの検出枠で4桁が主OCR、照合OCR、数字CTC、桁位置OCRで一致した場合に、画面中央へ向かう同じ軌跡上の32〜34 pxの検出枠を後続2フレームまで読みます。後続2枚でも番号が一致したとき、追跡結果を保存できます。

`ocr.requireDigitDetail` は、数字照合モデルを使う任意設定です（既定 `false`）。32〜55 pxの枠で4桁を読んだ場合に、下段の隣接画素の輝度差を測ります。同じ番号を支持するフレームのうち、下段の輪郭が閾値を満たす枠か、さらに幅の大きい枠を1枚確認してから保存します。

独立評価の集計では `tools/plate/quality_gate.py` に、目視参照と採点行を渡します。各版の再生出力を採点行と同じ順に `--records` で指定すると、判定保留の行を含めて全保存のクリップ・ID・一連番号を照合します。同じ車が複数の切り出しにまたがるときは、採点行の `originalClip` に保存元のクリップを記録します。

## 公開前の確認

```powershell
python tools/check-public-tree.py
python tools/third-party-licenses.py
python tools/check-doc-links.py
git diff --check
```

秘密値との照合には `tools/check-public-tree.py --known-private-values PATH` を使用できます。指定ファイルはGit対象外に置くJSON文字列配列で、検査結果に値そのものを表示しません。この検査は既知の値と公開対象パスを確認するもので、未知の秘密情報まで自動で保証するものではありません。

変更をstageした後に `python tools/verify-public-build.py --go PATH_TO_GO` を実行すると、Git indexだけを隔離ディレクトリへ展開し、端末由来のファイルや非公開資料なしで通常ビルドできることを確認します。公開スクリーンショットは `frontend/tests/readme-screenshot.spec.ts` の架空データから生成しています。

READMEの画面例はv0.4.0のダーク／ライト表示へ更新済みです。生成には実機と異なるポートのローカルViteを使用し、全APIをサンプルデータへ置き換えます。経路は [公開道路のfixture](../frontend/tests/fixtures/public-tokyo-route.json) を使用します。東京駅周辺のOpenStreetMap道路データを [OSRM Route API](https://project-osrm.org/docs/v5.24.0/api/#route-service) の `overview=full` で取得した120頂点を保持し、時刻は架空です。出典・取得URL・[OpenStreetMapの帰属表示](https://www.openstreetmap.org/copyright) はfixtureにも記録しています。

撮影時は地図をzoom 14へ1段階引き、移動後のidleとタイル描画完了を待ちます。MapLibreのベクトル地図・全経路頂点・7日選択・縮尺を検査してから撮影します。ナイト配色は道路・建物・文字の明るさと彩度を抑え、走行軌跡が前景として見えるよう調整しています。カメラ枠も既存の合成JPEGで、実機の映像や走行位置は使いません。

撮り直す場合は `frontend` で `npm run dev -- --port 5177` を起動し、別のPowerShellで次を実行します。ChromiumはPlaywrightで事前にインストールしてください。

```powershell
cd frontend
$env:NCC_PUBLIC_SCREENSHOTS='1'
$env:NCC_UI_REVIEW_URL='http://127.0.0.1:5177'
foreach ($theme in 'dark','light') {
    $env:NCC_PUBLIC_THEME=$theme
    npx playwright test readme-screenshot.spec.ts --output "../tmp/readme-screenshots-$theme"
    if ($LASTEXITCODE -ne 0) { throw 'Screenshot verification failed' }
}
```

`tmp/readme-screenshots-{dark,light}` 内の `dashboard-{dark,light}.png` を目視確認してから、同名の `docs/images/` の画像へコピーします。今回の両テーマは1440×1000で検証済みです。

依存関係の変更時には [サードパーティライセンス](../THIRD_PARTY_LICENSES.md) の手順で一覧と原文を更新してください。純正ファームウェア、ライブラリ、SDKヘッダー、逆コンパイル結果、実機録画・位置、資格情報は追加しません。Issueには再現手順・ビルド識別子・公開可能なエラーの要約を記載してください。
