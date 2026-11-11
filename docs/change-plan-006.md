# 独立した仮想APの短時間試験

純正アプリが利用不可との回答、純正IPC ABIが未確定のため、純正経路を書換えない代替を検証する。実施スクリプトは `tools/device/ap-smoke.sh`、STA読取りは同じディレクトリの `ap-channel.py`。今回の改訂ではAP/hostapdを起動していない。ホスト上の構文・静的検証に加え、実機ではhelperのSTATUS読取りだけを確認した。

## 変更範囲と事前条件

- 新規 `nccap0` だけが対象。同名IFが事前にあれば中断し、既存IFには後片付けも行わない。既存wlan0/wlan1/bridge0、純正設定・QCMAP・QMI/SIM・DHCP/NAT/routeを変更しない。
- rootで実行。必要コマンド、純正Communication/DriveRec/ImageRecog/Voice/QMMF/NEventWatcherComのactive・非ゼロMainPIDを先に確認。不健全なbaselineならAPを作らない。
- 純正STAの読取り専用STATUSから、COMPLETEDかつ2.4GHzのchannel 1〜13を取得。同じchannel、beacon100msで試す。`iw`の情報不足を未接続と決めつけない。helperの既存デフォルト出力はchannel番号のみを維持し、実機でのchannel6読取り実績と同じ経路を使用する。
- WPA2/CCMP、試験ごとのランダムPSK、固定の試験SSID。root専用 `mktemp -d /run/np1-ap-smoke.XXXXXX` を使い、以前のpidfile/configと混ぜない。PSK長を検証し、標準出力には出さない。
- helperの返信ソケットも毎回固有ディレクトリに置く。supplicant側グループの通過・書込み権限だけを付与し、成功・失敗で自分のsocket/directoryだけを削除する。`--snapshot` は比較用のSSID/BSSID/IP等を、ソートした許可キーとASCIIエスケープ表現で出力するため、root専用runtime以外やGitへ保存しない。credentialは含めない。

## バックアップ・read-back

純正設定・サービスを変更しないため、復元用に純正ファイルを書き戻す処理は置かない。代わりに、起動前と後片付け後の以下をprivate runtimeへ保存する。

- 純正6サービスのunit名、MainPID、ActiveState、SubState: `before.txt` / `after.txt`
- STAの関連付け状態・周波数・接続先識別・IP: `sta-before.txt` / `sta-after.txt`。読取り失敗は `sta-after-error.txt`
- IPv4/IPv6の全table経路、IPv4/IPv6 rule: `routes-before.txt` / `routes-after.txt`
- 診断用iw情報、試験用hostapdログ、AP status最終結果

経路の並び順はsortして比較する。IPv6有効期限等が自然に変化した場合も一致とは見なさず、ログから確認が必要な「判定不能」とする。比較のために経路やSTAを再設定しない。

## 試験と必須後片付け

1. baselineの収集後だけ仮想IFを作成し、作成成功を追跡してifindexを保存。catchable signalで成功記録を飛ばさないよう、その短い区間は終了要求を保留する。
2. hostapdはdaemon化せず自分の直接の子として起動。pidfileに頼らず `$!` と `/proc` の開始時刻・親PIDを保存する。稼働確認では実行ファイル・configの完全な引数も照合する。
3. 最大10秒で `state=ENABLED` と要求channelを確認。到達後20秒待ち、再度確認。プロセスactiveだけでは合格にしない。
4. 正常終了、途中エラー、HUP/INT/TERMの全経路を共通EXIT後処理へ集約。後処理中は再度のcatchable signalで処理を飛ばさない。
5. 自分で起動した子のPID・開始時刻・親PIDが一致する場合だけTERM。最大5秒後も同一子が残ればKILLし最大3秒確認。起動直後のexec前も自分の子として回収する。killall/pkill、純正hostapd停止、古いpidfileによるkillは行わない。
6. 自分で作成したIFで、保存ifindexとphyが一致した場合だけ削除。所有権を確認できない同名IFは削除せず失敗を報告。削除後の存在も確認する。
7. 不要になったPSK入りconfigを削除する。その他のログはroot専用runtimeに残す。
8. cleanupでエラーがあっても純正サービス・STA・routeの全read-backを試みる。baselineと比較できない場合を含め、一つでも失敗すれば非ゼロ終了。純正の停止・再起動や推測復元は行わない。

SIGKILL・電源断ではプロセス内後処理は実行できない。中断後に再試行する前に、残存 `nccap0` とprivate runtimeの記録、試験用configを引数に持つ子だけを確認する。所有権が不明なものを名前だけで消さない。純正PID/state・STA・routeに差があれば、試験を成功扱いにせず、実機担当が独立した管理経路から状況確認する。

## 合格範囲

AP ENABLED・channel一致、試験用hostapd/IF撤去、純正PID/state・STA・経路維持が全て確認できた場合だけ、この短時間試験を合格とする。可能なら外部端末のSSIDスキャンも別途記録する。DHCP/クライアント疎通/24時間/再起動試験までは常時AP完成としない。

## 初回ホスト検証（AP未実行）

- Git同梱POSIX shの `sh -n tools/device/ap-smoke.sh` 合格。
- `ap-channel.py` をPython 3.5文法でAST解析し合格。
- 純粋なchannel変換に対しchannel1/6/13の受理、未接続・5GHz・不正周波数・非整数の拒否をホスト上で確認。初回の静的レビューではsocket通信を行わず、その後の互換修正で読取り専用STATUS通信だけを実機確認した。
- killall/pkill/daemon化の不使用、6サービスの列挙、共通EXIT後処理と3種read-backの存在を静的確認。
- 実機へのファイル転送・AP作成・hostapd起動はこのレビューでは行っていない。AP実機試行は担当者が別途実施する。


## 最小Python実機互換の追加確認

実機Python 3.5ではjson等の標準モジュールが省略されている。構文互換だけでは十分でないため、helperのimportを実機利用可能な `os/socket/sys/time` の4個だけに限定した。argparse/json/tempfileは使用しない。固有directoryはpid・時刻・試行番号から名前を作り、`os.mkdir` の排他的作成で確保。既存directoryを再利用・削除しない。

snapshotは固定whitelistをキー順で並べた `key=ascii(value)` の行とし、SSID内の改行・日本語もASCIIエスケープして安定比較する。credentialキーは出力しない。ap-smoke側の比較ファイル名は `.txt` に統一。

- ホスト: Python3.5 AST、importの4個限定、channel6、snapshot順序安定、非ASCII/改行エスケープ、whitelist外credential非出力を確認。
- 実機: ファイル転送なしの `python3 -c` で同一ソースを実行し、通常モードはchannel6、`--snapshot` は6個のwhitelistキーを確認。snapshot内容は `tmp/ap-helper-compat/` にだけ保存し、画面・公開文書へ出力していない。
- stdinでのADB実行はEOF伝播待ちとなったためtimeoutし、引数 `-c` に切り替えて読取り確認が成功。AP/hostapdコマンドは一度も実行していない。
