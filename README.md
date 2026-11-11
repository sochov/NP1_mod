# NP1 Control Center

Pioneer NP1 のローカル管理画面。所有者の root ADB 環境向けに、純正機能を維持しながら段階的に開発しています。

**開発中。仕様全体の完成版ではありません。** 実装・実機検証の状況は [開発記録](docs/progress.md) を参照してください。

## 方針

- Go の静的 AArch64 バイナリと Vue / TypeScript の静的 UI。
- 非root Web/API と Unix socket 経由の限定 broker を分離。
- HTTP **80** / HTTPS **443**を併設。Webパスワードは不要で、ホットスポット認証・Tailscale ACLなどの管理ネットワークに接続制御を任せます。
- loopback・指定した管理IFだけへbindし、管理IFの80/443だけを専用firewall chainで許可。
- /overlay に保存し /run へコピーして起動。純正サービスを置換しません。
- 実機変更は tools のスクリプトで再現可能にし、バックアップと復旧手順を残します。

## 公開リポジトリ

`docs_private/`、`tmp/`、ダウンロードした資料・実機バイナリ・資格情報・映像は Git 対象外です。
GitHub への push は行っていません。サードパーティ製品との公式な提携はありません。

## ビルド・実機への配備

```powershell
powershell -ExecutionPolicy Bypass -File tools/build.ps1
python tools/deploy.py --serial SERIAL
python tools/connect.py --serial SERIAL
```

Linux/macOSは `sh tools/build.sh`。Node.js 24、Go 1.27.4、Python 3、ADBをホストで使用します。
UIとAPIは実機上でNode/Python常駐を必要としません。

ADB転送後は `http://127.0.0.1/`、同じ管理LANからは `http://<実機の管理IP>/` で開きます。
HTTPSも利用できます。カメラ映像は表示ボタンで直接開始し、閲覧中だけ取得します。
詳細は [配備・復旧](docs/recovery.md)、[構成](docs/architecture.md)、[対応状況](docs/capability-matrix.md) を参照してください。
