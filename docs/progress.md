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

## ダッシュボード・再生画面の改善と再起動確認

- ユーザー観点のレビューを3担当で分担。走行距離/走行時間を同一枠にし、7/30/90/365日切替、90日は週、365日は月集計。欠測と実測0を分離した。
- 通信量はNCCによる記録開始以降の受信＋送信。Wi-Fi/LTEを区別し、当月全体の通信量と誤認しない記録開始日時を表示する。
- 接続状態、GPS、CPU/RAM/温度、SDとアプリ保存領域の空き容量を整理。カーネルやファイルシステムはトップから除外。
- 録画ライブラリをドラレコ再生へ変更。確定済みMP4だけを索引し、日付・時間軸・録画区間から再生。純正録画のH.264/AACを再エンコードせず配信する。
- GNSSは純正録画のNMEA sidecarを読取り。有効時刻/測位のない実機データは距離0に変換せず欠測とする。過去の位置を現在位置として扱わない。
- LEDは提供参考に沿う5ゾーンの単色/レインボー/消灯を実装。短時間の実機試験でworkerのコマンド受理、更新、終了時消灯命令を確認。物理的点灯状態の読戻しは非対応。
- `/etc` overlayの永続upperとliveの不一致を修正。実機再起動1回でWeb 80/443、登録済みTailscale、カメラ/SD、純正サービスの自動復帰を確認。再起動後の実PCからtailnet経由HTTP 200も確認した。
- 320/390px、PC、ライト/ダーク、欠測/実測0/長いSSID/長期間グラフをブラウザー試験で確認。画面レビューの詳細は `usability-review.md`。
- 未解決: 純正所有ATポートを奪わずに取得するLTE観測経路、常時AP制御、継続位置ロガー、H.264ライブ/マイク、NASおよび24/72時間試験。

## 現状確認用UIの再配備

- 4GとWi-Fi STAの集計を分離。GobiNet物理親IFだけを4Gへ計上し、子IF・bridge・AP・Tailscaleを重複加算しない。専用の軽量カウンターAPIを2秒周期で採取する。
- 4Gを主表示し、設定可能な月間上限から記録分を引く残量ドーナツ、上下bps、Wi-Fi補助欄を追加。未設定/欠測/ゼロ/超過/更新停止を分離した。
- パンくず、ページタイトル、メニュー検索、OVERVIEWカテゴリと上部バーを削除。接続状態・Tailscale・再読込みを左下へ集約。リンクは右上ExternalLinkで統一。
- メニューはNETWORK / DRIVE RECORDER / NP1 SERVICE / MANAGE。SD、LED/音声、USB、ログ、設定/バックアップはシステムのタブに統合。旧画面hashもタブへ対応させる。
- SD使用率だけの警告は削除。残量・使用率は本体情報として中立表示。
- 原本再生、モバイル操作、通信上限/速度、欠測、設定競合などブラウザー17試験とGo tests/vet、実機SD/カメラ/純正サービス検証が合格。
- 独立NAS clientの単発Get Signal Infoが実機で成功し、純正6サービスのPID/state維持を確認。Web常駐observerは現在取得を確認できておらず、画面では未知のままとする。登録/PLMN/バンド追加照会はopt-in実装のみで実機未検証。
- 独立APの20秒試験はENABLED、後処理、純正PID/state、STA、IPv4/IPv6経路・rule維持まで合格。DHCP/クライアント接続/常時APはまだ未完了。
- NP1設定は純正共有mapの安全な読取りを解析中。現状配備UIは解析中・設定変更不可と明示し、仮の成功表示を出さない。
