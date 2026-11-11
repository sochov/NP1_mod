# 常時ホットスポットのインターネット共有

2026-11-11。LAN APに加え、実際に利用できる上流を選んでIPv4を共有する。常時テザリングが目的であり、管理画面専用APではない。

## 上流の判定

AUTOは検証済み4Gを優先し、利用できない場合は接続中のWi-Fi STAを使う。4Gの登録・PDP active・保存APNだけでは4G出口と判定しない。pinned EC25/GobiNetとQMAP親子関係、UP、実IPv4、default経路、前後のifindex、DNSの根拠が必要。未確認DNSをグローバルresolverから4G用と推測しない。Wi-Fiは純正wpa_supplicantのSTATUSがCOMPLETEDであり、アドレス、インターフェース指定route lookup、resolver、再読した接続identityが一致した場合に選ぶ。

画面には実選択に合わせて「4G経由」「Wi-Fi経由」「上流接続待ち」を表示する。`sharing.ready`は転送設定・DNS listenerのread-backであり、あらゆるインターネット宛先への到達保証ではない。実疎通の合格は外部クライアントのDNS/HTTPS試験で別に確認する。

## 変更範囲と復旧

- `nccap0`、専用hostapd、専用dnsmasqを使い、純正bridge/QCMAP/supplicant/設定ファイルは変更しない。AP SSID/PSKは既存のprivate設定を保つ。
- DHCPはAP自身をgateway/DNSとして配布する。dnsmasqはAPのIPv4だけでUDP/TCP53を待受し、`no-resolv`と専用servers-fileを使う。上流DNSを選択IFにbindし、変更時は所有PID/starttime/argvを検証したdnsmasqだけにSIGHUPを送る。キャッシュも再読時に消去する。
- `NCC_AP_FWD_V1`、`NCC_AP_DNS_V1`、`NCC_AP_NAT_V1`だけを更新する。base chainへは乱数token付きの限定jumpを追加し、既存chainのflush、ポリシー変更、全体restoreはしない。iptables-restoreは`--noflush`を使い、共有の標準xtables lockを使う。
- 転送はAP送信元subnetと選択egress、およびそのESTABLISHED/RELATED返信だけ許可する。その他のAP FORWARDはDROP。NATはAP subnetと選択egressだけのMASQUERADE。
- APから入ったAP subnetの通信だけ、priority 5000で専用table 427226へ送る。protocol 242の専用経路とblackhole defaultを置き、上流切断時に本体main/Tailscale経路へ流出しないようにする。stock table/ruleは変更しない。
- `runtime/private/sharing-owner.json`は0600。boot ID、token、変更前ip_forward、適用中/確定済みrule・routeを記録する。所有物と完全一致する場合だけ再適用・復旧する。iptablesのcanonical引数順や自動挿入protocol matchは意味単位で比較し、追加のmatch/ruleは許可しない。
- cleanupは自分のDHCP/hostapd停止後に、記録と一致するjump/chain/table/ruleだけ除去する。ip_forwardは自分が0→1へ変え、現在も1の場合だけ0へ戻す。元から1なら保持する。外部改変を検出した場合は破壊的なcleanupを拒否する。
- 上流欠測時はDNS転送先を空にし、FORWARD DROP/blackholeを保持する。AP association、DHCP、管理画面は維持し、上流復帰時に再構成する。IPv6 Internet共有とRAはまだ提供しない。

unitの書込例外は自身のruntime、標準`/run/xtables.lock`、`/proc/sys/net/ipv4/ip_forward`に限定する。lockはnamespace起動前にinstall/postbootで存在を保証する。単独の別lockfileへ逃がさない。

## 再現試験

`tools/ap-sharing-trial.py --serial ... --binary ... --out tmp/... --expect-upstream wifi --wan-recovery`は独立した/run unit/binaryを使い、インストール済み本体binaryを置換しない。稼働AP設定を読み、SSID/PSKはprivate試験ファイルだけに置く。元APは最後に復帰する。

Windowsは未接続Wi-Fiアダプタだけに一時profileを追加する。DHCPのgateway/DNS、APのHTTP/UIと観測station、AP宛UDP/TCP DNS、外向きHTTPSを確認する。PCのEthernet経路へ迂回した偽成功を防ぐため、HTTPSのsocketは`IP_UNICAST_IF`でWi-Fiを指定し、APの割当IPv4にもbindする。PCのdefault/host routeを変更せず、Windows SslStreamの標準証明書・hostname検証を使う。実装根拠は[MicrosoftのIPPROTO_IP socket options](https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ip-socket-options)。NP1のNATカウンタもprivateに記録する。

`--wan-recovery`は専用scriptで元STAのSTATUSをprivateに保存し、literal DISCONNECTを18秒だけ実行する。trapでRECONNECTし、元SSID/IDのCOMPLETEDをread-backする。純正network/設定ファイルは編集しない。AP HTTP連続到達、上流不在時の共有停止または実4Gへの遷移、元STA/profile集合、共有とDNS/HTTPS復帰を検証する。終了時は一時profile削除・PC切断、元AP、stock service PID/state、所有物以外のIPv4/IPv6 route/rule、filter/nat、ip_forwardを前後比較する。初めに試したunavailableSSIDのtyped transactionは純正が短時間で元STAへ再接続したため、継続的WAN不在の証拠として使っていない。

初回stageの所有判定はiptables引数順の正規化不足で停止した。固定した新binaryのjournal検証cleanupで全所有物を除去し、元APとstock状態の一致を確認した。Windowsの管理者権限が不要なsocket方式へ切り替えたため、試験のための昇格やPC経路変更は不要になった。

## 実機結果と残りの確認

Wi-Fi上流で、Windows AP WPA2 association、DHCP lease、gateway/DNSがAP自身であること、AP宛UDP/TCP名前解決、HTTP/UI、observed stationが合格した。IP_UNICAST_IFとAP割当送信元にbindした外部TCP接続・TLS証明書受信も確認でき、AP経由のデータ往復は実測済み。ただしPython OpenSSLの標準trust検証が失敗し、AP経由HTTPSの標準TLS検証は合格としていない。TLS検証を無効にする回避は行っていない。

その後、通常bundleを配備した実機でtools/verify-hotspot-internet.pyを実行し、Windows標準SslStreamのhostname/chain/revocation検証付きHTTPS200が合格した。IP_UNICAST_IF＋AP送信元bind、owned MASQUERADE +18 packets、AP/Web/broker PID不変を確認し、DHCP gateway/DNSとUDP/TCP名前解決も再確認した。PCのdefault/host routeは変更していない。上流は実Wi-Fi。4GはLinux出口の継続性とinterface/contextに紐づくDNSの確認が残り、4G共有は未確認。

18秒STA DISCONNECT時、workerはAP enabledとowned DHCPを維持し、sharingはwaiting-upstreamへ変わり、RECONNECT後にWi-Fi readyへ戻った。旧stageではWindows AP HTTPにtimeoutが複数回あった。worker enabledだけで到達成功とはしない。後続の通常配備で同操作を再試験し、下記のとおり通常HTTP到達は維持できた。切断中のread-only観測157 samplesでは4G default/CM出現はなく、4Gへ移行したとの証拠もない。

最終roundは元SSID/IDへのSTA復帰、専用unit停止・token/journal確認cleanup、元AP unit復帰、stock service PID/state、既存filter/nat、IPv4/IPv6 route/rule、ip_forwardの前後一致を確認して終了した。Web/API/broker unitはこの試験で再起動していない。ユーザーが利用中だったため一度切替試験を停止した。その後ユーザーが実機操作を明示再許可し、通常bundleの配備・共有再検証を行った。後続の通常bundle試験で共有と標準TLS HTTPSは合格したが、再起動後の共有成功にはまだ拡張しない。

通常配備後のtools/diagnose-ap-continuity.pyはAP/Web unitを再起動せず、DISCONNECT→18秒待機→RECONNECTを実行した。23 Windows samplesは通常HTTP（4秒期限）全成功・L2全Up。36 device samplesではAP ifindex同一、carrier=1/UP、hostapd ENABLED、AP TCP80 listener存続を確認し、AP/Web/broker PIDも不変だった。復帰後に標準TLS HTTPSとDNS、MASQUERADE +12 packetsを再確認した。

700ms期限のAP-bound直HTTP/UDPは3 samples失敗したが、通常HTTPは成功しており、短い無線経路遅延までゼロとはしない。scan/power-saveが原因との断定はしていない。STA carrier=0を捉えたのは2 samplesで、純正の自発復帰があるため「18秒連続STA切断」とは扱わない。旧stageで発生した数秒HTTP timeoutは、この通常配備roundでは再現していない。device側curlは未搭載でlocal HTTP比較は未取得だったため成功扱いにせず、後続用observerはminimal Python socketへ修正した（修正後の実機再試験は未実施）。

通常再起動後もAP自動復帰から、Windows標準TLS HTTPS、DHCP gateway/DNS、UDP/TCP名前解決、MASQUERADE増加（+28、再確認+36）を確認した。非切替試験中のAP/Web/broker PIDは不変だった。

## 端末別byte counterの制約

再起動後、同じPC stationをassociatedのまま通信終了後のworker更新を確認し、最大10秒待ってもhostapd報告のrx_bytes/tx_bytesは0だった。一方packet counterは正値で、同じstationのkernel iw station dumpにはbyte属性自体が無かった。stale snapshotではなく、hostapdが未提供属性をdefault0として表示している証拠として扱う。現機の端末別byte量を測定済み0と表示してはならない。

API/UIの任意receivedBytes/sentBytesは端末視点の接続中Wi-Fi量であり、kernelのbyte属性が存在する方向だけ有効にする。未提供は省略/null、実測0と区別する。回線請求量ではなく、管理画面等のローカル通信も含む。対応するUI/APIがあっても、現機で実byte量が得られたとは報告しない。

## 検証済みsystem DNSによる4G候補（sourceのみ）

回線が配布したDNSの取得とは別に、現在のsystem resolverを実4G IFとIPv4 sourceにbindして検査するsourceを追加した。設定を変更したり固定第三者DNSを強制したりせず、既存nameserverへの固定example.com A問合せが1秒の全体budget内で成功した場合だけ共有に採用する。UDP peer/local source、ランダムID、question、rcode、非空の有効AまたはCNAME→Aを検査し、無関係なA/誤ID/別peer/timeout/空回答は拒否する。前後のhardware/ifindex/IP/default/DNS/boot一致を要求する。

成功したresolver到達性だけをidentity固定で60秒cacheし、失敗は5秒backoffする。外部DNS問合せを2秒ごとに反復しない。identity変更で即無効化し、DNSレコードや実アドレスのTTLをこのcacheで上書きしない。API sourceはbound-system-dnsとし、carrier DNSと呼ばない。host UDP fixtures、cache/timeout/途中identity変更、全Go test/vet、arm64 buildは合格したが、実4G Linux出口上でのfield検証は未実施。
