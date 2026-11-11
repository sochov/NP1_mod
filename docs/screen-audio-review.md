# 画面連動LED・共有音声の独立レビュー

2026-11-11。ホストの実装・単体試験、ブラウザの合成入力による試験、実機でのAPI/サービス確認、物理的な見え方・聴感を分けて記録します。

## ホストのコード・単体試験

`internal/audio/{stream,http}.go`、`internal/led/managed.go`を別担当の視点でレビューしました。画面連動の5RGBは保存LED設定の明るさを二重適用せず、期限切れ・削除・不正RAMフレームで保存済みの通常設定に戻す実装です。通常設定の保存・revisionの変更と、画面連動の一時フレームは分かれています。

| 発見した条件 | 補強・確認 |
| --- | --- |
| 音声Writeがctxを監視しない場合、ctx cancelだけではWriteが戻らず、その後のCloseへ進めない | 独立watchdogが出力Closeを一度だけ実行し、pipeを解除する。明示Stop・idle・Manager.Closeの3経路を非協調fake pipeで試験 |
| Stop成功の直後にも旧writer/watchdogが残り得る | 両goroutineをjoinしてからStopを成功応答する。停止確認は最大1秒、未確認なら503。旧出力の終了を確認するまで新規再生を重ねない |
| 音声HTTPの重複keyやDELETE body/queryが許容されていた | JSONの重複・未知key、null、小数、不正base64、JS safe integer超過、サイズ上限、DELETE body/queryを拒否する |
| 壁時計が戻ると古いleaseが延命される | 音声・画面とも更新時刻が未来なら無効。TTL境界で失効する |
| worker終了状態に旧screen RGB/sequenceが残る | 最終off状態でZones/ScreenSequenceをクリアする |
| 遅い音声出力への入力が溜まる | serverは最大3×9600byte、clientは最新200msを1件だけ保持。古い待機音声を捨て、呼出元のbyte配列もコピーする |

`go test`と`go vet`をaudio/led/adapters/api/brokerで実行し合格。固定PulseAudioコマンドは`/usr/bin/pacat`、`--device=primary`、`--server=unix:/run/pulse/native`、mono/24kHz/PCM16LE、専用ストリームの`--volume=16384`（通常65536に対する25%）です。システムのsink音量や既定sinkを変更するコマンド、マイク入力、音声ファイル保存はありません。HTTP監査は開始・停止のメタデータだけで、PCM/RGB・session IDを記録しません。

## ブラウザの合成入力

実デスクトップやマイクを取得せず、canvas.captureStreamとoscillatorを使用しています。実AudioWorkletで48kHz→24kHzのmono変換、44.1kHzの分数レート、処理ブロックの連続性、PCM16LEの形式、clamp、相殺無音を検証しました。CSP script-src selfの下で独立した同一OriginのWorklet assetを読み込めることも確認しています。

画面選択のキャンセル・再試行、単一送信と最新frame優先、通信エラー・1秒timeout、hidden継続、共有終了、pagehide、画面遷移、mouse/touch、音声なしの理由表示、音声障害時のLED継続を確認しました。従来画面を含む37件の回帰結果と画像は`tmp/screen-audio-browser-37.log`と`tmp/verified-screen-audio-browser/`へ保存しました。最新previewの画面試験では`NCC_UI_REVIEW_URL`を優先し、音声UI配備前の実機を誤って試験先にしないようにしています。

## 実機で確認された範囲

親担当が`tools/verify-screen-led.py`を実行し、5RGBの受理・worker command stateの一致、12frame、1500ms無更新による通常設定への復帰、明示Stop、保存設定不変を確認しました。これはcommand stateまでの確認であり、カメラによるLEDの色の光学的read-backではありません。

親担当が`tools/verify-browser-audio.py`を実行し、合成quiet toneのPCMを12chunk/2.4秒送信、PulseAudio primaryの専用ストリーム観測、2秒idleによる終了と失効後410を確認しました。これはPulseAudio出力までの確認です。スピーカーからの聴感、音量の適切さ、遅延・音質は人による聴取が未実施です。OS・ブラウザごとに画面共有できる音声の種類も別途確認が必要です。

独立レビュー後のpipe終了・HTTP厳格化はホストで再検証しており、実機へ更新する場合は上記スクリプトで再確認します。実機試験の生情報・画面は`tmp/`にのみ置き、この記録へ個体情報を転記しません。
