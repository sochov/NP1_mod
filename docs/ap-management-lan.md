# 独立管理AP / DHCP（実装引継ぎ）

2026-11-11。独立APとowned DHCP UDP/67の30秒保持、純正/STA/routesの前中後13項目一致、終了後IF消去を実機確認した。外部端末のDHCP ACK/HTTP到達、常駐/再起動、STA不在、インターネット共有の成功をこの記録だけでは意味しない。

## 変更範囲

`phy0`へ新規`nccap0`を作り、独自hostapdとdnsmasqを直接の子として起動する。純正`wlan0`/`wlan1`/`bridge0`、QCMAP、wpa_supplicant、純正設定/バイナリは変更しない。STAの既存STATUSから2.4GHzの接続channelを読み、同じchannelを使う。

管理用IPv4は`192.168.226.1/24`、DHCP範囲は`.20〜.99`、leaseは10分。全tableのIPv4経路と全IPv4アドレスに重複があれば拒否する。dnsmasqは専用configだけを読み、`interface=nccap0`、`bind-interfaces`、`port=0`でDHCPへ限定する。router/DNS optionを空にし、DNS server、NAT、bridge、forwarding、既存firewallの変更は行わない。DHCP processが実際に所有するUDP/67 socket inodeを確認し、単なるprocess activeを準備完了としない。

DHCPサーバー待受の確認だけでは外部端末のlease取得やHTTP到達を確認したことにはならない。NATとインターネット共有は別段階。

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

`tools/device/ap-dhcp-smoke.sh`はステージbundle内のbinary/helperを固有の`/run/np1-ap-dhcp-smoke.*`へcopyして実行する。試験前、AP/DHCP準備完了中、cleanup後に、純正6サービスのPID/state、QCMAP unitのMainPID/ActiveState/SubStateと補助process snapshot、STA snapshot、IPv4/IPv6経路/ruleを比較する。QCMAP unitは0/inactiveも観測状態のまま比較し、process名だけで不在と判断しない。試験中の比較は`dev nccap0`の専用経路だけ除外する。変化/読取不能は成功扱いにしない。

DHCP準備完了後のAP保持は最大30秒。PSKはconsoleへ出さず`runtime/private/join.conf`にだけ置き、終了時に削除する。実行と実機変更はroot担当者だけが行う。

## STA不在の扱い

既定はSTA channelが不明なら`waiting-sta`。STAが切断/別channelへ移ると自分のAPだけを回収して再確認する。この段階では「STA不在でも管理APが常時使える」という最終要件は未達。

private configの`standaloneFallback`は既定falseのoperator-only opt-in。`STATUS`で`DISCONNECTED`または`INACTIVE`を確認できた場合だけchannel 6を選ぶ経路を設けている。STATUS読取失敗、接続処理中、5GHz接続時にはfallbackしない。これは独立した実機試験前の経路であり、WebのAPChangeから変更できない。root担当者がADBを維持したDISCONNECT/RECONNECT専用試験で実測してから有効化を判断する。

## ホスト検証

AP単体7本とadapters go vetが合格。SSID/PSK検証、GET秘密非出力、config注入対策、DHCP限定、経路/アドレス重複、実測0と未知の区別、UDP socket owner parsing、private/symlink config拒否を確認。Linux arm64向けadapters test binaryのcross compileも成功した。実機Pythonにはjson moduleがないことをread-onlyで確認し、helperの依存は既存のos/socket/sys/timeだけに保った。dnsmasq 2.81はDHCP対応buildとread-onlyで確認した。
