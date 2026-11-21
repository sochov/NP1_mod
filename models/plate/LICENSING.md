# NumberShare v4 同梱物の出所

- detector.tflite: NP1_mod由来。上流の重みと学習材料の権利表示はUPSTREAM_LICENSING.mdをそのまま保持する。
- ocr.tflite / ocr-kana.tflite / ocr-classification.tflite: NP1_mod readerを追加学習。MITコード・公開画像dyama/alpr_jpのMIT原文を保持。
- ppocrv5_r2_rec.onnx / ppocrv5_r2-dictionary.json / ppocrv5_r2_w128.tflite / ppocrv5_r2_w256.tflite: PaddlePaddle/PP-OCRv5_mobile_rec_onnx（Hugging Face、Apache-2.0、原文 notices/PP-OCRv5-Apache-2.0.txt）を改変・追加学習したもの。出力を273文字に限定し、所有者の写真・ドラレコ映像から作った切り出し（同梱しない）と合成画像で、小さく写った状態を学習。TFLite版は同じ重みを固定幅に変換したもの（tools/plate/gpu/）。
- ppocrv6_rec.onnx / ppocr-dictionary.json: bukuroo/Lipla-jp、固定revision 947afab7678ff50d7101e5cf802de25e9d4d0fe1。モデルカードはMIT表記、原文PPOCR_MODEL_CARD.mdとLIPLA_MIT_LICENSE.txtを保持。
- ONNX Runtime Android 1.31.1: Microsoft、MIT、ONNX_RUNTIME_LICENSE.txt。モデルとは別の推論ランタイム。
- 動画追加学習: star ceders「青山六本木4K」 https://www.youtube.com/watch?v=xyBgpYw2uQs 、取得時Creative Commons Attribution (reuse allowed)表記を確認。2フレームを切り出して地名・かなの一部に確認ラベルを付与。
- 評価動画: star ceders「皇居一周4K」 https://www.youtube.com/watch?v=WDuJABJWcXU 、同じくCC BY表記を保存。評価用の車両例は学習から除外。

元動画・教師ラベル候補・拡張特徴キャッシュはアプリへ同梱しない。各動画のクレジット、時刻、URL、ライセンス記録はml/data/driving-videos/*.source.jsonと各切り出しmanifest.jsonに保持。
UPSTREAM_LICENSING.mdの「Liplaモデルを同梱しない」は元NP1パッケージの説明であり、本NumberShare v4は上記PP-OCRモデルを追加同梱する。
