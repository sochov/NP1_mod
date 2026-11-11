# 受け入れ試験

各欄は2026-11-11の結果。ユーザー追加指示によりHTTP/Webパスワード不要を採用し、当初仕様のWeb認証必須とは分けて評価する。

| 対象 | 結果と根拠 |
|---|---|
| Go build / tests / vet | PASS。CSRF、Origin、Host、競合、dry-run、Range、HTML attachment、親参照/リンク拒否、電波集計など |
| Vue/TS build / npm audit | PASS。production auditで既知脆弱性0 |
| postboot script | PASS。複数回実行、他block保持、不一致marker拒否、永続upper/live更新の4テスト |
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
| AP常時維持 / SSID/DHCP | 未検証。純正開始/終了の制御と観測が必要 |
| LTE / 別SIM / APN | 未検証。安全な照会/適用経路と使用可能SIMが必要 |
| Tailscale | PASS。所有者tailnet登録後、実PCからHTTP 200、UI操作、直接経路pingを確認。再起動後も登録維持・HTTP 200 |
| GPS / 実走行 / radio map同期 | 未検証。収集・実データ地図連携が未完了 |
| NAS / AP+STA実動作 | 未検証。対象NAS・認証と管理APクライアントが必要 |
| 再起動 | PASS（1回）。専用reboot-test.pyでboot ID変更、自動復帰、Web/SD/カメラ/純正サービスを確認 |
| 24h / 72h / 10回再起動 | 未完了。単発の動作確認と混同しない |

電波集計は合成のテスト点で中央値、trip代表値、5秒時間差、欠測と圏外、境界値を検証したもの。
実機LTE観測が得られた証拠ではない。
