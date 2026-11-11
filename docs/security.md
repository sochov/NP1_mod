# セキュリティと公開境界

Webパスワード不要・HTTP許可は所有者の明示指定。管理Wi-Fi/APの接続認証とTailscale ACLをアクセス制御の境界とする。
同じ管理ネットワークに接続できる相手は管理画面を利用できる。

CSRF cookieは自動発行する。HTTP用とHTTPS用を分離し、Origin・Host・request IDを検査する。
IP以外の未知Hostを拒否し、DNS rebindingによる操作を抑止する。CSP、nosniff、frame拒否も適用。
カメラは追加指示により許可画面を設けず、表示開始ボタンで直接開始する。内部セッションと所有ブラウザー照合は維持し、閲覧が止まれば失効する。

## broker境界

Webは任意rootコマンドを実行できない。socketは0600、peer UIDを照合する。
root brokerの通常観測はread-only sandbox。Tailscaleログインは引数なしの固定操作、LEDは型・範囲・revisionを限定する別allowlistを持つ。LED設定のroot専用フォルダーだけ書込みを許し、sysfs操作は専用workerで行う。
SD書込み、APN変更、USB role切替、純正service再起動、reboot、firmware操作のWeb APIは無効。

## GitHub公開前

`docs_private/`、原本実機ガイド、`tmp/`、`resources/`の材料、実機ログ、ビルド成果物、TLS/key、位置/映像を除外。
公開する実機記録は要約と状態・集計値に限定する。ダミーのテスト資格情報は実機で使用しない。
Tailscale auth URLとstateはバックアップや診断へ含めない。

既存の所有者recovery serviceやroot shellはNCCの管理対象外。今回それらの設定は変更していない。
