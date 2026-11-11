# 実機変更計画 003: HTTP・Webパスワード不要

2026-11-11の追加指示を仕様より優先する。

- Webパスワードを不要にし、接続制御はホットスポット認証・Tailscale ACL等の管理ネットワークに任せる。
- HTTP 80 / HTTPS 443を併設。HTTP→HTTPSへの自動転送はしない。
- loopback、および明示した管理IF `wlan0` / `bridge0` / `tailscale0` のIPv4アドレスだけへbindする。アドレス変更は10秒周期で追従。WWANや0.0.0.0へのbindは行わない。
- 初回画面でパスワード入力を求めない。`GET /auth/session` は自動のCSRFセッション開始であり、ログイン処理ではない。
- CSRF/Origin/Host検査、カメラの個別同意と短期セッション、SD読み取り制限は維持する。
- HTTPではSecure属性なしの専用cookie、HTTPSではSecure専用cookie。いずれもHttpOnly/SameSite=Strict。
- 更新スクリプトの `--mode network-trust` で既存configからpassword hash/saltを除去し、管理IFの指定を保存する。以前の復旧コピーは復旧専用領域に保持。
- バックアップは更新前のconfigとバイナリ。復旧は通常のrollback。
- 実機INPUT policyはDROPだったため、専用`NCC_WEB_V1` chainで管理IFの80/443だけ許可するoneshot unitを追加。root default policyやNATは変更せず、uninstall時は自分のjump/chainだけを撤去する。
- 非root Webには低位ポート用CAP_NET_BIND_SERVICEだけを追加する。
- 成功判定: HTTP/HTTPS両方で直接ダッシュボード表示、LANの管理IPでHTTP取得、CSRFなしの変更拒否、カメラ許可なしの取得拒否、純正サービス維持。
