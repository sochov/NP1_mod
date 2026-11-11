# 機能と検証の対応表

2026-11-12。`実機確認`は記載した操作だけが対象。仕様P1全体の完了を意味しない。後から指定されたWindows Wi-Fi変更禁止と、指定解析PCの内蔵uplink保護を優先する。

| 機能 | 実装 / 実機確認 | 書込み・未解決事項 |
|---|---|---|
| HTTP/HTTPS UI | 80/443、パスワード不要、管理IF限定、PC/390px表示を実機確認 | 同一Wi-Fiクライアントからの直接到達は環境側の経路確認が必要 |
| Web基盤 | CSRF、Host/Origin検査、SSE、revision、操作履歴 | named account/RBACはネットワーク認証方式では提供しない |
| system | uptime、CPU/RAM、保存領域、SoC温度、純正26サービスの個別起動/停止/再起動と操作前監査。OpeningのWeb往復確認、QMMF/AVとカメラ利用側の依存ガードを実機GET確認 | 全26サービスの機能別往復を確認した意味ではない。利用者によるQMMF停止後のAV crash/rebootを解析し、依存未停止時の操作を拒否 |
| Wi-Fi STA | vendor STATUS実測。typed scan/保存接続/新規接続、90秒確定/取消/再起動journal復旧、秘密非公開。既存接続の確定、存在しないSSID要求→取消→元COMPLETED・network一覧復元をWeb実機確認 | 利用可能な新規SSIDへの確定/保存と再起動自動接続は未確認。stock設定直接編集/SAVE_CONFIGなし |
| AP/AP+STA | 独自nccap0/own hostapd/DHCPの常駐・WPA2/DHCP/HTTP・通常再起動復帰を実機確認。Wi-Fi上流と正式STA OFF中の4G上流でUDP/TCP DNS・標準CA/hostname付きHTTPS200・MASQUERADE増加を確認。優先設定だけはAPを再起動せずhot反映 | 長時間、複数端末、failover、channel実切替、STA不在cold bootは残る。以後のクライアント試験は指定解析PCの空きUSBを使用。stock QCMAPは変更しない |
| LTE/APN | 独立NAS workerで登録/PLMN/バンド/RSRPを継続実測。CM稼働中のstrict metadata照合付き固定NAS GET3種類も実機成功。WDS profile1のprivate復旧保存、APN編集・秘密保持・revision保存、純正ownerのstatus/同値接続を確認 | 非同期適用・管理AP維持・欠落defaultだけの条件復旧を実装。CMありのprofile2不存在証明と新APN反映/接続/復元の実機確認後に適用を開放する |
| traffic | /proc/net/devの64bit値、NCC記録開始後の日次/月次差分を永続集計、reset/再起動境界のテスト | 完全なキャリア請求量ではない。default routeのIFを集計対象として明示 |
| clients | Wi-Fi/APへ統合。owned hostapdの関連付け端末に期限内own leaseを補完。実端末のDHCP/HTTP/associated検出を確認。端末別byteのAPI/UIと方向・64bit・欠測判定を実装 | driverが端末byte属性を提供せずhostapdが0を代用するため、現状のbyteは未取得と表示。独立集計は低優先で継続。leaseだけを接続中と数えない |
| SD | mount確認、ディレクトリ検索、限定root、symlink/親参照拒否、安全な原本取得を実機確認 | 汎用ファイル一覧は最大300件。SDへの書込み・削除・フォーマットなし |
| recording | JSTカレンダー・録画区間タイムライン・前方/車内/並列、確定MP4索引、SD交換ID失効。実機H.264 1920×1080/AAC48kHz2ch、先頭/末尾Range206・任意時刻seek・両カメラのフレームdecodeと再生時刻前進を確認 | 通常/Event/Parkingの既知名を索引化、確定待ち/不正MP4は再生対象外。ブラウザー対応codecに依存、並列同期は0.6秒の補正閾値。サムネイル/イベントの詳細解析は今後 |
| camera | SD書込み中の原本H.264を受動取得し再圧縮せずfMP4配信。実機ブラウザーで前方/車内1920×1080同時復号・切替・390px配置、HTTPで録画切替をまたぐ210秒同時配信・全復号を確認。JPEGの軽量表示も維持 | H.264は現状映像のみ。ブラウザーの長時間試験を継続。WebRTC、ライブAAC/マイクは未対応 |
| LED | 単色/レインボー/消灯、明るさ/速度、常駐workerとrevision。実機9連続変更・同一PID確認。ブラウザ画面の5領域平均RGB・色補正・範囲指定、実機frame反映と期限/停止復帰確認 | 物理点灯readbackなし。画面共有はHTTPS/localhostと対応ブラウザが必要 |
| 共有音声 | ブラウザ共有音声を24kHz mono PCMでNP1のPulseAudio primaryへ送る任意機能。合成音源でbrowser変換・実機stream受理・idle終了を確認 | マイク取得なし。共有可能対象はOS/ブラウザ依存。聴感確認は別途 |
| 純正設定/音声 | 固定setterとAlexaウェイクワード、消音/音量補正を実装。従来14設定+Alexaに加え、通知2項目・音声2項目も本番Web変更/保存/元値復元と33unit維持を確認 | master音量は現在値のみ。意味・範囲が未確定のnative44/62を推測で変更しない |
| USB | peripheral実測 | 切替は代替経路・自動復帰未検証のため拒否 |
| Tailscale | 公式static arm64、tailnet登録、遠隔HTTP/UI、再起動維持。account/登録名表示・Web機器名変更/元値復元、tailscale IF限定root ADBと再起動後疎通を確認 | WebへのServeプロキシは使わず管理IPへ直接bind |
| GPS | 純正NMEA sidecarの現在測位と履歴cacheを分離。7/30/90/365/全期間/カスタム、日/週/月の距離/時間棒と累計線、日別・走行別詳細。SD FIFO後も測定区間を永続保持。再起動後のcollector稼働確認 | 実機は現在有効GNSSなし。実走行の精度・365日・電源断/SD交換の実機確認は未完了 |
| radio map | 常時電波ロガー、期間/回線/バンド・指標切替、点/経路/グリッド、CSV/GeoJSON、背景地図とlight/dark。個別許可画面なし | 現状有効GPSなしで位置なし電波を蓄積。無効GPSを描画しない。実走行同期未確認 |
| NAS | 常時/緊急別ON/OFF、Wi-Fi/Tailscale別保存先、SSID/リアルタイム、自動キュー、SMB2/3・SFTP・WebDAV、全体照合・中断/再開を実装。Wi-FiとTailscale各約19MB原本保存、約242MB実中断/再開、SSID不一致抑止と一致時の約103MB自動保存を実機確認 | 実緊急録画、経路切断/再起動復旧は継続。保存階層・付随メタデータ・追加ポリシーも今後。詳細はnas-backup.md |
| backup | 表示設定のexport/restore、dry-run、競合検査 | vendor設定やcredentialは対象外 |
| recovery | marker、manifest、前版snapshot、rollback/uninstall、永続upper修正後の通常再起動PASS。Web/broker更新・AP更新を分離。読み取り専用24/72h監視を開始 | 10回再起動・24/72h完走・電源断/SD交換は未完了。監視は試用中の純正停止も正常時間の中断として記録 |
| P3 | MQTT/audio/ルール等は未対応 | 任意root shell APIは提供しない |

## 純正制御経路の根拠と制約

`libNDeviceLibCom.so` の `NWlanSystem::SetWlanAp` / `NWlanHandlerProxy::SetWlanAp` と関連event、Communication側のAP操作・APN操作symbolを確認。
`QCMAP_CLI` はAP起動だけでなくfactory reset/teardown/profile変更も含む対話ツール。
現状では初期化・終了時の副作用とC++オブジェクト/イベントABIが確定していないため、推測した引数で呼ばない。
QCMAPのsystem bus introspectionはAccess denied。別qmicliプロセスでモデム制御を奪わない。

AP制御を進めるには、純正アプリからの正常AP開始/終了の観測、または該当SDK/IPC資料が有用。
LTE疎通は使用可能な別SIM、NAS試験は宛先と認証が必要。Tailscaleは所有者登録と遠隔試験を完了した。
