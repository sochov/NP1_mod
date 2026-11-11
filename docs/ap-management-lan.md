# 独立ホットスポット / DHCP・DNS・NAT

2026-11-11。目的は常時テザリング。現sourceはAP自身をgateway/DNSとして配り、検証済み4G優先・実接続Wi-Fi代替のIPv4共有を実装する。詳細は[共有設計・実機結果](hotspot-internet-sharing.md)。常駐LAN APと通常再起動からの復帰は実機合格済み。新共有sourceではWindowsのDHCP/gateway/DNS、UDP/TCP名前解決、HTTP、station、AP interfaceにbindした外部TCP/TLS証明書受信まで確認した。その後、通常bundleでWindows標準hostname/chain/revocation検証付きAP経由HTTPS200とMASQUERADE増加を確認した。4G共有は未確認。旧stageではDISCONNECT操作中にAP HTTP timeoutがあった。通常配備後の再試験ではDISCONNECTED/SCANNING遷移を含む23 samplesの通常HTTPは全成功し、AP/HTTP listenerの維持も確認した。700ms期限の直probeは3回失敗しており、短い遅延までゼロとはしない。下記の初期LAN段階の記録を、共有完成の記録と混同しない。

## 変更範囲

`phy0`へ新規`nccap0`を作り、独自hostapdとdnsmasqを直接の子として起動する。純正`wlan0`/`wlan1`/`bridge0`、QCMAP、wpa_supplicant、純正設定/バイナリは変更しない。STAの既存STATUSから2.4GHzの接続channelを読み、同じchannelを使う。

管理用IPv4は`192.168.226.1/24`、DHCP範囲は`.20〜.99`、leaseは10分。全tableのIPv4経路と全IPv4アドレスに重複があれば拒否する。dnsmasqは専用configだけを読み、`interface=nccap0`、`bind-interfaces`、AP IPv4の`port=53`を使い、DHCP option 3/6にはAP自身を指定する。所有PIDのUDP/67とUDP/TCP53 socket inodeを確認する。上流DNSは専用servers-fileだけから読む。初期LAN実装のport=0/router・DNS空配布から変更している。

NAT/FORWARD/DNS INPUTは専用chainとtoken付き限定jumpだけを追加し、AP送信元専用route tableを使う。stock bridge/chain/table/ruleは置換しない。WAN不在でもAPを止めない。待受・kernel read-backだけでは外部端末のInternet疎通を合格にしない。

## root側の統合点

- mainへ`ap-worker`→`adapters.RunAP(ctx)`、`ap-smoke`→`adapters.RunAPSmoke(ctx)`、`ap-cleanup`→`adapters.RunAPCleanup()`を追加する。常駐/試験はSIGINT/SIGTERMをcontext cancellationへ接続する。
- `np1-control-ap.service`はbrokerへ依存せず、binary/helperを自身の`/run/np1-control-ap`へcopyする。helperは`tools/ap-channel.py`もインストールする。`ExecStopPost`は所有確認付きcleanup。
- `APStatus()`は秘密を含まないObservation。`ApplyAP(expectedRevision, APChange{Enabled,SSID,Passphrase})`は固定unitだけを操作し、失敗時は前設定とmarkerへ戻す。中央APIは通常のCSRF/Origin/idempotencyを適用し、request bodyやPSKを監査へ記録しない。
- 永続設定は`/overlay/owner-apps/np1-control/private/ap/config.json`の0600。`private/ap/autostart`が起動marker。brokerにはこのprivate/apだけの書込み権を追加する。postboot/install/rollback/uninstall、管理bindの`nccap0`追加とfirewall統合はroot担当。

初回設定はrevision 0・無効・PSK未設定。有効化には8〜63文字のASCII PSKが必要。GETはPSKを返さず`passphraseConfigured`だけ返す。空PSKの更新は既存PSK維持。SSIDはUTF-8で1〜32byte、制御文字拒否。hostapdへSSIDをhex、PSKをPBKDF2導出のhexで渡し、設定注入を避ける。

## 所有と復旧

workerは排他flockを持つ。自分の生成したifindex、phy、ランダムな専用ifalias、boot IDをprivate ownership recordへ保存する。終了/異常復旧時はその一致を確認して`nccap0`だけを削除する。子の停止もPID開始時刻と実行引数の完全一致を使い、PID再利用、killall/pkill、純正hostapd/DHCP停止はしない。

常駐runtimeはroot:nccの0750、公開statusは0640。hostapd/DHCP config、ownership、接続情報、lease、各最大128KiBのログはruntime/privateの0700内。PSKを含むconfigはcleanupで削除し、statusへ入れない。生成直後にifindexを読めない等、所有を証明できない異常では他のIFを推測して削除せず`AP_INTERFACE_*`で失敗する。残留IFはprivate recordを確認してroot担当が復旧する。

接続台数はowned hostapdの`all_sta`の`[ASSOC]`を列挙できた場合に一覧長で数値化し、失敗時は独立した`STATUS num_sta[0]`の実測値だけを保持する。0は実測値、項目なしはnull。own dnsmasq leasesの期限内MAC一致分を一覧へIP/hostnameとして補完するが、leaseだけで接続中とはしない。詳細は[Wi-Fi制御設計](wifi-sta-control.md)を参照。

## 起動raceと実機smoke結果

最初のDHCP smokeはAP_HOSTAPD_EXITEDで失敗。hostapdがinterfaceを参照する前にcleanupが走った可能性を調査し、exec後に`/proc/cmdline`が確定するまで最大1秒待つ処理を追加した。live creatorは保持している子handleと開始時刻を用いて、子の終了を確認してからIFを削除する。crash cleanupは従来どおりpersistした厳密identityを要求する。原因を推測だけで断定せず、秘密を含まないstartup時系列をprivateへ記録する。

次の試験はAP-ENABLED/DHCP準備まで進んだが、routeの末尾space差でbaseline比較が誤検出したため成功扱いにしなかった。各行の空白だけを正規化した再試験では、30秒保持・before=during=afterの13項目一致・interface不在を確認した。古いADB shellはremote exit statusを0へ潰す場合があるため、`tools/device/ap-dhcp-smoke-reported.sh`で明示終了codeを返し、`tools/ap-smoke-trial.py --serial ... --binary ... --out tmp/...`がそのcodeを検証する。trial用binaryは独立`/run`stageへ置き、導入済みbinaryを置換しない。結果はprivate/ignored領域へ保存する。

常駐unitの初回有効化は、helperが`ProtectSystem=strict`でreadonlyの`/run`直下へ返信directoryを作ろうとして失敗し、root担当が旧無効設定への復元を確認した。helperをLinux abstract UNIX replyへ変更し、runtimeの0750を保ったままfilesystem返信経路を不要にした。公式ctrl_iface実装はrecvfromの送信元長をそのままsendtoに用いる。新helperのread-only STATUSは実機でassociated channelの応答を確認済み。unitのvendor control socketアクセスには`SupplementaryGroups=NEventWatcherCom`を追加した。cleanupがcurrent stateをstoppedへ変えても、private `failure.json`のcode/stage/timeは残し、APStatus.lastFailureへ返す。まだunit再試験の成功をこの修正だけでは意味しない。

## 30秒DHCP smoke

`tools/device/ap-dhcp-smoke.sh`はステージbundle内のbinary/helperを固有の`/run/np1-ap-dhcp-smoke.*`へcopyして実行する。試験前、AP/DHCP準備完了中、cleanup後に、純正6サービスのPID/state、QCMAP unitのMainPID/ActiveState/SubStateと補助process snapshot、STA snapshot、IPv4/IPv6経路/ruleを比較する。QCMAP unitは0/inactiveも観測状態のまま比較し、process名だけで不在と判断しない。試験中だけ`dev nccap0`の経路と、完全一致する専用source policy/table/protocolを除外する。cleanup後は専用rule/tableも残してはならない。変化/読取不能は成功扱いにしない。

DHCP準備完了後のAP保持は最大30秒。PSKはconsoleへ出さず`runtime/private/join.conf`にだけ置き、終了時に削除する。実行と実機変更はroot担当者だけが行う。

## STA不在の扱い

STAが接続途中・未接続・一時欠測でも、稼働中APは現在channelを維持する。`COMPLETED`で有効な2.4GHzの別channelを確認した場合だけ自分のAPをreconfigureする。接続状態の遷移だけを理由にAPを回収しない。

private configの`standaloneFallback`は独立fieldtrial後に既定trueへ変更した。初期STA channelを確認できない場合は、同じbootで検証済みの前回channel、なければ6を使ってホットスポットを開始する。Web APChangeからは変更できず、明示falseはoperator診断用overrideとしてだけ残す。旧設定のfield省略は新しい既定trueを継承する。Internet共有の上流選択は独立して判定する。

`tools/ap-standalone-trial.py --serial ... --binary ... --out tmp/...`は独立/run unit/bin、private config backup/restore、Windowsのdisconnected adapterだけを用いた再現試験。存在しないSSIDへの切替中も119 samplesのAP HTTP/station/APenabledが全維持され、STAのSCANNINGを実観測した。90秒自動rollback・手動rollback、STA未接続中のworker再起動→Windows再接続/HTTP、元network/profile集合と純正7unitPID/state不変、元APunit/configへの復旧が合格した。temporary PC profileは終了時に削除しadapterを切断する。個別のDISCONNECTED/ASSOCIATING stateはこの試験では未観測であり、そのstateを実測したとは表現しない。新しい有効STA channelへの実切替・STA不在の本体cold bootは別途確認する。

## ホスト検証

AP単体7本とadapters go vetが合格。SSID/PSK検証、GET秘密非出力、config注入対策、DHCP限定、経路/アドレス重複、実測0と未知の区別、UDP socket owner parsing、private/symlink config拒否を確認。Linux arm64向けadapters test binaryのcross compileも成功した。実機Pythonにはjson moduleがないことをread-onlyで確認し、helperの依存は既存のos/socket/sys/timeだけに保った。dnsmasq 2.81はDHCP対応buildとread-onlyで確認した。

新共有sourceでは上流proof選択、専用route/FORWARD/NAT、WAN不在guard、journal復旧、元ip_forward保持、所有権衝突、iptables canonical引数順を追加検証し、Go全体test/vet・arm64 buildが合格。Wi-Fi/AP/navigationの9 browser testsは320/390pxと実上流表示の区別を含め合格。実機の未確認・失敗をhost test合格で覆わない。
