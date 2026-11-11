# 解析PCの予備USB Wi-FiによるAP試験

Windows Wi-Fiは操作しない。解析PCの内蔵Wi-Fi（外部接続）と、NP1 STA上流を提供する使用中USB Wi-Fiも操作しない。対象はprivate inventoryで確認した**未使用のUSB Wi-Fi 1台だけ**。device名・USB path・driver・phy・保護対象2IFをignored `tmp` のprofileでpinする。変更実行はroot coordinatorの排他windowに限る。

## 現状と実装範囲

`tools/verify-analysis-ap.py` は既定でread-only preflight。SSH設定は既存`tools/analysis-pc.py`から取得し、known-host照合を継続する。sudoパスワード、AP SSID/PSK、source/payloadは暗号化SSH stdinで送り、argvには含めない。remoteの試験ファイルはroot専用0700の`/run/ncc-usb-probe-*`だけに置く。config/backupは0600。大容量copy・package導入は行わない。

preflightではroot権限、tmpfs、pidfd機能、USB/driver/phy一致、単一managed IF、切断中・IP無し、NetworkManager managed/autoconnect状態、他processによる対象IF/phy参照無しを確認する。内蔵と使用中USB、host全体のroute/rule/DNS/saved profile hashも比較対象にする。

read-only SSH/sudo・対象preflightは実機解析PCで合格した。現sourceの追加pidfd機能は非特権read-only照会で存在を確認した。**phy移動・WPA関連付け・独自DHCP・復旧を含む変更試験は未実行**。host fixtures合格だけでこれらの実動を合格にしない。

最初のroot起動試行はlock検証時のPython local変数`stat` shadowによってpreflight前に終了した。NM/phy/接続の変更には未到達。変数名を修正し、実start入口のlock検証からpreflight到達までをmock syscallで通す回帰を追加した。失敗後のread-only preflightも合格。以後、failureにはstage・exception type・trace frameだけを付け、例外文/locals/source line/SSID/credentialを診断に含めず、host private出力へ保持する。

2回目はWPA関連付け後にDHCP TIMEOUT。NP1 dnsmasqのprivate logで対象stationからDISCOVER12回とOFFER12回、REQUEST0回を確認した。IPv4未設定IFの通常UDP受信に依存しないよう、DHCPを固定Ethernet/IPv4/UDP/BOOTPのAF_PACKET方式へ修正した。wire checksum・ports・server・xid・MACとoptionをfixtureで検証し、実raw socket試験は未実行。CLIも4gate親directoryの作成・書込み可否・fresh/重複をSSH前に検証し、host保存例外が発生してもremote completeを読み切ってからcloseする。復旧証明が取れなければfalseを送り、元NP1復旧を創作しない。

2回目の事後read-only確認ではspareのMAC/MTU/UP/route/managed/autoconnectが元通りで、own WPA/namespace残留は0。protected uplinkに自然なIPv6 RA defaultが追加されたためhost route invariantは一致せず、remote runtimeのbackupを残した。既存route削除や他snapshot差は無いが、完全不変として合格扱いにせず、外部uplinkのRA routeを試験toolから消さない。

## CLIとgate

読み取りのみ:

```text
python tools/verify-analysis-ap.py --profile tmp/PRIVATE-profile.json --out tmp/FRESH-preflight
```

変更する場合は専用の明示flagと4つのfresh gateが必須。rootがwindowを確定してから使用する:

```text
python tools/verify-analysis-ap.py --profile tmp/PRIVATE-profile.json --ap-access tmp/PRIVATE-ap-access.json --out tmp/FRESH-client --allow-spare-usb-wifi-changes --prepared-file tmp/PHASE/prepared.json --proceed-file tmp/PHASE/proceed.json --traffic-file tmp/PHASE/traffic.json --cleanup-file tmp/PHASE/cleanup.json
```

`ap-access`は`{"ssid":"...","password":"..."}`。profileは`interface,phy,usbPath,driver,protectedInterfaces`。値は公開code/docsへ固定しない。gate directoryは事前にprivate `tmp` 配下で作成する。

1. workerが対象だけautoconnect=no/managed=noにし、元UP状態を保全してphyを独自netnsへ移動。own wpa_supplicantと0600設定でWPA2-CCMP接続する。globalのdaemon・保存profileを変更しない。
2. DHCP clientはnamespace内のAF_PACKET raw socketを対象IFへbind。IPv4未設定でもbroadcast OFFERを受けられるよう、固定Ethernet/IPv4/UDP frameを作る。IP/UDP checksum、ports67→68、固定AP server、xid/MACを照合し、同じleaseのOFFER/ACK、/24 mask、gateway・DNSがAP自身、妥当なlease期間を確認する。namespace内だけにIPv4/defaultを設定し、host DNSは編集しない。
3. AP HTTP APIと自分のMACのauthorized stationを確認し、`prepared`を公開する。
4. AP LANだけの最初の試験ならproceedへ`{"proceed":true,"localOnly":true}`。この結果は`scope=ap-lan-only`で、インターネット成功を主張しない。
5. Internet試験は`{"proceed":true,"expectedUpstream":"wifiまたはcellular","expiresAt":"UTC ISO"}`。fresh実upstream一致を確認し、APへのUDP/TCP DNS、公開A、IF/source bind TCP、Python標準CA・hostname検証TLS、固定example.comのHTTP200を確認する。任意URL、Proxy、redirect、証明書回避は使わない。測定は12秒とgate期限の短い方で打ち切る。
6. `traffic`を公開しても関連付けを維持。rootがNP1正式controllerの元ON/元STA復旧証明を確認後、cleanupへ`{"cleanup":true,"restored":true}`を書く。通信失敗時もprepared済みならこのgateをboundedに待つ。
7. workerはown WPAをpidfdで停止・reapし、own addressesを除去してphyを元host netnsへ戻す。managed/autoconnect/UP/MAC/MTU/target route/未接続managed型、protected IF、host routes/rules/DNS/profile hashをread-backして`complete`を返す。

正式STA OFFの実行はこのtoolに含めない。root/radioの既存bounded controllerをprepared後に起動する。NAT counterの実増加もrootがNP1側で別途採取する。prepared・希望優先回線だけで4G/NAT成功と判定しない。

## 復旧と所有権

- 元host netnsとtest netnsをFDでpin。test namespace名が外部で置換されても、その名前では復旧コマンドを実行しない。phyはpinned FD側から戻し、foreign名は削除せず未確認として報告する。
- WPAはPopen childかつpidfd/waitidで所有を確認する。SIGTERM後の期限超過時だけ同pidfdへSIGKILL。numeric PID再利用先へsignalを送らない。他processを停止しない。
- spareが外部操作で接続中/接続処理中になった場合、そのNetworkManager状態を上書きしない。全体をfailed/needsReviewとし、保護中の他IFにも触れない。
- 一工程が失敗しても他の復旧工程を続け、部分復旧を成功にしない。失敗時はroot-private tmpfsのrecovery.json/commands.log/own-wpa.jsonを残し、`cleanupErrors`を返す。foreign resourceやstockネットワークを推測で削除・修正しない。
- SSH EOF/TERM/HUP/watchdogでもfinallyで復旧を試みる。cleanup中は追加TERM/HUPを無視してbounded復旧を完了させる。

`tests/test_analysis_ap_probe.py`はparser、保護対象拒否、復旧順、途中failure、PID再利用、managed foreign change、foreign netns保持、gate待ち、local-onlyの範囲をfakeで検証する。現在の実接続・DHCP・復旧実測はroot fieldが必要。
