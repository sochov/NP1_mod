# 信頼境界

所有者の追加指定により、管理Wi-Fi/APの認証およびTailscale ACLの内側を信頼する。
管理ネットワーク上のクライアントはWebへパスワードなしでアクセスできる。

| 境界 | 対策 |
|---|---|
| LTE/WAN → Web | WWAN bindなし。管理IFだけの専用firewall許可。0.0.0.0待受けなし |
| 外部サイト → 管理ブラウザー | Host/Origin/SameSite/CSRF、CORS許可なし、CSP、フレーム拒否 |
| 信頼LAN → 映像 | 表示ボタンで直接開始。内部セッションとブラウザー照合、無閲覧時の失効 |
| SD内容 → ブラウザー | 限定root/リンク拒否、ftyp確認したMP4以外はattachment、nosniff |
| Web → root | Unix peer UID、固定operation、引数なし、読取りsandbox。一般shellや任意rootファイル操作なし |
| Web state → VPN秘密 | root所有private/tailscaleで分離、一般exportとログから除外 |
| 配備 → 純正 | 対象FWチェック、manifest、変更前snapshot、所有unit/block/chainのみ、read-back |
| 生ログ → GitHub | ignore、公開用要約、秘密/位置/映像のコミット禁止 |

実機の既存root recovery経路はNCCのアクセス制御対象外。NCCは純正/既存所有者サービスを停止しない。
rootまたは管理ホスト自体が侵害されている場合の防御は提供しない。
