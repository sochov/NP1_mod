# QMMF単独終了による本体再起動の静的調査

2026-11-12。WebのQMMF終了後に本体が再起動した報告を受け、既存unit・ELF・終了hookを読取りで調査した。この調査では実機の停止、再現操作、設定変更、watchdog解除を行っていない。主要資料はignored `resources/stock-service-inventory/`、実ログは実ログ担当が保持するignored `tmp/qmmf-stop-evidence/` に置く。

## 結論と適用範囲

QMMFを使っている純正映像処理が生きたままのQMMF単独終了は安全な個別操作として扱えない。`ignore-dependencies` はsystemdの依存ジョブを抑止するが、Binder切断通知・GStreamerの再生成処理・純正アプリ自身の再起動方針を抑止しない。

今回の保存ログ3件ではQMMF停止後の `NPVINDeviceCont` SIGSEGV、実体 `/usr/local/bin/NEventWatcherAV`、corefilterの本体再起動経路が一致した（実ログ担当の解析）。静的ELFにも接続失敗→NULL contextでGObject生成が戻る経路がある。これが今回の主経路を説明する。カメラhardware watchdogによるrebootを示す証拠は、この3件では確認していない。

DriveRecにはカメラ異常時に直接 `NSystem::Reset` を依頼する別経路もある。今回の3件でこのResetを発火原因と断定しない。SIGSEGVだけを回避すれば全てのカメラ依存が安全になる、とも扱わない。

## unitと終了hook

保存済み実機 `qmmf-server.service` は `ExecStart=/usr/bin/qmmf-server`、`Restart=always`、`KillMode=control-group`、`KillSignal=15`、`SendSIGKILL=yes`、`TimeoutStopUSec=90s`。`Requires=servicemanager.service` で、reboot用 `ExecStop` / `ExecStopPost` / `OnFailure` は存在しない。QMMFの `RequiredBy` / `BoundBy` / `ConsistsOf` も空だった。

従ってQMMFのunit定義自体に本体再起動コマンドがあるわけではない。純正 `corefilter.sh` の末尾は、異常プロセスの保存cmdlineが `/usr/local/bin/` に一致すると純正プロセスへTERMを送り、`reboot -f` を実行する。正常なQMMFのTERMだけでなく、その後に別の純正consumerがcoredumpすると本体再起動になる。

`signalhandler_app.sh` / `signalhandler_lib.sh` にも異常signal後の `reboot -f` があるが、通常TERMはこの分岐の対象外。SEGV/ABRTのcoredumpはcorefilter側と区別する。どのhookも無効化・改変する案は採らない。

## ELFで確認した経路

以下の住所はこの資料のSHA-256に対応するELFのVAで、実行時のASLR後の絶対アドレスではない。

| 対象 | 静的根拠 |
|---|---|
| `libqmmf_recorder_client.so` | `RecorderClient::ServiceDeathHandler` は `0x1a580`。各種buffer等の解放後、`0x1a93c` 以降で登録されたイベントcallbackへ通知する。 |
| `libgstqtiqmmfsrc.so` | Recorder callback `0x107d0` はevent値1で `Camera server has died !` を出力する。`0x10938` 以降のEOS送信はcontext/session状態に依存するため、ログ行だけで全pipelineが正常NULLへ遷移したとは判断できない。 |
| 同plugin | `gst_qmmf_context_new` は `0x11fe0`。`0x1207c` の `Recorder::Connect` が失敗すると `0x120d8..0x12100` でRecorder/contextを解放しNULLを返す。 |
| 同plugin | `qmmfsrc_init` は `0x9970`。`0x99e0` で上記関数を呼び、`0x99e4` でcontextをobject+`0x130`へ格納。NULLなら `0x9a04..0x9a20` で `g_return_if_fail_warning` に進み、contextの無いGObjectが戻り得る。保存ログの `qmmfsrc->context != NULL` assertionと一致する。SIGSEGV命令そのものまで一致させた証拠とは区別する。 |
| `NEventWatcherAV` | camera pipeline構築は `0x284e8`。既存qtiqmmfsrcのunref (`0x28540`) と再構築 (`qtiqmmfsrc name=%s`) を持ち、その後にcamera/SHDR等のpropertyを設定する。`onBusError` 相当 `0x290b0` はNPVIN状態イベントを作成する。 |
| `DriveRec` | `CDVR_SRV_REC_FunctionBaseState::Reset` は `0xf45f8`、`0xf4658` から `NSystem::Reset`。SecurityRecordingState camera状態処理 `0xe1cb8` は共通状態判定 `0xf0678` が失敗すると `0xe1d88` でResetを依頼する。mode3では `Camera Error no Reset. because RemoteMonitoring mode` を出して除外する。 |
| `libNDeviceLibSys.so` | `NSystem::Reset` (`0x2e658`) は既存 `NSystemHandlerProxy::Reset` (`0x2f0f0`) へ委譲し、接続済みproxyへ固定command3を依頼する。ここで本体resetの依頼が純正システム所有者へ渡る。 |

## 連動停止案の境界

手元の純正ELFで `NPVINClient` のconstructor/Register/Unregisterを使用するアプリは `DriveRec`、`ImageRecog`、`RemoteMonitoring`、`VirtualPassenger`。`System` も `NPVINCameraConfig::SetSceneMode` を使用する。映像入力のQMMF sourceを所有するのは `NEventWatcherAV`。単にDriveRec/ImageRecogを止める案では、残るclientや設定要求を無視することになる。

今すぐ提供できる範囲は、QMMF/AVを生かしたまま個別アプリを操作する既存方式と、問題が判明した基盤操作を拒否すること。ただし他アプリも全て安全に終了可能と一括認定しない。合格したOpening等の個別実測と、未検証項目を分ける。

将来QMMFの保守操作を実装するなら、最低限次の設計が必要になる。

1. 元の稼働集合・recording/camera状態・純正PID/Invocationをprivateに保存する。稼働集合が未知なら操作しない。
2. カメラconsumerの停止要求と新規設定要求を正規owner経路で抑止し、VIN client解放を確認する。上記4アプリとSystemの要求元を扱う。
3. `NEventWatcherAV` のcapture/pipelineが停止してNULLまで遷移し、QMMF session/buffer所有が解放されたことを確認する。unitのinactiveだけではpipeline解放の機能証拠にならない。
4. その後にQMMFを停止する。強制SIGKILL fallbackやcorefilter/watchdog停止を使わない。
5. 再開はQMMF ready→AV/VIN ready→元から稼働していたconsumerの順とし、録画の新規書込み・前方/車内映像・純正主要機能まで確認する。失敗時に未解放資源へ起動を重ねない。

これは設計候補であり、正常capture停止のowner API・AV自身の安全な終了/再開・資源解放の判定は未検証。現在の事故を再現して安全性を試すことは行わない。これらが確定するまでは単独終了の成功ボタンを残さない。

## 再確認用の読取りコマンド

```powershell
python tools/elf-disassemble.py resources/stock-service-inventory/libgstqtiqmmfsrc.so --address 0x9970 --size 180
python tools/elf-disassemble.py resources/stock-service-inventory/libgstqtiqmmfsrc.so --address 0x11fe0 --size 400
python tools/elf-disassemble.py resources/stock-service-inventory/libqmmf_recorder_client.so --address 0x1a934 --size 64
python tools/elf-disassemble.py resources/stock-service-inventory/NEventWatcherAV --address 0x284e8 --size 160
python tools/elf-disassemble.py resources/stock-service-inventory/DriveRec --address 0xe1cb8 --size 280
python tools/elf-disassemble.py resources/stock-service-inventory/DriveRec --address 0xf45f8 --size 112
python tools/elf-disassemble.py resources/libNDeviceLibSys.so --address 0x2e658 --size 100
```

主要材料のSHA-256：

| 材料 | SHA-256 |
|---|---|
| qmmf-server | `3870c138c41e99c3c3b2f945a049539ee30ad59a17b0b9bd13a7fca868ba5bc1` |
| NEventWatcherAV | `6d578df409d95c942ba074beb2a03e469596b42f57b070ea1ba18db0d039c370` |
| libgstqtiqmmfsrc.so | `0751e167478cf0141e5d11fedb8ec7b5877021bcceb3b129f6c0b31c9a533e69` |
| libqmmf_recorder_client.so | `b1828d22ee5b2a575383ecd8edf9cbdf4bb59896d5bb3262ab7e53e5b4a437de` |
| corefilter.sh | `83679a3e38c9ea15901c68db4f6369d915cd8e22908a6b3ad05e900e3d0617dc` |
| DriveRec | `000871dc36581f27ae831d9db3772b553a17cce0d814c52867276ac68ea0cde0` |
