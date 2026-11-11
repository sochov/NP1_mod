# ホットスポット上流：4G優先・Wi-Fi代替

2026-11-11。目的は常時テザリング。現sourceはAUTOで検証済み4G Linux出口を優先し、未成立の場合は実接続Wi-Fi STAを使う。STA共有を4G共有と表示しない。NAT/DNS/復旧は[共有設計・実機結果](hotspot-internet-sharing.md)を参照。

## 4Gの実測と推測の境界

初期調査ではusb0とQMAP子はdown・IPv4/経路なし、defaultはSTAのみ。その後、純正APN同値適用でCM稼働とQMAP子のIPv4/main defaultが一過性に成立したが、純正CommunicationのStopMCom/setLTELine(0)でCMが終了し、Linux出口は維持されなかった。PDP activeだけではLinux転送可能と判定しない。

4G proofはpinned EC25/GobiNetのUSB祖先、QMAP親子、UP、実IPv4、main default、ifindex前後一致を要求する。counter集計の物理usb0と実NAT出口は別。保存WDS profile1/GetProfileListも現在PDPの証明ではない。

4G DNSのinterface/contextに紐づく根拠は未確定。unbound CIDのGetCurrentSettingsがOutOfCallならunknown。global resolv.confを4G DNSと呼ばず、DNS未検証の4G候補は共有に選ばない。QCMAP owner候補はGetIPv4NetworkConfiguration（libqcmap_client 0x197a8、message0x3e）だがread-back未確認。関連は[APN経路](communication-apn-path.md)、[profile recovery](apn-profile-recovery.md)。

18秒STA DISCONNECT中のread-only観測157 samplesでは4G default出現0、CM出現0。4Gが自動で立つとは示せず、4G HTTPS試験は条件不成立で実行していない。

## Wi-Fi代替と転送境界

Wi-Fiは純正STATUSのCOMPLETED、実IPv4、wlan0を明示したroute lookup、上流DNS、再読した接続identityの一致で判定する。画面は「Wi-Fi経由」。4Gが未成立でも検証済みWi-Fiを共有することが正しいAUTO動作である。

AP入力とsource subnetだけをpriority 5000/table 427226へ送り、選択出口のconnected/default経路とblackholeを置く。main/stock table/ruleを変えず、上流切断時は他経路へfallthroughしない。専用MASQUERADE/FORWARD/DNS INPUTチェーン、token付きjump、0600 journalで所有・条件復元を管理する。IPv6 Internet共有/RAは行わない。

## 実機合否

Wi-Fi上流でWindowsのAP WPA2、DHCP、AP gateway/DNS、UDP/TCP名前解決、HTTP/UI、観測stationは合格。AP interfaceとAP送信元にbindした外部TCP接続・TLS証明書受信は、AP経由のデータ往復として確認済み。ただしPython OpenSSLの標準trust検証が失敗したため、AP経由HTTPS合格とは扱わない。その後、通常bundleを実機配備し、Windows標準SslStreamのhostname/chain/revocation検証付きAP経由HTTPS200を確認した。IP_UNICAST_IFとAP送信元bind、owned MASQUERADE +18 packets、AP/Web/broker PID不変で経路も確認。初期Python trust失敗の結果を成功に読み替えず、別の通常配備試験として合格を記録する。

旧stageのDISCONNECT操作ではAP HTTP timeoutがあった。通常配備後の再試験は23 client samplesのHTTP（4秒期限）全成功・Windows L2全Up。36 device samplesでAP ifindex/carrier/UP、hostapd ENABLED、HTTP listener存続を確認した。700ms期限のAP-bound直HTTP/UDPは3 samples失敗し、短い遅延は残る。scriptはDISCONNECT後18秒待ってRECONNECTするが、STA carrier=0の実観測は2 samplesだけであり、18秒連続切断とは表現しない。元STA同SSID/ID、元AP、stock PID/state、所有物以外のIPv4/IPv6 route/rule、filter/nat、ip_forwardの復帰は確認。一度ユーザー利用中に試験を停止し、その後の明示再許可で通常配備・再検証した。
