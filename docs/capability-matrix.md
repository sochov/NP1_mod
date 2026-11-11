# 機能と検証の対応表

2026-11-11。`実機確認`は記載した操作だけが対象。仕様P1全体の完了を意味しない。

| 機能 | 実装 / 実機確認 | 書込み・未解決事項 |
|---|---|---|
| HTTP/HTTPS UI | 80/443、パスワード不要、管理IF限定、PC/390px表示を実機確認 | 同一Wi-Fiクライアントからの直接到達は環境側の経路確認が必要 |
| Web基盤 | CSRF、Host/Origin検査、SSE、revision、操作履歴 | named account/RBACはネットワーク認証方式では提供しない |
| system | uptime、CPU使用率、RAM容量/使用率、保存領域、SoC温度、純正7 service | reboot/停止のWeb操作は無効 |
| Wi-Fi STA | vendor STATUSで接続状態・周波数・認証方式を取得 | scan/connect/saved AP変更は無効 |
| AP/AP+STA | iwがmanaged<=2/AP<=2/2channel/BI一致を報告 | 実動作・純正IPC・SSID/DHCP・競合回避が未検証 |
| LTE/APN | QCMAP所有・内部API symbolを棚卸し | SIM/登録/PDP/信号値の確定照会と変更は未検証 |
| traffic | /proc/net/devの64bit値、NCC記録開始後の日次/月次差分を永続集計、reset/再起動境界のテスト | 完全なキャリア請求量ではない。default routeのIFを集計対象として明示 |
| clients | 未対応を明示 | DHCP lease・関連付け照会の調査待ち |
| SD | mount確認、ディレクトリ検索、限定root、symlink/親参照拒否、安全な原本取得を実機確認 | 汎用ファイル一覧は最大300件。SDへの書込み・削除・フォーマットなし |
| recording | JSTカレンダー・録画区間タイムライン・前方/車内/並列、確定MP4索引、SD交換ID失効。実機H.264 1920×1080/AAC48kHz2ch、先頭/末尾Range206・任意時刻seek・両カメラのフレームdecodeと再生時刻前進を確認 | 通常/Event/Parkingの既知名を索引化、確定待ち/不正MP4は再生対象外。ブラウザー対応codecに依存、並列同期は0.6秒の補正閾値。サムネイル/イベントの詳細解析/書込中ライブH.264は未実装 |
| camera | front/in NV12から640x360 JPEGを実機確認、直接開始/停止、色調補正切替 | 1fps・閲覧中セッション。10fps/2stream負荷、H.264ライブ/WebRTC、マイク未検証 |
| LED | 参考実装に沿う単色/レインボー/消灯、明るさ/速度、専用workerとrevision・失敗復旧 | 実機で命令受理・更新・消灯処理確認。物理点灯readbackなし。停止は純正表示復元ではなく消灯 |
| USB | peripheral実測 | 切替は代替経路・自動復帰未検証のため拒否 |
| Tailscale | 公式static arm64、所有者tailnet登録、実PCからHTTPとUI操作・直接経路を確認、再起動維持 | WebへのServeプロキシは使わず管理IPへ直接bind |
| GPS | 純正NMEA sidecarを安全に索引、7/30/90/365日距離/走行時間、欠測と実測0を分離 | 実機は無効測位・時刻欠測。ライブGNSS/継続ロガー/実走行未完了 |
| radio map | 期間/回線/バンド・指標切替、点/経路/グリッド、履歴API、CSV/GeoJSON、外部背景地図は明示操作のみ | LTE/GNSS同時実測がまだなく本番観測は未収集。GPS無効を描画しない。実走行同期未確認 |
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
