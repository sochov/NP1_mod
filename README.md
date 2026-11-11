# NP1 Control Center

Pioneer NP1 のローカル管理画面。所有者の root ADB 環境向けに、純正機能を維持しながら段階的に開発しています。

**開発中。仕様全体の完成版ではありません。** 実装・実機検証の状況は [開発記録](docs/progress.md) を参照してください。

## 方針

- Go の静的 AArch64 バイナリと Vue / TypeScript の静的 UI。
- 非root Web/API と Unix socket 経由の限定 broker を分離。
- 初期 bind は loopback、HTTPS と所有者が作成した資格情報を使用。
- /overlay に保存し /run へコピーして起動。純正サービスを置換しません。
- 実機変更は tools のスクリプトで再現可能にし、バックアップと復旧手順を残します。

## 公開リポジトリ

`docs_private/`、`tmp/`、ダウンロードした資料・実機バイナリ・資格情報・映像は Git 対象外です。
GitHub への push は行っていません。サードパーティ製品との公式な提携はありません。
