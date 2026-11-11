# 機能と検証の対応表

2026-11-11。`実機確認`は記載した操作だけが対象。仕様P1全体の完了を意味しない。

| 機能 | 実装 / 実機確認 | 書込み・未解決事項 |
|---|---|---|
| HTTP/HTTPS UI | 80/443、パスワード不要、管理IF限定、PC/390px表示を実機確認 | 同一Wi-Fiクライアントからの直接到達は環境側の経路確認が必要 |
| Web基盤 | CSRF、Host/Origin検査、SSE、revision、操作履歴 | named account/RBACはネットワーク認証方式では提供しない |
| system | uptime、RAM、load、SoC温度、純正7 service | reboot/停止のWeb操作は無効 |
| Wi-Fi STA | vendor STATUSで接続状態・周波数・認証方式を取得 | scan/connect/saved AP変更は無効 |
| AP/AP+STA | iwがmanaged<=2/AP<=2/2channel/BI一致を報告 | 実動作・純正IPC・SSID/DHCP・競合回避が未検証 |
| LTE/APN | QCMAP所有・内部API symbolを棚卸し | SIM/登録/PDP/信号値の確定照会と変更は未検証 |
| traffic | /proc/net/dev、64bit boot累計の読取り | 日次/月次/再起動を跨ぐ永続集計・LTE IF特定は未実装 |
| clients | 未対応を明示 | DHCP lease・関連付け照会の調査待ち |
| SD | mount確認、ディレクトリ検索、限定root、安全なダウンロード | 最大300件、インデックス/ページング/録画中判定は未実装。書込みなし |
| recording | MP4 ftyp実確認、HTTP 206/Range/seek対応 | codec/ブラウザー可否は個別。イベント索引/同期/サムネイル未実装 |
| camera | front/in NV12から640x360 JPEGを実機確認 | 1fps・2分セッション。10fps/2stream負荷、H.264/WebRTC、音声未検証 |
| LED | sysfs存在を確認 | readがEIO。純正表示復旧が未検証のためwrite無効 |
| USB | peripheral実測 | 切替は代替経路・自動復帰未検証のため拒否 |
| Tailscale | 公式static arm64とdaemon動作、BackendState NeedsLogin | tailnet認証・Serve・遠隔到達は未検証 |
| GPS | 取得経路調査、ロガー既定OFF | GNSS IPC/live log/走行試験未完了 |
| radio map | Leafletローカルasset、独立凡例、空データ表示、固定地理セル集計とtrip代表値テスト | 収集・観測/動画同期・実データ描画/APIは未完了。グリッド表示操作はデータがない間は利用不可 |
| NAS | 候補と接続境界を整理 | 転送実装・NAS宛先/認証・AP継続転送経路の検証待ち |
| backup | 表示設定のexport/restore、dry-run、競合検査 | vendor設定やcredentialは対象外 |
| recovery | 明示marker、manifest、前版snapshot、rollback/uninstall | 初期失敗時の自動uninstallを確認。完全な反復・長期試験は別途 |
| P3 | MQTT/audio/ルール等は未対応 | 任意root shell APIは提供しない |

## 純正制御経路の根拠と制約

`libNDeviceLibCom.so` の `NWlanSystem::SetWlanAp` / `NWlanHandlerProxy::SetWlanAp` と関連event、Communication側のAP操作・APN操作symbolを確認。
`QCMAP_CLI` はAP起動だけでなくfactory reset/teardown/profile変更も含む対話ツール。
現状では初期化・終了時の副作用とC++オブジェクト/イベントABIが確定していないため、推測した引数で呼ばない。
QCMAPのsystem bus introspectionはAccess denied。別qmicliプロセスでモデム制御を奪わない。

AP制御を進めるには、純正アプリからの正常AP開始/終了の観測、または該当SDK/IPC資料が有用。
LTE疎通は使用可能な別SIM、NAS試験は宛先と認証、Tailscale遠隔試験は所有者のtailnet認証が必要。
