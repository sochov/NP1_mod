# 純正サービスの個別操作と専用ロックの復旧

システム→本体→純正サービスの各行から起動・停止・再起動を行う。一覧は5秒更新、操作中は二重操作を抑止し、未取得・未導入・競合・確認できなかった結果を成功扱いしない。起動/再起動は同一PIDとInvocationが3秒継続し、停止はinactive/PID0が1秒継続してから成功を表示する。途中の観測不能区間は安定時間に含めない。起動状態の操作であり、次回bootのenable/disable設定は変更しない。

2026-11-12の通常配備後、Web APIでOpeningの停止→起動→再起動を確認し、ほか25サービスのstate/PID/Invocationが不変だった。本番画面の26行と操作ボタン、390px幅、Tailscaleの両画面表示もGET限定browser試験で合格した。全26サービスの機能を個別に止めて往復した証拠とは区別する。一般UI/API契約はOpenAPIとfrontendのsynthetic操作・失敗試験で検証した。

## 操作対象と役割

実機の unit 定義・依存関係と既存 ELF を読取りで確認した。以下の26サービスを固定 ID で選択し、任意 unit 名や shell コマンドを HTTP 入力から受け取らない。役割は unit 名・起動対象・関連処理から確認できた範囲であり、すべての運転中挙動を試験したという意味ではない。

| unit | 役割・主な影響 |
|---|---|
| `app-@DriveRec.service` | 純正録画。停止中は録画できない |
| `app-@ImageRecog.service` | 映像認識・画像出力。ライブ画像と運転支援に影響 |
| `app-@Voice.service` | 音声案内・Alexa |
| `app-@Navigation.service` | 経路案内 |
| `app-@Communication.service` | 純正の4G・Wi-Fi・接続要求管理 |
| `app-@Setting.service` | 純正設定の保持・反映 |
| `app-@Update.service` | 純正ソフトウェア更新。OTA許可設定とは別 |
| `app-@RemoteMonitoring.service` | 純正リモート監視 |
| `app-@VirtualPassenger.service` | 対話・同乗者機能 |
| `app-@InfoMtgEngine.service` | 純正情報案内エンジン |
| `app-@SitAwrEng.service` | 純正状況認識エンジン |
| `app-@Opening.service` | 起動時の案内 |
| `app-@Misc.service` | その他の純正機能 |
| `app-@System.service` | 純正アプリ群のシステム制御 |
| `app-AppManager.service` | 純正アプリの管理 |
| `NEventWatcherAV.service` | 映像・音声イベント |
| `NEventWatcherCom.service` | モデム・通信イベント |
| `NEventWatcherITS.service` | ITS関連イベント |
| `NEventWatcherSys.service` | システムイベント。終了処理は下記参照 |
| `NPSystemMonitor.service` | プロセス・負荷の監視 |
| `qmmf-server.service` | カメラ・録画の基盤 |
| `QCMAP_ConnectionManagerd.service` | 純正ネットワーク接続管理 |
| `loc_launcher.service` | 純正測位プロセスの管理 |
| `location_hal_daemon.service` | 純正位置情報サービス |
| `pulseaudio.service` | 音声出力 |
| `socketserver.service` | 純正ハートビート |

`adbd.service` と `dbus.service` は調査時の復旧経路・OS基盤として確認するが、この一覧の操作対象には加えない。存在しない `app-@NPSystemMonitor`、`app-@PIMG_Core` などをテンプレートから生成して表示しない。

## 依存関係と終了処理

14個の `app-@…` は AppManager を `Requires` に持ち、AppManager はこれら14アプリ・4個の NEventWatcher・NPSystemMonitor を `Requires` に持つ。通常の stop/restart は要求された unit 以外にも停止ジョブを伝播し得る。3操作すべてで固定の `--job-mode=ignore-dependencies` を使用する。systemd v239 の `transaction_add_job_and_dependencies` は依存ジョブ追加を `!ignore_requirements` 内で行い、逆依存への stop/restart 伝播も同じ条件内にある。[systemd v239 ソース](https://github.com/systemd/systemd/blob/v239/src/core/transaction.c#L853)

これは `ExecStop` 内に書かれた別コマンドを抑止する機能ではない。`NEventWatcherSys.service` の `ExecStop` は `socketserver.service` を停止するため、Sys の個別再起動では元の socketserver の稼働状態も把握・復元する必要がある。

アプリ・AppManager・NEventWatcher・NPSystemMonitor は `Restart=no`。qmmf は `Restart=always`、PulseAudio と socketserver は `Restart=on-failure`。systemdに停止を依頼する方式なら、単に SIGTERM を送る方式と異なり `Restart=always` の再起動抑止も正規に処理される。

純正の `signalhandler_app.sh` / `signalhandler_lib.sh` は通常の TERM を再起動対象から除外する。一方、KILL 等の異常終了ではほかの純正プロセス停止・本体再起動を実行する。unit は `SendSIGKILL=yes`、停止上限90秒のため、操作の短いHTTPタイムアウトで純正プロセスを強制終了してはならない。操作側からの SIGKILL fallback は設けない。

AppManager の `SetMiconWatchDogReset` は BootInfo ハンドラ内、`NSystem::Reset` は特定のサブCPU状態イベント内に確認した。NPSystemMonitor はプロセス統計・メモリ・負荷の処理を持つ。これらを根拠に「任意アプリを止めると必ず本体が再起動する」「監視は一切存在しない」のどちらも断定しない。

## 生存期間ロックの残留

Opening の個別停止では他サービスと起動IDを維持できたが、その後の開始は終了255となった。静的解析と実測は次の経路で一致した。

1. main `0x12520` は Process Start 出力後、`0x125c4` で `NProcSyncObject::Create("Opening")`。
2. `0x125d8` は専用 map の mutex を100msで取得する。時間切れなら `0x12924` へ進み `-1` を返す。
3. `Create` は新規作成だけでなく既存 map の open 成功でも true。新規時だけ初期 mutex を解放する。
4. `/tmp/victoria/victoria_Opening.map` は payload 0 の128byte専用 map。mutex は offset16、所有TIDは24、kindは32、参照数は112。
5. 停止後の実物は kind129（再帰・process-shared・non-robust）、参照1、死亡済みの元 Opening 所有TIDを保持していた。新しい起動試行の所有TIDではなかった。

したがって「最初の起動中に再度停止したことだけが原因」とは断定しない。non-robustな生存期間ロックは正常なTERM後にも残る可能性があり、systemd の active だけでなく起動後の同一PID継続を確認する。

### 固定 singleton 名の静的根拠

次の `Create` 引数が UTF-16 の名前と一致する。表の住所は解析対象 ELF の VA。

| 対象 | main / Create 呼出し | 名前の VA |
|---|---|---|
| Communication | `84d30 / 84dd4` | `2f7610` |
| DriveRec | `3f090 / 3f134` | `1747c0` |
| ImageRecog | `274f0 / 27594` | `bae10` |
| Voice | `54978 / 54a1c` | `21efc0` |
| Navigation | `5e058 / 5e0fc` | `41ec90` |
| Setting | `367d0 / 36874` | `130360` |
| Update | `21c10 / 21cb4` | `a2e78` |
| VirtualPassenger | `224c0 / 22564` | `e4710` |
| InfoMtgEngine | `3bc20 / 3bcc4` | `1384b8` |
| SitAwrEng | `431c8 / 4326c` | `19b948` |
| Misc | `11650 / 116f4` | `51de0` |
| RemoteMonitoring | `1b9e0 / 1ba84` | `68620` |
| Opening | `12520 / 125c4` | `4a540` |
| System | `52be8 / 52c8c` | `173820` |
| AppManager | `11250 / 112f8` | `4a5e0` |
| NPSystemMonitor | `78d8 / 7920` | `9078`（GOT `1c018` の RELATIVE） |

NStandard 側は `NProcSyncObject::Create 34650` → `OpenOrAlloc`、新規時 `346d8` で unlock。`SynchronizeStart 31bc8` は `pthread_mutex_timedlock` の ETIMEDOUT だけ false とする。グローバルな SDK map や設定 map はこの退避対象に含めない。NEvent 等で未確認の名前を推測して削除しない。

## 通常操作での退避条件

`adapters/stock_service_sync.go` は上記16個にだけ固定で対応する。開始前、または個別 stop 完了後の restart 中に以下をすべて確認する。

- unit が inactive/failed、MainPID 0、進行中 Job なし。
- 固定 path が symlink でない regular file、128byte、hardlink数1。
- payload0、mutex lock1/2・count1・owner非0・users1・kind129、参照1。
- 所有TIDの `/proc/TID` が存在しない。PIDとTIDを取り違えない。
- 全 `/proc/*/maps` の device/inode、および全 fd に対象の参照がない。読取り不能な生存プロセスは失敗扱い。
- 全検査の再実行後、同じ inode・同じ128byteである。

条件成立時だけ `/tmp/victoria/.ncc-service-backups`（root所有0700）配下へ同一ファイルシステム上で rename する。map 内容を修正したり、mutex を外部から unlock したりしない。バックアップは所有マーカー付きで保存し、自動削除しない。4096件を上限としてそれ以上の退避は拒否する。元ファイル不在は no-op、未知形式・生存所有者・生存参照はエラーにする。退避した古いロックは復旧済みの有効な状態ではないため、稼働中に元 path へ戻さない。

broker は process の fd/maps 参照を調べる権限と、既存 `/tmp/victoria` の group write、当該ディレクトリの書込み許可を必要とする。CAP_DAC_READ_SEARCH は書込みを許可しないため、それだけで退避可能と扱わない。

## 再現と検証

読取りのみの inventory：

```powershell
python tools/inspect-stock-services.py --serial SERIAL
```

Opening がこの既知 lock で開始できず inactive/failed の場合の固定復旧（rootで、ほかの実機操作と排他する）：

```powershell
python tools/recover-opening.py --serial SERIAL
```

このツールはチェック済みスクリプトを一時領域へ stage し、専用 map の退避 → Opening のみ `ignore-dependencies start` → 同一PIDで5秒 active → 全 loaded service のPID・InvocationID・状態と本体起動IDの前後一致まで確認する。ADBがremote終了コードを返さない構成もあるため、host側は固定PASS出力の存在まで要求する。

初回復旧フィールドは成功し、ほかの全 loaded service と本体起動IDの不変を照合した。生dump・PID・起動ID・map・ログは `tmp/stock-service-evidence` 内だけに保存する。Web経由の各サービス操作や、停止した機能の実際の復帰は別に検証する。プロセスが active であるだけで、録画・映像・音声・通信・運転支援すべてが復帰したとは報告しない。

host 検証は Go の `TestStockSync*`（元bytes保存、所有者生存、maps/fd参照、未知形式、inode/bytes変化、バックアップ上限・所有者、取消し等）、Python の `test_stock_service_diagnostics.py`、Go test/vet と Linux ARM64 build を実行する。
