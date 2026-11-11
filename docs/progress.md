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

## 管理基盤・初回実機検証

- Go静的AArch64 + Vue/TypeScript。非root workerとpeer UID照合付きroot brokerを分離。
- ユーザー追加指定を優先しHTTP 80 / HTTPS 443、Webパスワード不要へ変更。管理IF限定bindと専用INPUT chainを実装。
- Tailscale公式arm64を導入し、所有者の登録後Running/Onlineを確認。実PCからtailnet経由のHTTP 200・ブラウザー操作・直接経路pingを実測。
- proc/sysfs、純正7 unit、STA STATUS、SD一覧/MP4 Range、front/in JPEG、表示設定/backup、SSE、監査を実装・実機検証。
- 白/グレーと中立ダーク配色、大きく太字のメニュー。左上ブランドとOwnerアイコンを削除。カメラの許可modalを廃止。
- NV12は純正H.264の色と一致。video-range変換とプレビュー限定の色補正を追加。純正録画にはH.264 1080p + AAC 48kHz stereoを実測。
- build/test/vet、npm audit、postbootテスト、1440/390px E2Eとカメラ直接開始をPASS。
- 配備のCRLF/legacy PTY、/etc rename ESTALE、純正INPUT DROPに対処。変更はすべてスクリプト化し、失敗時の自動撤去も確認。

## 継続中

- 提供されたLED gaming-modeとHR01 radio-mapを未改変でresources/referencesへ保管し、実装への取り込みを進める。
- 純正アプリは利用不可との回答。APのnative制御経路を継続調査。
- QCMAP_CLIはSIGTERMでDisableMobileAPを呼ぶことを逆アセンブルで確認したため実行しない。
- H.264ライブ共有・PulseAudio共存、GNSS/モデム観測、連続ロガー、NAS、長期/再起動試験は未完了。
