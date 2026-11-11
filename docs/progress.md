# 開発状況

2026-11-11 開始。仕様 v1.2 に従って Sprint 0 → P0 → 読み取り機能の順に実装する。

## 運用ルール

- 実機変更は必ずリポジトリ内の再実行可能な sh スクリプトから実施する。
- 実行前に対象・影響・バックアップ・復旧・成功判定を記録する。
- docs_private、資格情報、個体識別子、位置・映像・実機生ログはコミットしない。
- 純正サービス、rootfs、raw block、SIM契約情報は変更しない。
- 未検証機能は有効化しない。実測と仮説を分ける。

## Sprint 0

- ローカル Git main を作成。docs_private の ignore を検証済み。
- root ADB、AArch64 / kernel 4.14.206、overlay noexec、SD vfat を確認。
- 純正 Communication / DriveRec / ImageRecog / Voice / QMMF は active、failed unit なし。
- STA 接続中、AP bridge は DOWN。AP+STA の同時動作は未検証。
- カメラ export は存在するがファイルサイズは 3,112,960 byte。先頭 3,110,400 byte の扱いを実測検証する。
- /dev/net/tun、qmicli、QCMAP CLI は存在。Tailscale は未導入。

## 完了条件

仕様の最終受け入れ表は docs/test-matrix.md で個別管理する。
24h/72h、移動中測位、別SIM疎通、Tailscale認証、NAS接続は外部条件が必要。実施していない試験を成功としない。
