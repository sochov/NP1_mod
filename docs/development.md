# 開発・検証

## ディレクトリ

| パス | 内容 |
| --- | --- |
| `frontend/` | Vue / TypeScript UIとPlaywrightテスト |
| `backend/` | GoのWeb / broker / worker、単体テスト |
| `systemd/` | 配備する専用unit |
| `tools/` | ビルド・配備・検証、`device/` の実機スクリプト、`native/` の独自ヘルパー |
| `tests/` | ホスト側Pythonテスト |
| `docs/` | 公開用の利用・構成・API文書 |
| `licenses/` | OSSの原文・NOTICEと固定依存の一覧 |
| `artifacts/` | ビルド成果物。Git対象外 |
| `resources/` | 取得資料や端末由来のリンク材料。Git対象外 |
| `tmp/` / `docs_private/` | 生データ、一時出力、内部の作業・調査記録。Git対象外 |

## ホストでの確認

GitHub ActionsではUbuntu 24.04、Go 1.27.4、Node.js 24、Python 3.11でビルド・Go/Pythonテスト・ARM64クロスビルドと公開検査を実行します。解析用テストも実行するため、protobuf 5.29.1をCIに導入します。シェルの検証にはBashとPowerShell（Linuxでは`pwsh`）を使用し、CIの開始時に実行環境を確認します。権限を検査するテストの一時保存先は、ホストのumaskに依存せず0700で用意します。

```powershell
cd frontend
npm ci
npm run build
npx playwright install chromium
npm run test:e2e
cd ../backend
go test ./...
go vet ./...
cd ..
python -m unittest discover -s tests -p "test_*.py" -v
python tools/third-party-licenses.py
```

Playwrightのfixture試験にはローカルのUIが必要です。別ターミナルで `cd frontend` → `npm run dev -- --port 5177` を起動し、試験側の `NCC_BASE_URL` と `NCC_UI_REVIEW_URL` を `http://127.0.0.1:5177/` に設定してください。実機の映像や変更操作を伴う試験は環境変数で明示的に有効化する方式です。各specの先頭で必要条件を確認してください。

GNSSの期限付き診断は `python tools/inspect-gnss.py --help` を参照してください。位置や識別子を含む詳細出力はGit対象外のディレクトリへ保存します。実機で値が得られない機能を、fixtureの合格だけで実機確認済みにはしません。

通常のARM64配備物は `tools/build.ps1` または `tools/build.sh` で生成します。追加の純正ヘルパーを必要とする場合は [導入手順](installation.md) を参照してください。

## 公開前の確認

```powershell
python tools/check-public-tree.py
python tools/third-party-licenses.py
python tools/check-doc-links.py
git diff --check
```

秘密値との照合には `tools/check-public-tree.py --known-private-values PATH` を使用できます。指定ファイルはGit対象外に置くJSON文字列配列で、検査結果に値そのものを表示しません。この検査は既知の値と公開対象パスを確認するもので、未知の秘密情報まで自動で保証するものではありません。

変更をstageした後に `python tools/verify-public-build.py --go PATH_TO_GO` を実行すると、Git indexだけを隔離ディレクトリへ展開し、端末由来のファイルや非公開資料なしで通常ビルドできることを確認します。公開スクリーンショットは `frontend/tests/readme-screenshot.spec.ts` の架空データから生成しています。

依存関係の変更時には [サードパーティライセンス](../THIRD_PARTY_LICENSES.md) の手順で一覧と原文を更新してください。純正ファームウェア、ライブラリ、SDKヘッダー、逆コンパイル結果、実機録画・位置、資格情報は追加しません。Issueには再現手順・ビルド識別子・公開可能なエラーの要約を記載してください。
