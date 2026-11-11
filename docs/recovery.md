# 配備と復旧

## ビルド

Node.js 24、Go 1.27.4（またはgo.modを満たす互換版）、Python 3をホストに用意。

```powershell
powershell -ExecutionPolicy Bypass -File tools/build.ps1
python tools/deploy.py --serial SERIAL
python tools/connect.py --serial SERIAL
```

Linux/macOSホストは `sh tools/build.sh`。ビルドはCGO=0 / linux arm64、公開物にnode_modulesやOS依存の実行環境を持ち込まない。
ホストから `http://127.0.0.1/`、同じ管理LANから `http://<management-IP>/` で開く。Webパスワード不要。HTTPSは443で併設。
接続先IPやserialはこの公開文書へ固定しない。

## 復旧

```text
python tools/deploy.py --serial SERIAL --action verify
python tools/deploy.py --serial SERIAL --action rollback
python tools/deploy.py --serial SERIAL --action uninstall
```

rollbackは直前のNCCバイナリ・config・unitだけを復元する。初回導入ならuninstall相当。
uninstallは専用unit、専用postboot block、専用firewall chain、今回作成したnccアカウントを撤去。
設定・復旧コピー・Tailscale state・材料は削除せず所有者用に残す。
再起動後も実機 `/overlay/owner-apps/np1-control/tools/` からshスクリプトを利用できる。

## VPN任意導入

`python tools/prepare-tailscale.py` で公式static arm64版をSHA-256照合して取得し、`artifacts/ncc/vpn/`へ配置、`python tools/package.py artifacts/ncc`でmanifestを再作成する。
`--action install-vpn` で導入後、インターネット画面から認証を開始する。tailnetログインは所有者が完了する必要がある。
認証前に遠隔到達を成功としない。DNS変更・route受入・SSH・subnet広告は無効。

## 検証

`python tools/hardware_verify.py --camera` はSDのRange、path traversal拒否、カメラJPEG、純正主要サービスを照合する。
`python tools/reboot-test.py --serial SERIAL --count 1` は単発再起動試験。10回試験はcount 10を指定する。
カメラ映像・位置情報を共有用レポートへ保存しない。
