# APN取得と純正Communication制御経路

2026-11-11、実機から得たGit管理外バイナリの静的解析と、既存設定ファイルのread-only調査。ここに記した追加WDS照会・native IPC・設定反映は実機で実行していない。SIM番号、IMEI、credential、実際のAPN値は記録しない。

## 読取候補と現在の実装

固定 `/etc/data/mobileap_cfg.xml` にはIPv4/IPv6のUMTSプロファイルindexがあるが、APNそのものの要素はない。ここから接続中のAPNや有効なPDPを推測しない。`modem.ReadQCMAPProfileConfig()` はこのindexだけを返し、`current:false` と取得元を付ける。その他の設定・DDNS資格情報等は出力しない。

独立WDS CIDでのGet Current Settings（0x002d）は、現在のAPN/IP/profileを取得する候補。要求maskは0x109とし、この3項目だけを要求する。username・認証プロトコル・DNS等は要求しない。応答のAPNはTLV0x14、IPv4は0x1e、profileは0x1f。厳密な長さ・結果・重複検査後、型付きの3項目だけを返す。[公式WDS schema](https://github.com/linux-mobile-broadband/libqmi/blob/main/data/qmi-service-wds.json)、[公式request mask定義](https://github.com/linux-mobile-broadband/libqmi/blob/main/src/libqmi-glib/qmi-enums-wds.h)

`modem.ReadGobiNetAPN(ctx)` は固定service=1、transaction=4、Get0x002dのみを送る独立したopt-in関数。既存NAS workerへ暗黙に追加していない。Start/Stop Network、profile書込、Mux Bind、IP family書込などは一切ない。module版/hash、per-FD CID、所有者検査と通常closeを使用し、WDA(service0x1a)の特殊解放経路を避ける。新CIDが純正ECMのセッション情報を参照できるかは未検証で、out-of-callや省略項目は欠測のまま。IPv4はモデムの割当値であり、公開インターネット側のglobal IPや疎通成功とは別である。

## 純正のAPN設定所有者

静的に確認した呼出し列:

`Communication → NPMComSystem::ConnectMComAPN → NPMComHandlerProxy → NPMComHandler IPC → NEventWatcherCom → liblteapi::setAPNstatus`

| バイナリ/位置 | 根拠 |
| --- | --- |
| Communication 0x220d8c / 0x220f64 | NPMComSystem::ConnectMComAPNへAPN1/2情報を渡す |
| libNDeviceLibCom 0xa1ee8 | proxy ctorのhandler名はUTF16 NPMComHandler、type 0x0c |
| libNDeviceLibCom 0xa2a48 | GetMComAPNStatusはcommand0x0a、24byte要求 |
| libNDeviceLibCom 0xa2b08 | ConnectMComAPNはcommand0x0b、640byte要求 |
| NEventWatcherCom 0x90110 | liblteapi::setAPNstatusを呼ぶ |
| liblteapi 0x4168 / 0x34a0 | 2組のAPN/user/password/authを比較・格納し、AT+QICSGPを所有reader経由で送る |

Connect要求のAPN1領域は存在flag+0x18、UTF16 name/user/passwordが+0x1a/+0x7e/+0xe2、認証typeが+0x148。APN2はflag+0x14c、文字列+0x14e/+0x1b2/+0x216、認証type+0x27c。各文字列は50文字枠。これは解析上のABI情報であり、この要求を送信していない。

実際の接続/切断は所有者内で固定quectel-CMコマンドを起動する。setAPNstatusはprofile1/2を起動し、resetAPNは対応プロセスを終了して保持APN構造体をゼロクリアする。resetAPNを「変更前設定への復旧API」と解釈しない。設定失敗時の自動復旧には変更前2profileの確実なprivate backupまたは純正の既定復帰手順が必要。

## getterとイベントの範囲

`getAPNStatus`（liblteapi 0x64a0）はAT+CGACT?とAT+CGPADDRを送信し、APN1/2の状態intを返す。APN名・user/passwordを返すAPIではなく、別プロセスからライブラリを直呼びして所有ATを奪う用途には使わない。

調査したexported NPMComSystem/HandlerProxyのGet一覧、およびAPN status/connect/change result eventには、2profileの文字列設定を返すgetterは見つからなかった。APN状態eventは固定40byteのデータ構造で、文字列objectを含まない。全実装にgetterが存在しないと断定したものではない。

AIPI CCOMのRequestCCOMLTEStatus/SubscribeCCOMLTEStatusとReplyにはAPN1/2接続状態のgetterがある。一方、同じreply構造にICCIDやdevice passcode等も含むため、全体dumpは行わず、許可した状態getterだけを抽出する必要がある。

libNDeviceLibComはlibNStandardLibへ依存する。Native helperを追加する場合、既存setting helperの全4sem preflightと共有mutex所有権追跡を共通利用する。初回constructorによる共有資源cleanupや、timeoutで非robust mutexを保持したまま終了する事態を避けるため、単純なdlopen/ctor実行へ進まない。

## SDの純正APN override入力

CommunicationのReadAPNInfoFromFile（0x230268）が固定 `/mnt/sdcard/apnInfo.csv` をread-onlyで読む。DoActionReadApnFile（0x232148）が2行から2組のAPN設定を作り、内部APNInfoイベントをentityへ渡す。読み取られる形式はヘッダなし・CRLF区切りで、各行は `APN,user,password,auth` の4項目。authは0..3、純正parserは単純なカンマsplitであり、一般的なquote付きCSVではない。

CCOM_InCarConnectEntityのStart相当（0x22cd50）でFileProcRequest action3がenqueueされ（0x22ce48）、action dispatch（0x232e30）から読込へ進む。ファイル常時監視や公開reload要求は調査範囲では確認できていない。したがってSDへ置くだけで即時反映・即時復旧するとは主張しない。調査時点の実機SDにこのファイルはなかった。

`modem.ParseNativeAPNOverride(reader)` はこの入力のAPN/authだけを読取り、credential列を保持・出力しない。出力は `current:false`。保存・有効化・nativeイベント送信は行わない。

## 検証とfingerprint

Go test/vetとCGO0 Linux/ARM64 compileを合格。QCMAPの正確なXML経路、サイズ上限・重複・directive拒否、WDS request mask、APN/IP/profile型、IPv4 endian、異常長、未取得nil、credential TLV混在時の非出力、純正overrideのCRLF/サイズ/認証範囲とcredential非出力を検証した。実機追加query/open、純正停止、設定書込はしていない。

解析対象SHA256:

- Communication: aaa0d09d406a2b762e24b303bc138665d3da97523a07dcc338ce0ec4bec49c8b
- liblteapi.so.0: da4fbea491e90625202e851e90524745216ed652fe188b49ed41b3ed6c450245
- libNDeviceLibCom.so: 2b142e70f34103a59333808de36699cc064e602fae2524d960664c1f3ceeca71
- libqcmap_client.so.1: ff0aefd4308528154ffaff5e6dbb394d494dc170794e9e90392355cf5d25a5be
