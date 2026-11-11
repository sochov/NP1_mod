# 受け入れ試験

各欄は2026-11-11の結果。ユーザー追加指示によりHTTP/Webパスワード不要を採用し、当初仕様のWeb認証必須とは分けて評価する。

| 対象 | 結果と根拠 |
|---|---|
| Go build / tests / vet | PASS。CSRF、Origin、Host、競合、dry-run、Range、HTML attachment、親参照/リンク拒否、電波集計など |
| Vue/TS build / npm audit | PASS。production auditで既知脆弱性0 |
| postboot script | PASS。複数回実行、他block保持、不一致marker拒否、永続upper/live更新、既知AP blockの移行を含む6テスト。Python全体20テスト合格 |
| AArch64実行 | PASS。CGO=0 static binaryの実機self-testとservice起動 |
| HTTP 80 / HTTPS 443 | PASS。両方のsession 200、UI shell、待受けを実測 |
| パスワード不要 | PASS。HTTPで入力欄なし・直接dashboard表示 |
| PC / mobile | PASS。1440px / 390px、横overflowなし、menu/SD/map操作、ブラウザー例外0 |
| 実機STA状態 | PASS。純正STATUSによる実測。インターネット疎通とは分離 |
| SD録画原本 | PASS。実ファイルのftyp、206、Range 100byteを確認 |
| SDパス制限 | PASS。実機で親参照/絶対パスを拒否、ホストでsymlink拒否と不正HTML attachmentを確認 |
| camera front / in | PASS。両方の640x360 JPEG。先頭画像の長さ・再open・閲覧中だけ更新するセッション。許可画面なし。画像内容はGitへ保存せず |
| stockサービス | PASS。主要7 unitのactiveを反復確認。10fps/長時間時の退行は未検証 |
| 設定バックアップ | PASS。表示設定のみ、秘密値なし、dry-run非更新、revision競合拒否 |
| 低位ポート/firewall | PASS。80/443空きを確認後、専用chainとCAP_NET_BIND_SERVICEだけ追加 |
| LANクライアント直結 | 未確認。このホストは別セグメントで、実機の管理IPへの直接TCP疎通なし。ADB転送ではPASS |
| AP常時維持 / SSID/DHCP | PASS。Windows WPA2/実DHCP/HTTP、associated一覧、STA検索中119 sample連続到達、通常再起動後のAP自動復帰。NAT/DNSの共有試験は別工程で継続 |
| LTE / 別SIM / APN | NASの登録/PLMN/バンド/信号を継続実測。WDS profile1の復旧保存/読取り、private APN編集/保存はPASS。同値native接続はLinux4Gが一過性に成立後、純正Communicationの明示Stopで終了する事象を解析中。別SIMと新APNの適用/接続確認は未完了 |
| Tailscale | PASS。所有者tailnet登録後、実PCからHTTP 200、UI操作、直接経路pingを確認。再起動後も登録維持・HTTP 200 |
| GPS / 実走行 / radio map同期 | 常時電波収集、現在GPSの独立cache、SD FIFO後の走行集計保持を実装・配備。collectorの再起動復帰を確認。有効GPSがないため実走行の精度・地図同期は未検証 |
| NAS / AP+STA実動作 | 未検証。対象NAS・認証と管理APクライアントが必要 |
| 再起動 | PASS（1回）。専用reboot-test.pyでboot ID変更、自動復帰、Web/SD/カメラ/純正サービスを確認 |
| 24h / 72h / 10回再起動 | 未完了。単発の動作確認と混同しない |

電波集計の中央値、trip代表値、5秒時間差、欠測と圏外、境界値は合成データで検証した。実機LTE信号の収集は別に確認しており、有効GPSと同時に取得した走行地図の検証とは区別する。

| 追加対象 | 結果と根拠 |
|---|---|
| LED連続操作 | PASS。消灯を含む実機9連続設定・同一worker PID・Result=success・元設定復元 |
| 画面連動LED | PASS。5領域の面積平均/色補正/反転/平滑・mobile操作。実機5RGB command state・12frame・TTL/明示停止の通常設定復帰。光学的な色読取は未実施 |
| 共有音声 | PASS。実AudioWorkletのdownmix/24kHz PCM16LE/200ms変換、実機PulseAudio primary専用streamと2秒idle終了。聴感は未確認 |
| 純正設定 | PASS。既存2+拡張9項目のWeb変更・純正保存値確認・元値復元。未確定enumや未検証writeは操作可能にしない |
| Alexa | PASS。Web ON→OFF→ON、Voice再照会と保存544一致。アカウント連携を推測表示しない |
| OTA抑止 | PASS。導入初期OFF、表示/API、再起動後のdisabled/managed/writable。実際のdownload起動試験は行わない |
| 全画面レビュー | PASS。20画面×1440/390px×light/dark=80画像、実機10画面GET限定レビュー。最新テザリングUI統合後の一括回帰は継続 |
