# Tailscaleの登録情報と名前変更

ダッシュボードとTailscale画面に登録機器名とアカウントを表示する。実行中CLIの`status --json`から、Self.UserIDに対応するUser entryだけを取得し、LoginNameとDisplayNameを使う。他のpeerの所有者を代用しない。UserIDはJSON numberを整数精度のまま扱う。

登録機器名はSelf.DNSNameの先頭label、ホスト名はSelf.HostName。通常は同じだが、管理コンソールの個別設定や名前重複で異なることがあるため分けて保持する。完全なDNS名、IP、接続状態もローカル観測値として扱う。アカウントや実機名は公開リポジトリのfixture・ログへ転記しない。

`PUT /api/v1/vpn/tailscale/name`は`{expectedHostName,hostName}`を受け付ける。新しい名前は1〜63文字の英小文字・数字・ハイフン、先頭/末尾は英数字。現在値のCASと登録node IDを照合し、固定コマンド`tailscale --socket=... set --hostname=NAME`だけを実行する。変更後のHostNameを再取得してから成功を返す。任意CLI引数、再ログイン、サービス再起動、OSホスト名の変更は行わない。再認証時にも既存の選択名を引き継ぐ。

Tailscaleの[公式機器名ドキュメント](https://tailscale.com/docs/concepts/machine-names)のCLI操作に従う。MagicDNSで登録名に追従する設定ではDNS名も変わる。管理コンソールで別名固定の場合はその差を表示し、ローカルのホスト名設定を登録名の確定と偽らない。

ホストテストは自分のUserID選択・整数精度・登録名/HostNameの差・名前制限・CAS競合・未反映・CLI失敗を検証。browser testは長いアカウント名、mobile/light/dark、適用中/競合/read-only、確認できない200応答を成功扱いしない点を検証する。実機は`tools/verify-tailscale-name.py`の一時名往復と原状復元で別途確認する。

2026-11-12、最新版を配備して実機名のWeb往復が合格。一時HostNameと登録機器名の両方の変更、所有者アカウントとIP維持、元のHostName/登録名への復元を確認した。登録名が自動追従する場合はその変更を待ってから成功を返し、直後のsummaryが古い値を返さないよう観測cacheを無効化する。`tailscale-hardware.spec.ts`でも本番APIの実値がダッシュボード・個別画面・編集欄へ表示されることをGETだけで確認した。fixtureの15browser試験・Go test/vetも合格。実値はignored tmpだけに保持する。
