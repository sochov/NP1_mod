# 管理APの4Gインターネット共有・前提調査

2026-11-11。LAN APのDHCP待受とインターネット共有は別の検証対象。この時点でNATを実機へ追加していない。

## 実測と推測の境界

read-onlyでIPv4/IPv6アドレス、全tableのroute/rule、link/sysfs、既存iptablesを調査した。物理`usb0`とQMAP子`usb0.1`/`usb0.2`はすべてdownでIPv4アドレス/経路を持たず、defaultはSTAのみ。counter集計用の物理usb0をNAT出口と決めつけてはならない。既存QCMAPに子IFを出口とするNAT/policy設定はあるが、それだけで現在PDPの存在・有効APN・通信成功を証明しない。生データはprivate `tmp/ap-egress/`へ保存した。

GobiNetの物理祖先pinはEC25の管理device識別に使えるが、現在のPDP egressとは別。保存WDS profile1とGetProfileListの存在証明も現在のPDPではない。unbound CIDのGetCurrentSettingsがOutOfCallの場合はunknownのまま扱う。QCMAP owner側の候補は`GetIPv4NetworkConfiguration`（libqcmap_client `0x197a8`、IPC message`0x3e`、request0x18/response0x54）だが、まだ実機でowner read-backを確認していない。関連は[APN経路](communication-apn-path.md)、[profile recovery](apn-profile-recovery.md)を参照。

## 実装前に確定する値

- owner getterからの現在PDP/WAN IPv4、gateway、DNS。
- そのIPv4を実際に持つUPなQMAP IF、物理祖先が確認済みEC25であること。
- 接続中の4G向け経路。STAのdefaultや保存設定で代用しない。
- IPv4 forwardingの元値と、変更を必要とするか（現観測は既存側で有効）。

候補が不明・複数・down・IP不一致なら共有を開始せず、UIはLAN専用/4G接続待ちとする。PDPがまだ成立しない状態でSTAへNATして4G共有の成功と表示しない。

## 専用経路・firewallの設計要件

AP subnetは`192.168.226.0/24`。STAが本体defaultでもAP端末は確認済み4Gへ出すため、`iif nccap0`とsource subnetに限定したowner専用policy/tableを用いる。tableに経路がなくなった際に次のmain/STAへfallthroughしないよう、専用prohibit/blackhole guardを先に設置する。候補IFのdown/IP変更を監視して共有を止め、古い候補のまま転送を続けない。

iptablesはowner専用chainと厳密なjumpを用い、APから確認済みegressへの送信とそのESTABLISHED/RELATED返信だけを許可する。APから他IFへのFORWARDは終端で拒否する。MASQUERADEはsource subnet＋確認済み4G IFへ限定する。既存QCMAP/Tailscale/管理INPUT chainをflush/置換しない。IPv6共有は別途owner設定・prefix確認前には開始しない。

DHCPは現在router/DNSを配らないLAN専用。共有が検証できた場合だけ、AP gatewayとownerから得た有効DNSを配る。DNS serverや外部公開portを増やす必要はない。HTTP80/HTTPS443/DHCP67の管理境界は既存のままにする。

設定・専用rule/chain/tableの所有記録、変更前値、rollback、boot復帰、負荷試験、リンク切替試験を再現可能なscriptで実装してからroot担当が適用する。実際の4G PDPが成立しない現状態では、構成案をNAT動作の成功記録にしない。
