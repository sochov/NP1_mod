# 純正APN所有プロセスへの固定RPC

## 読取りの実機確認

2026-11-11、固定command0x0aの状態取得を合格した。受付byte1、自分PID宛のGenericResult（sequence照合、subtype0x13、result0）、APNStatus event0x3f2の両方を受信した。結果はprofile1 state3、profile2 state2。純正Communication/DriveRec/ImageRecog/Voice/QMMF/NEventWatcherCom/SettingのPIDとActiveStateは全て前後一致した。Proxy名の取得、vendor constructor、共有メモリー、AT/QMIの直接open、APN設定変更は行っていない。

初回はRPC pathと通知pathを同一と誤認して通知timeoutとなった。次の検証でNEvent packed event IDの位置を正した。いずれも成功表示せず、純正PID/stateが維持されていることを確認してから修正した。

## 固定ABIと宛先

NEventWatcherComとCommunicationが引き継ぐSESSION Unix busを使用する。接続先は、fingerprint一致の既存NEventWatcherComが1つだけ存在するとき、その `/proc/PID/environ` のDBUS_SESSION_BUS_ADDRESSから取得する。値やPIDは公開しない。TCP/autolaunch/複数候補/未知optionを拒否し、private connectionでEXTERNAL認証とHelloだけを行う。

- well-known Stub: `SPF.EventBase.NPMComHandler.Stub`
- RPC path: `/SPF/EventBase/NPMComHandler`
- interface/method: `SPF.EventBase.v1_0.CallFunction`
- request signature: `uuuauayu`。値はinternal function1、helperPID、固定command0x0a、空uint32配列、固定24byte payload、reply size1。
- payload: sequence u32、ownerが上書きするcallerPID u32、command0x0a、subtype0x13、NULL Client pointer u64。
- reply: byte配列1個（値1）と空uint32配列。受付だけでは結果取得成功にしない。
- notification: `/SPF/EventBase/Evt012`、`SPF.EventBase.v1_0.SPFEvent`、signature `uuay`。destPID、flags0、event bytes。
- event bytes: event ID u64、handler type0x0c u32、packed NEvent。packed内event IDはoffset24、GenericResultのsequence/subtype/resultは32/36/40、Client pointerは48。GenericResultは56byte、APNStatusは40byte。

Stubのunique owner名を取得し、busが返すUnix PID/UIDを既存プロセスと照合して、そのunique nameへNoAutoStartで送信する。通知senderも同じ所有PID/UIDであることを検証し、前後でowner名とprocess start timeが変わらないことを確認する。通知は通常broadcastであり、eavesdropや固定Proxy名の取得は不要。

## 静的根拠

| 対象 | 根拠 |
|---|---|
| libNDeviceLibCom 0xa2a48 | GetMComAPNStatus、24byte、Client NULL分岐、timeout500ms |
| libNEventBase 0xfd60 / 0xff00 | 固定D-Bus CallFunction envelope、caller getpid |
| NEventWatcherCom 0x98c90 | callerPIDを要求header+4へ保存、受付、enqueue、GenericResult |
| NEventWatcherCom 0x9c030 | sequence/subtype/Clientを結果へ引継ぎ、callerPID宛送信 |
| libNEventBase 0x16b24 | 既存receiverがdestPIDをgetpidと比較 |
| libNEventBase 0x14cb0 | handler番号から `Evt%03u` を生成 |
| libNEventBase 0x19f90 | destination指定のないbroadcast signalを生成 |
| libNEventBase 0x11df8 | NEvent event IDはobject+0x20、Packはobject+8からコピー |

APN enumはNEventWatcherCom 0x96080の明示変換に基づく。liblteapi getAPNStatusがCGACT activeをnative1、inactiveを2、失敗を-1として返し、ownerはnative1→state3、native2→state2、native0→state1、その他→state0へ変換する。state3は純正所有ATで確認したPDP activeを意味するが、WAN IPや実際のegressは別途必要。

fingerprintは `native_apn_linux.go` に固定したNEventWatcherCom、libNDeviceLibCom、libNEventBaseLib、liblteapiのSHA256を使用する。接続と処理は全8秒、method受付500ms、frame64KiB、event4096byte、受信128件まで。資格情報や他のイベント本文を保存しない。

## 再現

`python tools/build_native_apn_probe.py --go resources/toolchain/go/bin/go.exe` の独立bundleを使い、`python tools/deploy.py --serial SERIAL --action native-apn-status-readonly` を実行する。実機変更は専用 `/run/np1-native-apn-status` へのhelperコピーと状態診断だけで、純正サービスを停止しない。外側12秒timeout、signal時もchildをkill/waitしてからstock比較する。

Go tests/vet、CGO0 LinuxARM64 compile、固定要求signature・NULL pointer・宛先/sequence/subtype・enum/長さ・Unix address allowlistを検証した。APN適用処理はこの読取りの実機合格とは別に検証する。

## 同値profile1試験と管理経路の復旧

既存のprofile1と完全に同じ設定だけをcommand0x0bへ送る試験を行った。profile2 presence flagとその全payloadは0、Client pointerはNULLだった。受付とGenericResult0、Connect event0x3f3（profile1 result2/profile2 result1）は受信した。ただしConnect event自体はsequenceを持たないbroadcastであり、別の純正処理との相関を単独で保証するものではない。profile2の入力有無をこのresultだけで判断しない。

さらにownerのwire decoder（0x82be8）を追うと、presence flag0でも空NStringを持つAPNInfoへのnonnull pointerを作っていた（82cf8/82d0c）。wire省略は最下層のNULL pointerと同じではない。全fieldをゼロにしたAPN2は、setAPNstatus4304の空APN guardでQICSGP2へ進まないことと、実際のprofile一覧維持を保護根拠とする。空cacheとの比較でnative1→result1になる場合もある。

liblteapiのCM launcher0x3360は `/bin/sh -c` と末尾 `&` の固定commandをposix_spawnする。shellの終了statusをlogするが戻り値には反映しない。result2はnative0からの変換であり、CM生存やLinux IP取得の保証ではない。NEventWatcherComはlte groupとNET_ADMIN等を持つため、単純な権限不足とも断定しない。

直後のGET欠測をfalseとした初版summaryは不適切だった。後続の独立読取りとprivateバックアップの厳密比較で、profile1は全39 TLVが完全一致、profile一覧は `[1]` のまま、owner状態3/2も維持されていた。後続実装は未知値をnullとし、固定GETだけを上限3回再試行する。タイムアウトした0x0bは自動再送しない。

私有captureの再比較は `tools/audit_apn_trial.py --trial PRIVATE_SUMMARY_JSON --recovery PRIVATE_RECOVERY_DIR` で再現できる。provenance/固定QMI header/TLV長/重複/必須fieldを検証し、元と後続のfield数・一致bool・変更field番号だけを出す。実機値やcredentialは出力しない。

一方、純正0x0bの処理後にwlan0の元default routeが1本消失し、Tailscaleの接続が落ちた。IPv4 address、STA association、AP、Web、純正7PID/stateは維持されていた。firewallはコメントとpacket/byte countersを正規化すると全rule一致で、実際の追加・削除は0だった。APNの変更やfirewall全体復元は不要だった。

`tools/recover_apn_trial_network.py --serial SERIAL` と `tools/device/recover-apn-trial-default.sh/.py` に復旧を固定した。直前の検証済みprivate WiFi baselineと今回の事前snapshotに対し、同じSSID/networkID/IPv4・COMPLETED・純正7PID/state・競合default無しを確認し、記録した元defaultの固定argvだけをaddした。route flush、APN再適用、firewall書換えは行わない。読み戻しとroot側のHTTP200/Tailscaleオンライン/AP有効を確認して復旧完了とした。

実機のwpa_cliはreply socket権限によりSTATUS timeoutとなるため、復旧toolは既存Go VendorStatusと同じgroup0660の短命Unix datagram socketを使う。private証拠は0700 directoryに置き、vendorが探索できるreply socketだけを既存runtime内に作る。これらのSTATUS失敗はいずれもroute変更前だった。

## Web適用用transaction（source検証済み・実機write未検証）

`NewNativeAPNProfile1Input` はPDP IPv4とASCII fieldを検証し、opaque型のfmt/JSONは値を出さない。`ApplyNativeAPNProfile1` はprofile1だけを受け取り、任意index/operation/pathを公開しない。root helperは他のnative/network操作と直列化し、固定WDS読取り中は自分のmodem observerだけを停止・復帰させる。プロセスをまたぐlockも固定root recovery directory内に限定した。

処理前に固定WDS profile1、profile2不存在、native状態、元STA/default/stock identityを取得し、root0600のimmutable profileと固定 `native-transaction.json` へ私有backupを保存する。0x0b後は管理defaultを即座に条件復旧し、10秒間は後続の純正処理による欠落も観測して条件復旧する。終了時にも独立した上限10秒のcleanupで確認する。helper外側timeoutはcleanup込み70秒以上が必要。途中終了後は `RecoverNativeAPNManagementRoute` が固定private metadataからrouteだけを条件復旧できる。

Connect callbackは自分宛GenericAckのmonotonic時刻以後だけを認め、native setAPNstatusの必須200ms待ちに基づいて150ms未満の古いbroadcastを除外する。時刻はpinned実装の32bit milliseconds wrapも扱う。journalのCM開始記録が初期helper結果より約5秒遅く現れたため、ログ配送遅延や純正後続resetを考慮し、callbackだけで早期完了にしない。最終WDS/native再確認は管理経路の観測期間の後に行う。

WDSの全APN field、profile2不存在、native状態を読み戻す。PDPがactiveから非activeへ落ちた場合は、現在fieldが自分のtargetと再照合できるときだけ元profile1を同じowner経路で戻し、WDS/PDPを再検証する。他者の設定、未知状態、profile2の変化へ盲目的に上書きしない。欠測はnullable値と理由を返す。

`dataLink` は純正PDP状態とは別で、quectel-CM processの有無、GobiNet/USB2c7c:0125へ紐づくusb0とその子IFのIPv4、Linux defaultを取得する。Linux egress設定があることもInternet到達性の証明ではない。今回の事後確認ではPDP state3でもquectel-CMは存在せず、usb0系にIPv4/defaultがなかったため、通信可能とは表示しない。

単体テストでprofile2未確定時のno-write、欠測null、write再送無し、他者設定への上書き拒否、PDP低下時の原状戻し、route復旧不能時の成功禁止、秘密非出力、route grammar、STA identity、PDPとLinux egressの分離を検証した。Go tests/vetとARM64 compileを合格。新transactionによる実機writeとWeb適用は、まだ合格とはしていない。
