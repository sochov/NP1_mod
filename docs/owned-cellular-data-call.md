# STAと併用する独立WDS接続の試作

## 目的と現在の検証範囲

純正CommunicationはWi-Fi STAの接続完了時に純正LTE data callを止める。APの「4G優先」だけでは、登録済みの4GからLinuxの出口を作れない。STAを保持したまま、独立WDS CIDで保存済みprofile 1のIPv4接続を所有する経路を試作している。純正サービスの停止、状態40の偽装、APN設定変更、純正CMとの再起動競争は行わない。

2026-11-12の固定read-only preflightで、ピン留めした実機GobiNetのRMNET_INFO取得が合格した。QMAP version 5、mode 2、受信サイズ4096、HSUSB endpoint/interface 2/4、`usb0.1`/mux `0x81` と `usb0.2`/mux `0x82` の対応を確認した。data callは開始していない。

次のsession-only helperはソース・fixture・Linux ARM64 buildまで実装済み。**Start/Current/Stopの実機試験は未実施**である。IP、link UP、Linux route、resolverは一切設定せず、`LinuxConfigured` / `InternetVerified` はfalseのままになる。WDS接続とLinux出口・APの外向き疎通を混同しない。

利用者の2026-11-12の指示に従い、STAと4Gの同時維持は今後のTODOへ移した。QMAP取得以外のfield試験は実施せず、以下は明示opt-inの試作として保存する。通常起動サービス、Web API、AP upstream選択には接続していない。

## 純正の優先ポリシーを調べた結果

保存したCommunication ELFの静的解析で、`DataManager::NwStateUpdateProcessingWifi` (`0x207d18`) のWi-Fi接続完了側は `0x208030 → 0x20e070 → 0x212900 → 0x218428 → CLControl::StopMain(0x21aff0)` を通る。未接続側は `0x207f80 → 0x20df58 → StartLTEMain(0x212358)` を通る。LTE側の状態処理 `0x2078b8` もstatus 40を参照し、STA ONの場合はLTE active flagを落とす。status 40はownerが更新する派生状態なので、これを偽装する解決は行わない。

内部のStartLTEMain自体にはSTA判定がないが、外部要求の `OnAIPI_CCOM_RequestLTEStartPromotion(0x212680)` は `0x212704` でstatus 40を検査し、STA ONなら開始せず返る。permission/ICCの自動開始にも同じ判定がある。`TelephonyServiceIFWrapper(0x206f38)` の内部input `0x0b` からStartMainへ進む経路はあるものの、外部から同時維持ポリシーを設定できる正規envelopeは実証できていない。

Telephony function `0x501` の既存protobufもLTE状態/ICC/準備中/接続許可を持つのみで、確認した範囲にSTAとLTEの併用優先度はない。`ChangeWifiStaPriority` はSTA接続候補の順位であり、Wi-Fi対LTEの優先度と扱わない。低層 `ConnectMComAPN` での開始は、STA復帰後に純正StopMComを受ける実測がある。このため、純正CMを強制再起動するloopや純正serviceの停止ではなく、別CID/別muxを所有する候補を残した。

## 根拠

- [libqmi WDS schema](https://github.com/linux-mobile-broadband/libqmi/blob/main/data/qmi-service-wds.json): Bind Mux `0x00a2`、Set IP Family `0x004d`、Start `0x0020`、Stop `0x0021`、Current Settings `0x002d`、Packet Service Status `0x0022`。
- [libqmi WDS enums](https://github.com/linux-mobile-broadband/libqmi/blob/main/src/libqmi-glib/qmi-enums-wds.h): IPv4 family 4、disconnected 1 / connected 2、Current requested-settings bitmask。
- [公開Quectel CM由来コードのmirror](https://github.com/kmilo17pet/quectel-cm): `GobiNetCM.c` はQMAP有効時にWDA CIDを開かず、`QMIThread.c` はEndpoint/Mux bindingとIPv4 client familyを設定してからStartする。これは参考実装であり、NP1の配布物や公式最新releaseと同一とは扱わない。コード自体を本実装へコピーしていない。
- 実機のGobiNet moduleをローカルで静的解析し、version/srcversion/SHA256を既存 `verifyGobiNet()` で固定する。`qmap_ndo_do_ioctl` の `0x89f3` 分岐は164byte構造体の生成と`copy_to_user`だけであり、link-state変更の `0x89f1` は呼ばない。`0x89f2` も呼ばない。
- RMNET_INFOの実配置は、size/rx_urb_size/ep_type/iface_id/qmap_mode/qmap_version/paddingの7個のuint32、ifname[8][16]、mux_id[8]。ARM64 ifreqのdata pointerはoffset16。固定対象以外のinterfaceやioctl番号を入力するAPIはない。

## 固定操作と所有範囲

1. 純正CMが存在せず、QMIの他所有者もいないこと、必須通信ownerの同一性と保存したstock状態を検査する。利用者が停止した非必須stockは起動しない。
2. 元STAがCOMPLETEDであること、元APのHTTP・DHCP所有者・プロセスが検証できることを必須とする。USB各IFにアドレスがある場合は開始を拒否する。
3. 新しいWDS(1) CIDだけを割り当てる。CTL/WDA/DMS/ATや既存FDには触れない。
4. 全profile TLVと同CID inventoryをroot-only recoveryへ保存する。初段はprofile 1が解釈可能で、profile 2の不存在がGETとinventoryの両方で確定する構成だけを受理する。
5. 自分のCIDを `usb0.2` / mux `0x82`、HSUSB interface 4、tethered clientにBindし、IPv4 familyを指定する。binding済みCIDのstatusがdisconnected以外ならStart/Stopをせず終了する。
6. Startに渡すのはIP family 4、3GPP preference 1、保存済profile index 1だけ。APN/username/password/auth、autoconnect、DNS preferenceやprofile変更のTLVは送らない。
7. 応答から得た非zero・非wildcardのPacketDataHandleだけを保持する。Current Settingsでprofile 1/IPv4とIP/mask/gateway/MTUを確認し、DNSは確認できたものだけを私有記録する。
8. 3秒の観測後、同じCID/取得したown handleにだけStopする。Stop後の同CID status=disconnectedを確認し、全profile TLV/inventoryが完全に同一であること、元STA/AP/stock/IF/main IPv4 route/ruleを確認する。自分のCIDをcloseして終了する。

元main default、STA、AP、IPをこの試作が変更する処理は存在しないため、第三者が変えたrouteを上書きする復旧処理も入れない。元状態の変化は失敗として保存する。Linux設定を持つ次段では、追加した自分のIP/専用table/ruleだけを撤去し、元mainを変更しない設計が必要となる。

## 中断・衝突・再実行

stdinは親が保持するpipeで、EOF/SIGTERMは新しいStartを止める。Start送信直後は相関する応答を最大15秒drainし、handleが判明したら独立した最大10秒のcleanupでStopする。純正CM出現や未知のQMI FDは以降の通常操作を拒否する。cleanup時だけ、元のFD/hardware/muxとown handleの同一性を検証してStopできる。

Start応答が失われhandleが不明の場合、wildcardや記憶した別CIDのhandleをStopしない。`cleanup-unverified` を記録し、closeだけでPDP解放が確認できたとは表示しない。次のStartをpersistent journalが拒否する。

private journalは固定 `/overlay/owner-apps/np1-control/private/cellular/session-probe` のroot 0700、状態/lockは0600、no-follow、exclusive flock、fsync後atomic rename。Start前に `start-pending` を保存する。起動時は `complete` かつcleanup/baseline/profileがすべて検証済みの場合だけ次の有限試験を許す。途中状態から自動再送・別CIDでのStop・自動修復は行わない。

shellは自作modem observerだけを必要に応じて止め、試験子のwait/reap後に元active状態へ戻す。純正unitは操作しない。100秒でTERM、115秒で同じ専用実行ファイルの子にKILLを送る外部監視がある。driverのopen/ioctl/closeがkernel D-stateから戻らない場合、ユーザー空間のdeadlineだけではreapを保証できない。その場合は次のsession/observerを起動せず、journalと実PIDの診断が必要となる。

## 実行と現在の制限

ホストで `python tools/build_owned_cellular_probe.py`。rootが通常bundleとは別のstageへ配置し、固定 `tools/owned-cellular-preflight.sh` または、排他試験窓で `tools/owned-cellular-session-probe.sh` を実行する。任意profile/APN/packet handle/ifname/routeの引数はない。

shell構文、QMAP構造体の境界、fixed request、IPv4 byte order、異なるprofile/family・不正mask/handleの拒否、Start直後キャンセル、既存call、CM出現、応答喪失、Stop失敗、phase保存失敗、原状TLV差分、atomic journalと未復旧時の再実行拒否をhost testで検証した。Go modem/cmd tests・vet、CGO=0 Linux ARM64 build/test compileを実施。

次段はsessionの実機GET/Start/Stopと復旧確認、その後にLinux IPと自分専用のrouting table、IF/source-bound DNS/TLS、最後にAP共有への接続を別々に検証する。必要capの候補はroot限定のCAP_SYS_PTRACE（QMI所有者検査）とCAP_NET_ADMIN（netdev ioctlおよび次段の自分のIP/route設定）。device/private journal以外はread-only、AT/WDAやstock設定へwriteできる経路を公開しない。現時点で起動unitやWebからStartを有効化していない。

## 今後のTODOと統合境界

- 保存済profile 1をmux 0x82へ開始できるか、返るIPv4/mask/gateway/MTU/DNS、Stop後disconnected・全原状TLV一致を実機で確認する。現時点の実証はQMAP metadataだけである。
- Linux設定は `noprefixroute` 付きの自分のIP、専用tableと自分のsource /32に限定したruleを候補とし、main defaultを変更しない。既存のIF/IP/routeがある場合は上書きせず拒否し、自分が加えたものだけを撤去する。まだこの設定処理は実装していない。
- 自分のWDS FDが常時開くと、現在のNAS observerは意図どおり未知の所有者として拒否する。背景guardを無条件に緩めない。data-worker自身による固定NAS観測、またはpinned worker PID/start/FDを検証する専用strictGET経路が必要であり、未実装である。
- APの現cellular getterはmain defaultを要求する。将来は、ライブworkerのboot/PID/start/exe、freshness、同一hardware/ifindex/mux、同CID status、実IP/専用route、IF/source-bound DNS/TLSのproofをroot-only `/run/np1-control-cellular/private/state.json` へatomic保存し、opaque getterからAPへ渡す案である。このlive state/readerはまだ実装していない。今回の終了済probe journalをAPへ渡してreadyと扱わない。
- 純正CMが突然現れた時、SIGTERM/親EOF時、Start応答喪失時、kernel ioctl/closeが戻らない時の撤収を実機で検証する。ホストfixtureだけでfield復旧を合格にしない。

最終ホスト検証は `cd backend` で `go test ./...` と `go vet ./...`、CGO_ENABLED=0 / GOOS=linux / GOARCH=arm64で `go build ./...`・`go vet ./...`・`go test -c ./internal/modem`。固定shellは `bash -n`、独立probe bundleは `python tools/build_owned_cellular_probe.py` で生成する。これらは実機Startの検証ではない。
