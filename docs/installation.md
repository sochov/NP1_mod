# 導入手順

## 前提

- 所有するNP1へroot ADBで接続できること。root化の手順や純正ファームウェアは本リポジトリに含めません。
- 対象はAArch64 Linux、systemd 239の環境です。ファームウェア202404231204で確認しています。
- ホストにNode.js 24、Go 1.26以降、Python 3、ADB、Gitを用意します。通常ビルドにPythonの追加パッケージは必要ありません。
- カメラ・録画・走行履歴には、読み取り可能なSDカードと関連する純正サービスが必要です。

## ビルドと配備

```powershell
git clone https://github.com/sochov/NP1_mod.git
cd NP1_mod
powershell -NoProfile -ExecutionPolicy Bypass -File tools/build.ps1
adb devices
adb -s SERIAL shell id
python tools/deploy.py --serial SERIAL
python tools/connect.py --serial SERIAL
```

`id`がrootであることを確認します。Goのパスを指定する場合は `tools/build.ps1 -Go PATH_TO_GO`、依存インストール済みの場合は `-SkipInstall` を使えます。Linux / macOSのビルドは `sh tools/build.sh` です。

ビルドはフロントエンド、Goのテストと静的検査、Linux ARM64バイナリ、ライセンス一覧、配備用manifestを生成します。成果物は `artifacts/ncc/` に置きます。配備はハッシュを確認し、変更前の復旧コピーを本体へ保存します。本体のNode.js常駐は不要ですが、導入・起動スクリプトは既存のPython 3を使用します。

## アクセス

| 経路 | URL |
| --- | --- |
| ADB転送 | `http://127.0.0.1/` / `https://127.0.0.1/` |
| 管理LAN | `http://本体の管理IP/` / `https://本体の管理IP/` |
| Tailscale登録後 | `http://本体のTailscale IP/` / `https://本体のTailscale IP/` |

標準ポート80 / 443を使います。ホストの同じポートが使用中なら、ADB転送前に競合を確認してください。HTTPSは自己署名証明書のためブラウザーで警告される場合があります。`tools/connect.py` の指紋はADB経由で取得します。Webパスワードはなく、管理ネットワークへ接続できる端末が利用できます。

## 初期状態

常時テザリング用の専用APと、管理Webの起動処理を追加します。Wi-Fi / AP画面でSSID・認証・DHCPと実際の上流を確認してください。AP設定の変更時には無線接続が一時切断する場合があります。

本体の自動アップデートは初回導入時に無効化します。システムの設定画面から有効に戻せますが、対応する純正ABIが変わる可能性があるため非推奨です。走行ロガーは動作し、有効な測位データが得られた分を記録します。NASの自動バックアップはOFFです。

## Tailscaleの任意導入

準備ツールは固定バージョンの公式AArch64配布物を取得し、公開SHA-256と照合します。

```powershell
python tools/prepare-tailscale.py
```

生成先 `artifacts/ncc-vpn/bin/` の `tailscale` と `tailscaled` を `artifacts/ncc/vpn/bin/` へコピーし、manifestを再作成して導入します。

```powershell
New-Item -ItemType Directory -Force artifacts/ncc/vpn/bin
Copy-Item artifacts/ncc-vpn/bin/tailscale,artifacts/ncc-vpn/bin/tailscaled artifacts/ncc/vpn/bin/
python tools/package.py artifacts/ncc
python tools/deploy.py --serial SERIAL --action install-vpn
```

WebのTailscale画面から認証を開始し、表示されるリンクで所有者のtailnetへ登録します。DNSの上書き・他ノードのルート受入・Tailscale SSH・subnet広告は初期設定で無効です。既存のADB listenerがある対応環境では、専用firewall設定がTailscaleインターフェースからの接続を許可します。ADB listenerを新設する機能ではありません。

## 純正連携ヘルパー

通常の公開ソースだけでWeb / brokerをビルドできます。純正設定の変更や一部の純正サービス起動には、別途ビルドする専用ヘルパーが必要です。ヘルパーのソースは `tools/native/`、ビルド入口は `tools/build-native-*.py` と `tools/build-stock-start-helper.py` です。

この追加ビルドにはClang / LLD、Node.js、pyelftoolsと、所有する対応端末のリンク用ファイルが必要です。必要なファイル名は各ビルドスクリプトに記載しています。取得したファイルは `resources/native-sysroot/` に置き、公開・再配布しないでください。生成済みヘルパーがある場合、通常ビルドが配備物へ取り込みます。端末の純正ライブラリは実行時に照合し、未知のファームウェアには適用しません。

更新や取り消しは [復旧手順](recovery.md) を参照してください。
