# 純正録画H.264の再利用経路

カメラのライブJPEGは純正の既存NV12 exportを読む。SD録画はH.264映像とAAC音声を持つMP4であり、JPEGしか存在しない機器ではない。確定済みMP4の取得・ブラウザー再生は既存のドラレコ再生で対応する。一方、書込中MP4のライブ化は別の処理で、確定ファイルのRange配信だけでは実現しない。

既に取得した実録画はH.264 1920×1080、AAC 48kHz stereo。MP4のbox順は `ftyp/free/mdat/moov` でmoovは末尾。書込中のmdatには映像/音声サンプルの境界・時刻・AVC設定を確定するindexがまだなく、そのままvideoへ渡しても再生可能なライブstreamとは限らない。新しいencoderを作成する代わりに、純正の圧縮済みデータと既存録画indexを再利用する方向で調査する。

## 既存encoder後段の静的根拠

固定DriveRec ELF SHA256は `000871dc36581f27ae831d9db3772b553a17cce0d814c52867276ac68ea0cde0`。以下はvendorコードを実行せず、既存ELFを逆アセンブルした結果。

| 処理 | VA | 確認内容 |
|---|---|---|
| 動画encoder生成 | `165b30` 内 `165fa8` | `NPMPS_CreateVideoEncodeStreamer` を呼ぶ既存純正構成 |
| 圧縮データ受取loop | `170c08` | 既存stream readerのvirtual+0x10からデータ/長さ/種別を受け取る |
| ring投入 | `170d30/170e2c` | 前方/車内を分けて `13c858` へ引き渡す |
| PushCamH264Data | `13c858` | timestamp、buffer pointer、長さ、開始点判定などを含む0x28byte recordを作る |
| ring前方 | `13ccc4..13cd44` | this+0x20/0x28の900枠ring。古いrecordとbufferはdeleteされる |
| ring車内 | `13caa4` 以降 | this+0x2400/0x2408等を使う別ring |
| SetH264StartingPoint | `13dbc8` | ring内の開始位置を選び、録画consumerへ供給する |
| MP4 container生成 | `149db4/14dd04/15a5ac/15ed2c` | `NPMultimediaContainer_CreateContainer` を呼ぶ複数の録画経路 |

したがって既存のH.264バッファ自体は純正プロセス内に存在する。ただしこれは公開共有mapではなく、bufferの寿命を純正ringが管理するprivate heapである。単に別プロセスからpointerを追って読むと、ring更新と解放の競合が起き得る。未確認のABI呼出しやこのheapの直接操作をライブ取得として実装しない。以下のSD書込済みデータを使う経路を実装した。

`libNDeviceLibAV` 内にもH.264/RTP/MP4のdebug pipelineがあるが、追加の `omxh264enc` を生成する経路は、SD録画と同じ既存圧縮データを再利用する要件を満たさない。既知のV4L2 encoder試験は停止済みで再実行しない。

## 書込中mdatの読取りと映像のみの再mux

`tools/inspect-native-recording.py --serial SERIAL` はSDのmetadataと直近6MP4の各先頭/末尾512byte・box headerだけを読み取る。実機で書込中2系統の増加を観測し、どちらも `ftyp/free/size-zero mdat`、直近確定4本は `ftyp/free/mdat/moov` だった。既知の `.moov/.idx/.index/.h264/.sps/.pps` は走査範囲内に見つからず、DriveRecのSD向けopen descriptorは2MP4だった。この結果を、未知形式の索引が絶対に存在しないという主張には広げない。

両mdatはAVCC長4byteの `AUD(len2/type9)→SPS→PPS→IDR` で始まる。独立package `backend/internal/h264tail` は、この既存NALを映像AU単位で検証してfMP4へ組み直す。**再エンコードはしない。初段は映像のみでAAC音声を含めない。**

- `ConfigFromMP4(reader,size)` は直近の同じカメラの確定moovからavcC・mdhd・sttsを取得する。mdat本体はseekで飛ばす。対応は確認済みBaseline・固定sample duration・composition offset無しだけ。未知のcodec/構造/時刻変動は拒否する。
- `NewDemuxer(config)` の `Feed(bytes)` は不完全末尾を保持する。AUDの固定長・NAL type・RBSP trailing bit、SPS/PPS完全一致、sliceのPPS ID/first_mb/frame_num/POCを検証し、SPS/PPS付きIDRからのみ出力する。frame欠落・構文不一致では次のIDRまで再同期する。AACらしい残余はvideo sampleへ含めない。単なる4byte長スキャンでpacketを採用しない。
- 純正writerの最終sliceにはAnnex-Bの末尾zero paddingが残る場合があった。構文検証では末尾の最大32byteだけを除き、内部の未escape start-codeは拒否する。出力byteには元のpaddingをそのまま残す。
- `Config.InitSegment()`、`Config.Fragment(sequence,baseDTS,samples)` はMSE向けvideo-only fMP4を生成する。公開Fragment側でもAU/flagsを再検証し、追加のAAC等が混じるSampleは拒否する。DTSは確定sttsの間隔で再構築し、元の可変PTSを取得したとは主張しない。
- `MediaDataRange` は確定後のmoovを除いた範囲を返す。size-zeroの書込中mdatでは観測時点のfile sizeまでを返す。呼出側はinode/同じSDの継続・ファイル切替・読取り位置・timeoutを管理し、ライブの未完AUを偽AUDで強制flushしない。
- 前方と車内でfpsが異なることを実測したため、片方の設定をもう片方へ流用しない。`Config.Codec()` で対応するMSE codec文字列を取得する。

hostテストは6件、加えてincremental入力のfuzz 4317件、Go vet、Linux ARM64 compileを合格した。別のhost検証では、既存privateの180秒録画から前方4860 AU・車内2880 AUを欠落0で抽出し、全packetのSHA256が元MP4と順序込み完全一致した。生成fMP4は両方ともffmpegで最後までdecodeし、error無し・映像trackだけであることを確認した。生映像・packet hashはignored tmpのみ。

```powershell
python tools/verify-h264-tail.py --recording PRIVATE_RECORDING.mp4
```

実機のHTTP経由では前方・車内のH.264を同時に約12秒取得し、両方1920×1080、純正26サービスのプロセス同一性維持を確認した。取得終了時に分断された末尾fragmentだけを除いた両映像は、ffmpegで最後までdecodeできた。検証ツールもこの末尾境界を処理するよう修正した。ブラウザーMSE再生・再生時刻の前進・SDファイル切替を含む長時間確認は未完了。

リアルタイムビューとドラレコ再生は共通の `CameraViewSwitch.vue` を使用し、前方・車内・並べて表示を切り替える。モバイルでは縦に並べる。JPEG fixtureでは320/390/1440pxの幅、残すカメラのセッション維持、停止時の解放を確認した。H.264のMSE表示にはCSPの `media-src 'self' blob:` が必要で、この変更の実機配備はチェックポイント時点では未実施。

## 起動待ちとの区別

2026-11-12に、利用者がカメラ関連アプリを起動した後も5アプリが1threadで開始通知待ちにあり、録画とNV12 exportが増えない事象を確認した。これはcodec/HTTP配信以前の純正開始barrierの問題である。正式なAppManagerのNotify経路を先に復旧し、その後に録画成長・export更新・H.264取得の各段階を別々に検証する。詳細は [stock-start-barrier.md](stock-start-barrier.md)。過去の画像をliveとして表示するためのstale判定解除は行わない。
