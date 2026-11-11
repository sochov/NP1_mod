# H.264エンコーダーの小規模検証

- `gst-inspect-1.0`でV4L2 H.264 encoderを確認。codec deviceは`/dev/video33`。camera/QMMF sourceとは別。
- `tools/device/encoder-smoke.sh`は合成のvideotestsrcだけを1つの追加codec contextで30フレーム符号化する。
- 保存先は揮発`/run/np1-encoder-smoke`だけ。SD、純正設定、カメラセッション、ALSAに書き込まない。
- 20秒timeout、異常時は自分のGStreamer子プロセスだけを停止。
- 純正DriveRec/ImageRecog/QMMFのPID/stateを前後で比較。H.264/fMP4をホストffprobeで直接検証する。
- 成功しても純正H.264圧縮バッファをそのまま共有できたことは意味しない。既存NV12からの独立hardware encode候補の検証。
- カメラ・音声・ネットワークの長時間共存は別途試験する。

## 実測と再試行停止

試験はV4L2 S_FMTがETIMEDOUTとなり失敗した。その後に実機の再起動を検知したが、因果関係や再起動原因の証拠は得られていない。純正カメラサービスは復帰後active。追加エンコーダー実験は停止し、再現スクリプトは先頭で拒否して保管する。原因が確認できるまで再試行しない。
