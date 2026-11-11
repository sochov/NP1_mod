# 実機変更計画 002: 任意Tailscale導入

- 公式static arm64 1.104.6、公開SHA-256照合。第三者バイナリはGit対象外。
- `tools/prepare-tailscale.py`で取得し、NCC bundleの`vpn/`に配置してmanifestを再作成。
- `tools/device/install-vpn.sh`で`/overlay/owner-apps/np1-control/vpn/`とroot専用`state/tailscale/`に配置。
- `/run/np1-tailscale/`にコピー。専用unitと専用postboot識別blockを追加し、/dev/net/tunの通常kernel modeで起動する。
- 認証前は未接続と表示。ログインは所有者の明示操作。DNS上書き・route受入・SSH・subnet広告は無効、netfilter操作も無効とする。
- tailscale0作成・Tailscale専用経路は追加され得る。純正STA/APとdefault routeは変更しない。
- 認証state/keyとauth URLをGit、診断、一般ログに含めない。
- 成功判定はdaemon active、TUN interface、実測BackendState、純正service・STA経路の維持。tailnet認証前の遠隔到達は未検証。
- 復旧は専用service停止・vpn-enabled解除。全体uninstallもVPNを停止するが、秘密stateは保持する。
- 既存Webはloopbackだけにbindし続ける。Tailscale ServeのHTTPS経路は認証後に別途検証する。
