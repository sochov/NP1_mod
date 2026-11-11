# 純正STA ownerと4G継続経路の調査

2026-11-11〜12。純正バイナリはGit管理外の参照材料として静的に読み、変更しない。実機のSSID/IP/PID、映像、APN/認証、生ログはここへ記載しない。正式STA ownerのGETと、短時間OFF→元ON復帰を段階的に検証した。

## 低層APN接続だけでは維持されない理由

同値profile1試験では、一時的に純正quectel-CM、GobiNet子IFのIPv4、4G defaultを取得した。その後、保存済みprivate journalに `OnMComApnChangeStatusResult → DisconnectMComAPN → OnMComApnDisconnectResult → StopMCom` の順序があり、Communication自身が切断を要求した。単なるCMの自然終了とは異なる。

静的な対応は次のとおり。

| 位置（Communication） | 挙動 |
|---|---|
| 21ac48 CLControl | SIM state2かつPhoneNum/ICCID非空のとき、desired Next(+e8)を判定 |
| 21ad18..21ae10 | Next1はAPN1、Next2はAPN2、Next0/3/4は切断側21a270へ |
| 21a270 / 21a574 | main connected状態ならAPN1 Disconnect要求8をenqueue |
| 21f428 / 21f518..524 | APN1切断成功result2でmain状態2へ、要求15をenqueue |
| 221054→22104c | 要求15はStopMComへ合流 |
| 21ae20 | 正規StartLTEMainはNext1を設定 |
| 21af60 / 21aff0 | StartLTESubはNext2、StopLTEMainはNext0 |
| 21b168 | PauseLTEMainはNext3を設定し元のNextも保持 |

純正NEventWatcherComのsetLTELine(action0)はliblteapi 3f98/402cへ入り、既存 `quectel-CM` をkillallする。resetAPNとは別経路。Web側がCM再起動loopで対抗したり、derived stateを偽装して解決したりしない。今回のNext実値はdebug log欠測のため、切断条件を一意に断定していない。

## STA状態40は実ownerの完了に追従する

Communicationのログliteral `AL_SRV_SET_ID_WIFI_STA_STATUS` と `STA ON / STA OFF` から、GetStatus ID40はSTA状態、保存値0=ON、1=OFFと確認した。PermissionFileReply(212518)等は40が0のときLTE開始を抑制し、非0だけStartLTEMainへ進む。wpa DISCONNECTでassociationだけを落とす操作は、この正式on/off状態を変えたことにはならない。

書込み側もWLAN state machineの完了event内にある。1d6418は受信結果と要求state7/8、action2/3を照合し、1d6558でSetStatus(40,0)、1d6620でSetStatus(40,1)する。1cf340は実STA状態code7と同期して0へ更新する。40だけの直接writeは実行しない。

正規StartLTEMainのservice側212358はInCarConnectのConnectPermissionも確認する。permissionはAL_FilePath index1の配下 `RW/CCOM/ConnectPermission.dat` の存在から得る（230798/2309b8）。permission fileを書き足して回避する実装はない。206f38 TelephonyServiceIFWrapperのinput0bも内部Start入口だが、外部IPCへの接続は確認できていないため、この番号を推測でD-Busへ送らない。

## 正式STAの固定FreeStyle protocol

Communicationの161c30 `CCOM_WifiService::OnAIPI_CommonRequestFreeStyleCommandEvent` はfunction0x500（WIFI_STATION_MODE）を扱う。function0x501はAP modeであり、STA要求には使わない。

埋込み `proto_STAmode.proto` のdescriptorはVA314360、長さ0x1762、登録関数1855a8の1855e8..f4にある。Google protobuf FileDescriptorProtoで実際にparseし、次を確認した。

| 要求 | command | 内容 |
|---|---|---|
| StaSettingInformationRequest | 1 | 空protobuf |
| StaSettingChangeRequest | 2 | field1 `setting`。1=SST_ON、2=SST_OFF |
| InformationResponse | 0x8001 | field1 result、2 error、3 setting、4 MAC文字列、5 IP文字列、6登録AP最大数int32 |
| ChangeResponse | 0x8002 | field1 result、2 error、3 setting |

ResultCodeはDEFAULT0/SUCCESS1/ERROR2、ErrorCodeはDEFAULT0/NONE1/UNKNOWN2/STA_SETTING3/SSID4/SECURITY_TYPE5/PASSWORD6。settingの0はDEFAULTでありONへ丸めない。操作成功にはresult1/error1、setting1または2と新規GET/40の一致が必要。

162214はcmd2をdecodeし、ON1→164e18の要求7と160090(2)、OFF2→162254の要求8と160090(3)へ進む。要求payloadは固定2byte `08 01` または `08 02`。SSID/PSK/network listの入力は受け取らない。

送信は既存AIPI_Commonの宛先 `Communication`、custom application ID NCCW(0x4e434357)、毎回のnonceを使う。応答はCommon reply event0x80001f02、Communication PID、名前、NCCW、nonce、function0x500、0x8001/8002をすべて照合する。MAC/IPは型・長さを検査してskipし、出力・保存しない。eventや空responseだけでは成功にしない。

## 再現toolと復帰

`tools/build_wifi_sta_probe.py` は独立 `artifacts/ncc-native-wifi-sta` を作る。既存Alexa/Setting helperと実機workerを置き換えない。`tools/trial_wifi_sta.py --serial SERIAL --mode preflight|get|roundtrip --records tmp/PRIVATE` が一時stageへ配備し、checked-in shell/Pythonからだけ操作する。defaultはpreflight。試験の実行はcoordinatorのnetwork排他windowで行う。

roundtripの前提は、元ownerON/40=0、STA COMPLETED、同SSID/networkID/IPv4、固有wlan0 default、純正7サービスとown5サービスのPID/state、AP自律IPv4 HTTPが確認できること。OFFは1回だけ送り、短時間のroute/IPv4/CM/worker状態をprivateに記録する。AP自律HTTPはNP1自身からの到達確認であり、外部WiFi端末の無線継続試験とは区別する。

shell exit/signal trapで `--restore-on` を呼ぶ。この操作は新規GETがOFFならON、既にONなら維持、unknownなら盲目的に変更しない。復帰後は同SSID/id/IPとON/40=0を15秒連続確認する。遅れてOFF完了が観測された場合だけ、最大2回のfresh GET/CAS ON復帰を行い、OFFは再送しない。応答timeoutが後続操作をcancelしたとは主張しない。

元wlan0 defaultだけが欠落した場合、同じ純正PID/owner starttime、STA profile/IP/prefix、競合default無しを再確認して記録済み1本だけをaddし、読み戻す。他のdefaultをreplace/flushしない。Setting.DATや純正ファイルのcopy-backも行わない。保存状態が戻らなければ失敗としてprivate証拠を残す。

host testsはroute grammar/注入拒否、同SSID/id/IP、foreign owner時no-write、遅延OFFからのON復帰、競合default非置換を確認した。C helperは実CをWasmで動かしてframe/nonce/owner/enum/metadata boundsを検証し、ARM64 ELFのvendor-free startupとguard exportsも確認する。

## preflightとGETの実機結果

最終helper SHA256 `edd2b2c1f572e223921a50b36f76b91bb118733c9e33b9c84edbe4fc40ffc5d1` を独立stageへ置き、preflight→GETを合格した。owner応答wire1=ON、native40=0が一致し、自分PIDのreceiverはcleanup matched/removed。constructor前、全送信前、相関応答後、cleanup後に指定Communication PIDのexe/starttimeも照合する版である。

純正7サービス、own5サービス、AP IPv4、全4sem/preflight値、既存SDK map headerの参照数は前後完全一致し、AP自律HTTPも通った。このGET段階ではWiFi on/off変更、route変更、サービス起動停止はしていない。

## 正式OFF→元ON復帰の実機結果

他のnetwork/native変更を止めた排他windowでroundtripを1回だけ行い、合格した。ON(wire1/40=0) → OFF(wire2/40=1) → 元ON(wire1/40=0)の全応答が相関し、各helperのreceiver cleanupも一致した。OFF要求の再送はしていない。

OFF中の6回の観測では、最初はdefault待ち、その後5回は純正CM、GobiNet子IF usb0.1のusable IPv4、同IFのmain defaultが同時に存在した。正規STA OFFが4G Linux経路の自動開始へ繋がることを実測した。外向きHTTPSやAP clientの4G Internet到達性、長時間継続はこの短時間試験では確認していない。

AP自律IPv4/HTTPは各観測で維持された。元ONへ戻した後は同SSID/id/IPと元defaultが自動復帰し、15秒のON継続監視も合格した。routeの手動addは不要だった。純正7/own5サービスのPID/state、SDK map参照数、4sem/preflight値、Setting.DAT全体は前後完全一致。modemは復帰後availableで、試験前より新しい実測signalを確認した。

次の課題は、正式OFFを使った4G外向き疎通とAP共有の実確認、および純正CMがQMI FDを保持している間の独立NAS/WDS GETを安全に許容する所有者検証。接続中の値を取得できない状態を成功や0へ丸めない。

## 30/45秒の協調試験準備

host CLIに `--hold-seconds 30|45`、`--off-ready-file tmp/PRIVATE/off-ready.json`、`--complete-file tmp/PRIVATE/sta-complete.json` を追加した。既定は従来相当の3秒。PC側のAP接続準備後に起動し、correlated OFFと40の一致後の固定markerだけで `offConfirmed/offAt/restoreDeadline` を発行する。restoreDeadlineはON復帰要求へ進む保持期限であり、元STA associationまでの復旧完了期限ではない。

OFF保持はmonotonicとSIGALRMで制限する。観測処理が遅い場合は自分の読取りchildをreapし、ON復帰へ進む。PC側のtraffic完了を無期限に待たない。元ON・同SSID/id/IP/default・15秒の安定監視・純正/own PID・AP・SDK照合をすべて満たしたときだけcompleteの `restored=true` を発行する。失敗はfalseであり、単なる子process終了を復旧完了にしない。

独立bundleにはmetadata-only CM証明modeを含め、最初にCMを見たときに1回だけ実行する。この版でNAS/WDS GETの併用許可や背景workerのguard緩和はしていない。deadline・alarm cleanup・phase偽装/重複/古い時刻・private出力範囲・失敗時falseをhost fixturesで検証し、C/Wasm/ELFguardと独立ARM64 buildも合格した。30/45秒の実機OFFとPCの4G共有試験はまだ未実施。

その後45秒の協調roundtripを1回合格し、元STA/stock PID/own PID/保存設定/APを復帰確認した。OFF中の4G AP client標準TLS/UDP・TCP DNSとcellular NAT増分も確認された（別担当）。CM metadataは固定argvの末尾NUL欠落をparserが拒否したため、この証明自体は失敗である。詳細と修正は `stock-cm-ownership.md` に記録する。

以後、ユーザーがWindows Wi-Fiの変更を禁止したため、このWindows AP client接続手順は再実行しない。次のSTA OFFはNP1だけの診断を明示windowで行い、Windowsの接続・切断・profile操作を伴わせない。新 `--cm-get-mode nas|profiles` は既定noneの明示限定probeで、元ON復帰と45秒期限を変えない。

静的調査は `tools/elf-disassemble.py`、`tools/elf-callers.py`、`tools/elf-address-refs.py` で再現できる。address-refsはADR/ADRP/ADD等の候補indexであり、制御フローの証明として単独利用しない。
