# セキュリティと公開境界

Webパスワード不要・HTTP許可は所有者の明示指定。管理Wi-Fi/APの接続認証とTailscale ACLをアクセス制御の境界とする。
同じ管理ネットワークに接続できる相手は管理画面を利用できる。

CSRF cookieは自動発行する。HTTP用とHTTPS用を分離し、Origin・Host・request IDを検査する。
IP以外の未知Hostを拒否し、DNS rebindingによる操作を抑止する。CSP、nosniff、frame拒否も適用。
カメラは追加指示により許可画面を設けず、表示開始ボタンで直接開始する。内部セッションと所有ブラウザー照合は維持し、閲覧が止まれば失効する。

## broker境界

Webは任意rootコマンドを実行できない。socketは0600、peer UIDを照合する。
root brokerの観測対象はread-onlyで、変更は型・範囲・revisionを限定した個別APIへ分離する。privateのLED/APN/Wi-Fi/AP/OTA管理記録と純正OTA抑止flagディレクトリだけ書込みを許す。LED sysfsとAPのNET_ADMINは専用workerが扱う。純正設定はroot peerだけ受け付ける別socket/workerで、固定mapping/hashと変更・保存・復元を検証済みの11項目を変更する。Alexaも専用の要求/応答経路を使う。Webや通常brokerへvictoria共有mapの書込みを許さない。
APNはprivate保存までで、実適用は配送/復旧経路を調査中。SD書込み、USB role切替、任意の純正service再起動、reboot、firmware payload適用のWeb APIは無効。OTAは純正の新規取得抑止flagだけを管理し、進行中処理を中断しない。

## GitHub公開前

`docs_private/`、原本実機ガイド、`tmp/`、`resources/`の材料、実機ログ、ビルド成果物、TLS/key、位置/映像を除外。
公開する実機記録は要約と状態・集計値に限定する。ダミーのテスト資格情報は実機で使用しない。
Tailscale auth URLとstateはバックアップや診断へ含めない。

画面連動のRGBフレームと音声PCMはRAM内の短期sessionで扱い、画像・音声を永続保存しない。追加の独自許可画面は設けない。画面取得はブラウザ標準の共有選択を使い、共有音声の任意出力もブラウザが返した音声trackだけを利用する。

既存の所有者recovery serviceやroot shellはNCCの管理対象外。ADB5556の既存listenerは変更せず、所有INPUT chainだけでtailscale0からの到達を許可した。
