# NP1 Control Center 開発ルール

- 説明・作業記録は原則日本語。実装・検証・docs更新を一まとまりとしてコミットする。
- 最上位の追加指示: HTTP 80 / HTTPS 443を併設し、Webパスワードは要求しない。接続制御はホットスポット/Tailscaleの管理ネットワークに任せる。
- rootfs、dm-verity対象、raw block、純正バイナリ・サービス・設定を変更しない。
- 実機への変更は `tools/device/` の再現可能なスクリプトで行い、変更計画・バックアップ・復旧・read-backを記録する。
- serial、SSID、MAC、機器ID、位置、映像、credential、実機生dumpは `tmp/` / `resources/` / `docs_private/` に置き、Gitへ追加しない。
- 実機serialはCLI引数に渡し、ソースへ固定しない。未確認の制御をダミーで成功表示しない。
- プロセスのactiveだけで機能を合格にしない。テスト後に純正Communication/DriveRec/ImageRecog/Voice/QMMFを確認する。

ビルド: `sh tools/build.sh` または `powershell -File tools/build.ps1`。
単体テスト: frontend build後に `cd backend && go test ./... && go vet ./...`。
postbootテスト: `python -m unittest discover -s tests -p test_*.py -v`。
実機試験: `tools/connect.py` → `tools/hardware_verify.py --camera` → `cd frontend && npm run test:e2e`。
