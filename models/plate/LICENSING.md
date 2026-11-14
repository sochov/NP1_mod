# モデル別のライセンスと出所

この文書は、plate-v2（読取り器reader-v6b、検出器と設定はplate-v1bと同じ）とplate-v1bの重みと設定、Release `plate-models-v1b` の関連チェックポイントに適用する権利表示です。アプリケーション・学習スクリプトの独自コードは引き続き [MIT License](../../LICENSE) です。

## 対象ごとの扱い

| 対象 | 本プロジェクトが付与する許諾 | 上流の重み |
| --- | --- | --- |
| `ocr.tflite`（reader-v6b）、reader-v4 / reader-v5の `.pt`・ONNX | 本プロジェクトの独自成果物としてMIT | 独自の読取り器をランダム初期化して学習（reader-v6bはreader-v4からの追加学習）。ImageNet等の外部学習済み重みを読み込まない |
| `plate-model.json`、独自の変換・評価結果 | MIT | 設定・ラベル一覧・集計値。学習画像やフォントは含まない |
| `detector.tflite`、detector-v2の `.pt`・ONNX、旧package内の同系列検出器 | 本プロジェクトが権利を持つ追加・変更部分はMIT | ImageNet学習済みMobileNetV2の重みを初期値として使用 |

検出器のうち上流の重みに由来する部分には、上流の条件が適用されます。

## 検出器の出所

学習コード [`train_detector.py`](../../tools/plate/train/train_detector.py) は、次の重みを取得してMobileNetV2の特徴抽出層を初期化し、その後に本プロジェクトのデータで学習します。

- TorchVision識別子: `MobileNet_V2_Weights.IMAGENET1K_V1`
- 上流ファイル: `mobilenet_v2-b0353104.pth`
- 配布元: <https://download.pytorch.org/models/mobilenet_v2-b0353104.pth>
- 上流ファイルのSHA-256: `b03531047ffacf1e2488318dcd2aba1126cde36e3bfe1aa5cb07700aeeee9889`
- アーキテクチャと重みの対応: [TorchVision公式ソース](https://github.com/pytorch/vision/blob/main/torchvision/models/mobilenetv2.py)
- TorchVisionのコードのライセンス: BSD-3-Clause。[原文](notices/torchvision-BSD-3-Clause.txt)を保持する

TorchVision公式は、学習済みモデルには学習データ由来の別の利用条件があり得ると説明しています。[公式のモデル利用条件](https://github.com/pytorch/vision#pre-trained-model-license)と[上流での議論](https://github.com/pytorch/vision/issues/2597)を参照してください。

[ImageNetのアクセス条件](https://image-net.org/accessagreement)はデータベースの利用を対象とし、非商用の研究・教育目的という条件を含みます。

## 追加学習に用いた材料

- [Open Images V7](https://storage.googleapis.com/openimages/web/factsfigures_v7.html#licenses): 画像はCC BY 2.0、注釈はCC BY 4.0。検出器の追加学習に使用。画像・注釈アーカイブは同梱しない。
- 開発者が撮影した写真・ドラレコ映像: 読取り器・検出器の追加学習と評価に使用。元画像・動画・量子化用画像・位置情報は同梱しない。
- 本プロジェクトの合成画像: 読取り器・検出器の学習に使用。PCのフォントで描画し、フォント本体は同梱しない。
- [Lipla-jp](https://github.com/ikeboo/Lipla-jp): MIT。実写に付ける教師ラベルをPC上で生成する用途。Lipla-jpのコードや学習済み重みを、配布するモデルに組み込まない。

材料や学習ツールのライセンスと、学習済みモデルの許諾は同一とは限りません。この一覧は出所を示すもので、学習データの権利を本プロジェクトが再許諾するものではありません。

## 公開について

コードと独自の読取り器に対するMITの許諾を、検出器の第三者由来部分へ拡張しないでください。

確認日: 2026-11-12。現行モデルの識別ハッシュは [README](README.md) に記載しています。
