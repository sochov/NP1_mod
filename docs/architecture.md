# アーキテクチャ

## アクセス方式

追加のユーザー指示を優先し、Webパスワードは不要。HTTP 80とHTTPS 443を並列提供する。
管理IFのIPだけにbindし、所有者専用iptables chainで同じIFの80/443のみ許可する。
WAN直接公開・0.0.0.0 bind・NAT/default route変更はしない。

## プロセス

| プロセス | 権限と責務 |
|---|---|
| ncc server | 専用nccユーザー。HTTP/HTTPS、静的Vue UI、CSRFセッション、設定、監査。CAP_NET_BIND_SERVICEのみ |
| ncc broker | root、必要capabilityのみ。固定の観測、SD読み取り、既存カメラ出力、引数なしTailscaleログイン。Unix socketのpeer UIDを検証 |
| firewall oneshot | 所有chainの追加・削除のみ。CAP_NET_ADMIN/RAW。通常時はプロセスなし |
| optional tailscaled | 独立unit。TUN通常モード。stateはrootのみ、Webパスワードとは独立したtailnet認証 |

永続ELFはoverlay（noexec）に保管し、broker開始前に/runへコピーする。
overlay上の実行スクリプトは/bin/shで解釈。純正プロセスへ組み込まない。

## データ

- proc/sysfs/systemctl: 10秒キャッシュ、出所・UTC取得時刻・状態・更新周期を保持。
- Wi-Fi: 純正supplicantへの固定STATUSのみ。返信用socketの権限をvendor groupに限定。
- SD: mount確認、os.OpenRootの範囲内だけをopen。親ディレクトリ・symlink拒否、読み取り専用。MP4のftyp確認後だけinline、その他はattachment/octet-stream。
- カメラ: リクエスト時だけatomic-replace済みexportを再openし、video-range NV12先頭画像を640x360 JPEGへ変換。表示開始で自動セッションを生成、閲覧中更新・無閲覧30秒で失効。許可modalなし。自動色調補正はプレビュー限定、元映像へ切替可能。録音・録画処理なし。
- 設定: JSON、0600、temp/fsync/rename。expectedRevisionで競合拒否。バックアップには表示設定だけを含める。
- 監査: 許可済みaction/resultのみ。1ファイル512KiB+旧世代1つ。本文・URL・credential・映像・位置は記録しない。
- 電波品質: 指標別の凡例と固定地理メッシュ集計ライブラリ。実機観測がない間は地図を空にする。第三者タイル接続は既定なし。

## 実機固有の注意

- systemd 239とlate /etc overlayに合わせ、自分のpostboot blockからdaemon-reload/startする。
- /etcのrenameがESTALEになる個体では、事前コピー・同時変更チェック・fsync・live view read-back付きのin-place更新へ限定フォールバックする。電源断時の原子性は保証できないため、復旧コピーを保持する。
- ADBはlegacy PTYのみ対応。echoを無効化したハンドシェイク後にstdinを渡し、shell終了コードの代わりに明示的な結果markerも検証する。通常インストールにパスワードは不要。
- 入手した純正ELFは公開・同梱しない。内部C++symbolの存在だけを安全なABI仕様とみなさない。
