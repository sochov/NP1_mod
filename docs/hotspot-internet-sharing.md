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

`--wan-recovery`は既存typed Wi-Fi transactionで存在しないSSIDへ接続を試し、90秒未確定による自動rollbackを使う。AP HTTP連続到達、上流不在時の共有停止、元STA/profile集合、共有とDNS/HTTPS復帰を検証する。終了時は一時profile削除・PC切断、元AP、stock service PID/state、所有物以外のIPv4/IPv6 route/rule、filter/nat、ip_forwardを前後比較する。

初回stageの所有判定はiptables引数順の正規化不足で停止した。固定した新binaryのjournal検証cleanupで全所有物を除去し、元APとstock状態の一致を確認した。Windowsの管理者権限が不要なsocket方式へ切り替えたため、試験のための昇格やPC経路変更は不要になった。実疎通の最終結果は試験完了後に追記する。
