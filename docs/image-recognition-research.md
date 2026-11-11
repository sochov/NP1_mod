# 純正映像認識の構成とナンバー認識追加の可否（2026-11-12調査）

ADB接続中の実機を読取り専用で調べた（停止・設定変更・書込みなし）。バイナリの文字列解析には、ignored `tmp/imagerecog/` へpullしたコピーを使った。

## 純正ImageRecogの構成

| 要素 | 内容 |
|---|---|
| `app-@ImageRecog.service` | `/usr/local/bin/ImageRecog`（User=ImageRecog、Nice=8、RSS約210MB）。子プロセスとして`PIMG_Monitor`と`PIMG_Core`を起動 |
| `ImageRecog` | サービス本体。protobufによるアプリ間通知（`ImageRecognition.proto`）、音声通知（前車発進・信号変化・イベント録画）、HTTPクライアント（DemandCapture系、クラウドへの画像要求）、フレームexport |
| `libAIPI.so` | 名前は「AI」に見えるが、実体はアプリ間メッセージIF（`AIPI_*Event`）。推論ライブラリではない |
| `PIMG_Core` | 認識エンジン。`libPIMG_SharedLib.so`以外への依存はlibc/libstdc++のみ |
| 認識手法 | 自社製の古典的画像処理（`cvp`: Sobel、Gaussian、二値化、膨張・収縮、差分、ヒストグラム、色変換、間引き）。DNN、SNPE、TFLite、OpenCV、DSP、GPUは使っていない。モデルファイルも存在しない |
| 検出タスク | `TrafficSignalChangeDetection`（信号変化。赤信号位置の学習データあり）、`FrontCarStartingDetection`（前車発進）、`TailGating`（あおり・接近）、`HeadLightDetector`、`BreakLumpPairDetection`（ブレーキランプ対）、消失点、`RearWindow`学習（車内カメラ） |
| 結果出力 | `/tmp/application/imagerecog/result*.bin`、`*Log.bin`、`setting*.bin` |
| 画像export | `/tmp/application/imagerecog/imagedata/{frontcamera,incamera}/imagedata/value`。1920×1080 NV12。フロントは約7 fpsでatomic replace（5秒間でinode変化37回） |

## 利用可能な計算資源

| 資源 | 状況 |
|---|---|
| CPU | QCS610。onlineは6コア。計測時はidle約44%。DriveRecとqmmfがそれぞれ約84%（1コア換算）を使用 |
| メモリ | 総量2.6GB、available約1.2GB。swapなし |
| CDSP（Hexagon、HVX） | `cdsp ONLINE`。NN用途には誰も使っていない（`cdsprpcd`だけが常駐）。`/dsp/cdsp/fastrpc_shell_unsigned_3`があるため、unsigned PDで自前skelを使える可能性がある（未検証） |
| GPU（Adreno 612） | `/dev/kgsl-3d0`。常時使用しているプロセスは見当たらない |
| 推論ランタイム（rootfs既存） | `libtensorflow-lite.so`（XNNPACK、GPU、NNAPIのdelegate）、NNAPI HAL（`libunnhal-acc-hvx.so`、`libunnhal-acc-adreno.so`）、`libSNPE.so`、`libhexagon_nn_stub.so`、`/usr/lib/rfsa/adsp/libhexagon_nn_skel.so`、GStreamer `qtimletflite`、OpenCV 3.4.3、FastCV、`/usr/bin/label_image`、`benchmark_model` |
| SNPE DSPランタイム | `libSnpeDspV*Skel`は見当たらない。SNPEでDSPを使うには自前で用意し、unsigned PDで動作するか確認する必要がある |

## ナンバー認識追加の見立て

- 純正の認識エンジンへの追加は不可。閉じたバイナリであり、rootfsの変更も禁止されている。
- 独立サービスとしての追加は可能と見込む。既存の画像exportを読み、カメラ、QMMF、純正サービスには触れない。配置は既存ncc helperと同じく、`/overlay`（noexec）から`/run`へコピーして実行する。モデルは`/overlay`からデータとして読む。
- 構成案: (1) ナンバープレート検出（INT8の軽量detectorを縮小画像で動かす）、(2) 元解像度の切出し、(3) 日本のナンバー向けOCR（地名、分類番号、ひらがな、一連番号）、(4) 複数フレームの投票。
- 最大の制約は解像度。広角の1080pでは、プレート幅330mmに対して数m先でも数十pxにとどまる見込み。読取り対象は、信号待ちなどで停車中の直前車両の一連番号4桁が現実的な上限と想定する。対向車、隣車線、走行中、夜間の読取りは難しい。
- 録画MP4はH.264 1080pで圧縮されているため、OCRには不利。生のNV12 exportの方が良い。

## 推論速度の実測（2026-11-12、`tools/plate/bench_inference.py`）

端末の`/usr/bin/benchmark_model`で、TensorFlow公式の参照モデルを計測した。モデルはignored `tmp/plate/models/`に置き、端末側は`/tmp/np1-plate-bench`へ一時配置して計測後に削除した。

| モデル | CPU 4スレッド | XNNPACK 4 | GPU | NNAPI | Hexagon |
|---|---:|---:|---:|---:|---:|
| SSD MobileNet v1 量子化 300×300（検出） | 46.3ms | 57.5ms | 非対応（CPUへfallback） | 15.6ms | **13.4ms**（64ノード中61ノードをDSPへ委譲） |
| MobileNet v2 量子化 224×224（分類） | 31.7ms | 39.6ms | 非対応 | 9.6ms | **7.0ms** |
| MobileNet v2 float 224×224 | 90.6ms | 54.0ms | **25.7ms** | 45.2ms | 非対応 |

- Hexagon delegateは`--hexagon_lib_path=/usr/lib`と`ADSP_LIBRARY_PATH=/usr/lib/rfsa/adsp;/dsp/cdsp;/dsp`の指定で動作した。rootfsへの追加は不要だった。
- 60秒間の連続実行（Hexagon、平均13.3ms、約75fps）でも、benchmark_model自体のCPU使用は約1コアの30%にとどまった。DriveRec、qmmf、ImageRecog、Communication、Voiceは、PIDとNRestartsが不変のままactiveで、録画も継続した。
- `libtensorflow-lite.so`はHexagon delegateのC API（`TfLiteHexagonDelegateCreate`など）を公開している。一方、Interpreterは公開C APIがなくC++ APIだけなので、自作engineはTFLiteの版を合わせたC++ helperにする。
- 計測中にImageRecogが再起動していたが、原因は計測ではなかった。並行して動いていたNCC APIの`stock-service id=image-recog action=restart`による再起動である。ツールは計測期間中のこのような外部操作をjournalから抽出して記録する。

## 実機の素材の状況

端末のSDにある録画とイベント録画（2026-10-08〜11-11）は、すべて机上で撮影したボケた映像だった。GPSで測位できたファイルは302件中4件で、走行中のものは無かった。プレートが何pxで写るかの実測には、実車の前に車がある状態のフレームが必要になる（`tools/plate/capture_frame.py --serial SERIAL --count N`）。

## 要件（2026-11-12 利用者指定）

- Web UIから設定できること。
- 認識したナンバーと場所をすべて記録し、後から一覧で見られること。地図に点を打ち、一覧と連動させること。
- 条件（一連番号、分類番号、地名など）に合致したものは画像を保存し、地図上で目立たせること。
- 条件に合致したナンバーがいた場合は、本体の発話機能で発話すること。

## 実装状況（branch `feature/plate-recognition-poc`）

| 部分 | 状態 |
|---|---|
| `backend/internal/plate` | 実装・単体テスト済み。読取り結果の正規化（全角→半角、カタカナ→ひらがな、区切り文字の除去）、条件照合（`*`は1文字のワイルドカード、未読のフィールドは条件を満たさない）、設定の検証、同一車両の統合（既定30秒）、JST日別のappend-only JSONL、画像の保存、保持日数、画像容量上限 |
| `backend/internal/api/plate.go` | 実装・テスト済み。`GET /api/v1/plate/status`、`GET/PUT /api/v1/plate/settings`（expectedRevision）、`GET /api/v1/plate/records`、`GET /api/v1/plate/records/{id}/image`。センサー（`/run/np1-control-plate/sensor.sock`）から1秒ごとに取り込む。センサーの再起動はinstanceの変化で検出する |
| 発話 | 未対応として記録し、発話したことにはしない。純正には`AIPI_CVPRequestVoicePlayEvent`と`WEB_TTS`（クラウド）が存在するが、payloadは未解析。端末内のTTSエンジンは見つからなかった |
| センサープロセス（フレーム取得、推論、GNSS結合） | 未実装。protocolは`GET /status`、`GET /detections?after=`、`GET /frames/{id}` |
| 認識モデル | 未作成。日本のナンバー向けで再利用可能なライセンスの公開モデルは見つからなかった。合成データと実画像による学習が必要 |
| Web UI | 未着手。並行作業との衝突のため中断した |

## 次の検証（PoC）

1. 前車が停車している状況でexport画像を1枚取得する（ignored `tmp/`に限定）。プレートの実ピクセル幅を測り、読取り可能な距離を見積もる。
2. `benchmark_model`で軽量detectorのTFLiteモデルを測る。CPU（XNNPACK）、GPU delegate、NNAPI（HVX）を比較し、純正サービスの負荷とDriveRecのフレーム落ちが無いことを確認する。
3. 日本のナンバー向けOCRの学習データと手法を選ぶ。
4. ナンバーは位置・時刻と結び付くと個人に関わる情報になり得る。結果は端末内に保持し、外部送信と保存期間を明示的に設定する。
