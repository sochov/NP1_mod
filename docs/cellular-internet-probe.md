# NP1自身の4G出口検証

`adapters.ProbeCellularInternet(ctx)` はNP1本体からの固定宛先疎通を確認する。Windows・解析PCの接続設定を操作せず、NP1のAPN/STA/route/firewall設定も変更しない。正式STA OFF等の回線状態変更はこの関数に含まれず、必要なら別の許可済みcontrollerが排他的に実施する。

## 検証内容

1. pinned USB/GobiNet物理IFとQMAP親子、稼働IPv4、main default、ifindex、boot ID、実system resolver、そのresolverへのIF/source指定routeをfreshに読み取る。
2. DNS socketを対象IFとIPv4送信元へbindし、`example.com.`のAを問い合わせる。Go resolverがtransaction/question/recordを解析する。IPv4以外、private/loopback/link-local/multicast/CGNAT/予約・文書用rangeの応答は拒否する。
3. TCPも`SO_BINDTODEVICE`と送信元IPv4へbindし、実socketの送受信endpointを照合する。宛先はDNSで得た公開IPv4の443に限定する。
4. Go標準system CAと`example.com`のhostnameを厳格に検証し、TLS 1.2以上でHTTPS GETを実行する。Proxyを使用せず、redirectを追わず、HTTP 200だけを合格とする。応答読取りは32 KiB、全処理は最大8秒。CA欠落・証明書エラー・期限切れは失敗で、検証回避しない。
5. hardware/IF/index/source/prefix/default gateway/DNS/bootを再読し、前後一致と処理期限を確認する。成功でも生IP・DNS・boot ID・証明書は公開結果へ含めない。

戻り値は`CellularInternetProbeResult`と安定したerror code。`status=passed`にはDNS/TLS/HTTP/identityの全証拠が必要。途中の成功flagがtrueでも全体statusがfailedなら疎通合格にしない。任意URL/host/portを受け取るUI/APIは設けない。

## 統合と検証状況

中央CLIへの推奨mode名は`cellular-internet-probe`。rootがmainへ配線し、専用の正式STA OFF窓で呼び出す。関数本体はネットワーク状態を書き換えないため復元処理を持たず、回線controllerの期限・元ON復帰を妨げない。

host testではloopback UDP/TLS serverを使用し、標準証明書検証の成功・未信頼CA・hostname不一致、redirect/非200、DNS ID不一致/空回答/timeout、非公開A、IF/index/source/gateway/DNS/boot変化、socket送信元不一致を確認した。Windows Wi-Fiや実機には操作していない。実機CA trust store・4G上流での本probeは未試験。

このprobeが示すのは**NP1自身の4G Linux出口**。AP clientからの転送・DHCP・DNS配布・NAT counter増加の再証明には使わない。過去の実AP clientによる4G DNS/TLS/NAT試験は[別記録](cellular-hotspot-trial.md)として扱う。
