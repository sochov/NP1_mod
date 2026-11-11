# 機能と検証の対応表

2026-11-11。`実機確認`は記載した操作だけが対象。仕様P1全体の完了を意味しない。

| 機能 | 実装 / 実機確認 | 書込み・未解決事項 |
|---|---|---|
| HTTP/HTTPS UI | 80/443、パスワード不要、管理IF限定、PC/390px表示を実機確認 | 同一Wi-Fiクライアントからの直接到達は環境側の経路確認が必要 |
| Web基盤 | CSRF、Host/Origin検査、SSE、revision、操作履歴 | named account/RBACはネットワーク認証方式では提供しない |
| system | uptime、CPU使用率、RAM容量/使用率、保存領域、SoC温度、純正7 service | reboot/停止のWeb操作は無効 |
| Wi-Fi STA | vendor STATUS実測。typed scan/保存接続/新規接続、90秒確定/取消/再起動journal復旧、秘密非公開。既存接続の確定、存在しないSSID要求→取消→元COMPLETED・network一覧復元をWeb実機確認 | 利用可能な新規SSIDへの確定/保存と再起動自動接続は未確認。stock設定直接編集/SAVE_CONFIGなし |
| AP/AP+STA | 独自nccap0/own hostapd/DHCP。常駐API有効化、Windows WPA2/DHCP/HTTP、30秒保持・13baseline一致を実機確認。STA SCANNING中119回のAP HTTP/station維持、90秒/手動復旧、STA不在worker再起動からのAP到達を実機確認。standalone既定有効 | 個別DISCONNECTED/ASSOCIATINGと新channel実切替、STA不在cold boot、NATは別途検証。旧QCMAPは変更しない |
| LTE/APN | 独立NAS workerで登録/PLMN/バンド/RSRPを継続実測。WDS profile1のprivate復旧保存、APN編集・秘密保持・revision保存。純正ownerのAPN statusと同値接続を実機検証中 | native変更時のLinux経路切替を含めた非同期適用・接続確認・元設定復旧を実装中 |
| traffic | /proc/net/devの64bit値、NCC記録開始後の日次/月次差分を永続集計、reset/再起動境界のテスト | 完全なキャリア請求量ではない。default routeのIFを集計対象として明示 |
| clients | Wi-Fi/AP画面へ統合。owned hostapd all_staのASSOC端末に期限内own leaseのIP/hostnameを補完。未知と実測0を分離 | 実端末の関連付け/離脱と一覧一致は未確認。leaseだけの端末は接続中と数えない。端末別通信量は未実装 |
| SD | mount確認、ディレクトリ検索、限定root、symlink/親参照拒否、安全な原本取得を実機確認 | 汎用ファイル一覧は最大300件。SDへの書込み・削除・フォーマットなし |
| recording | JSTカレンダー・録画区間タイムライン・前方/車内/並列、確定MP4索引、SD交換ID失効。実機H.264 1920×1080/AAC48kHz2ch、先頭/末尾Range206・任意時刻seek・両カメラのフレームdecodeと再生時刻前進を確認 | 通常/Event/Parkingの既知名を索引化、確定待ち/不正MP4は再生対象外。ブラウザー対応codecに依存、並列同期は0.6秒の補正閾値。サムネイル/イベントの詳細解析/書込中ライブH.264は未実装 |
| camera | front/in NV12から640x360 JPEGを実機確認、直接開始/停止、色調補正切替 | 1fps・閲覧中セッション。10fps/2stream負荷、H.264ライブ/WebRTC、マイク未検証 |
| LED | 単色/レインボー/消灯、明るさ/速度、常駐workerとrevision。実機9連続変更・同一PID確認。ブラウザ画面の5領域平均RGB・色補正・範囲指定、実機frame反映と期限/停止復帰確認 | 物理点灯readbackなし。画面共有はHTTPS/localhostと対応ブラウザが必要 |
| 共有音声 | ブラウザ共有音声を24kHz mono PCMでNP1のPulseAudio primaryへ送る任意機能。合成音源でbrowser変換・実機stream受理・idle終了を確認 | マイク取得なし。共有可能対象はOS/ブラウザ依存。聴感確認は別途 |
| USB | peripheral実測 | 切替は代替経路・自動復帰未検証のため拒否 |
| Tailscale | 公式static arm64、所有者tailnet登録、実PCからHTTPとUI操作・直接経路を確認、再起動維持 | WebへのServeプロキシは使わず管理IPへ直接bind |
| GPS | 純正NMEA sidecarの現在測位と履歴cacheを分離。7/30/90/365/全期間/カスタム、日/週/月の距離/時間棒と累計線、日別・走行別詳細。SD FIFO後も測定区間を永続保持。再起動後のcollector稼働確認 | 実機は現在有効GNSSなし。実走行の精度・365日・電源断/SD交換の実機確認は未完了 |
| radio map | 常時電波ロガー、期間/回線/バンド・指標切替、点/経路/グリッド、CSV/GeoJSON、背景地図とlight/dark。個別許可画面なし | 現状有効GPSなしで位置なし電波を蓄積。無効GPSを描画しない。実走行同期未確認 |
| NAS | 候補と接続境界を整理 | 転送実装・NAS宛先/認証・AP継続転送経路の検証待ち |
| backup | 表示設定のexport/restore、dry-run、競合検査 | vendor設定やcredentialは対象外 |
| recovery | 明示marker、manifest、前版snapshot、rollback/uninstall、永続upper修正後の実機再起動1回PASS | 初期失敗時の自動uninstallを確認。10回・24/72時間の長期試験は別途 |
| P3 | MQTT/audio/ルール等は未対応 | 任意root shell APIは提供しない |

## 純正制御経路の根拠と制約

`libNDeviceLibCom.so` の `NWlanSystem::SetWlanAp` / `NWlanHandlerProxy::SetWlanAp` と関連event、Communication側のAP操作・APN操作symbolを確認。
`QCMAP_CLI` はAP起動だけでなくfactory reset/teardown/profile変更も含む対話ツール。
現状では初期化・終了時の副作用とC++オブジェクト/イベントABIが確定していないため、推測した引数で呼ばない。
QCMAPのsystem bus introspectionはAccess denied。別qmicliプロセスでモデム制御を奪わない。

AP制御を進めるには、純正アプリからの正常AP開始/終了の観測、または該当SDK/IPC資料が有用。
LTE疎通は使用可能な別SIM、NAS試験は宛先と認証が必要。Tailscaleは所有者登録と遠隔試験を完了した。
