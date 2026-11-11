# 実機変更計画 001: ローカル管理基盤

2026-11-11。ユーザーの開発・実機検証指示に基づく。実行コードは `tools/device/` に保存。

## 対象と影響

- `/overlay/owner-apps/np1-control/`: 実行バイナリ、TLS/Argon2設定、表示設定、監査ログ、復旧コピー。
- `/etc/systemd/system/np1-control{,-broker}.service`: 所有者用の2 unitのみ追加。
- `/etc/init.post_boot.sh`: `BEGIN/END NP1_CONTROL_CENTER_V1` の識別ブロックのみ追加。他の既存blockは保持。
- `/etc/passwd`, `/etc/group`, 関連shadow: `/nonexistent:/bin/false` の専用システムユーザー ncc を追加。
- `/run/np1-control/`: 揮発実行先とUnix socket。
- `/tmp/ncc-stage/`: 検証済み配備bundle。
- 既存ネットワーク、純正サービス、録画、LED、USB role、SDファイルは変更しない。
- Webは `127.0.0.1:8443` のHTTPSのみ。ホストではADB forward経由で利用。

## バックアップと復旧

インストーラーはpostboot原本とSHA-256、既存NCCバイナリ・unit・config（存在時）、純正service状態を `recovery/<UTC>/` に保存。
途中失敗はNCCだけをrollbackする。初回のrollbackはuninstallであり、state/configは所有者用に保持。
`tools/deploy.py --action rollback` または `--action uninstall` で再現する。
全体のpostboot原本で上書きせず、自分のblockだけを削除する。

## 成功判定

- ELF self-test、manifest SHA-256、sh構文が一致。
- 非root worker、0600 Unix socket、peer UID照合。
- 未認証API/カメラ/SDは401、HTTPS UI shellは200。
- 認証後に実機情報とSD一覧・Rangeが取得できる。
- 既存exportからJPEGを取得し、純正Communication/DriveRec/ImageRecog/Voice/QMMFがactiveを維持。
- 1回の再起動でlate-boot起動、ログイン、USB ADB復帰を検証。
- 24h/72hと10回再起動は別の耐久試験であり、この計画の単発試験と混同しない。
