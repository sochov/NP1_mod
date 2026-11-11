# 正式STA OFF窓での4Gホットスポット検証

`tools/verify-cellular-hotspot.py` は、既存のWindows AP client検証と正式STA owner roundtripを連動する。実行者はネットワーク試験を排他しているcoordinatorに限る。source/fixtureが合格しただけで4G実疎通を合格にしない。

現在、ユーザー指示によりWindows Wi-Fiの接続・切断・設定・profile変更は禁止されている。この試験は再実行しない。関連する全Python入口は`--allow-windows-wifi-changes`、PowerShell直起動は`-AllowWindowsWiFiChanges`を明示しない限り、入力ファイルの読取り・ADB・出力作成より前に拒否する。これらのflagは将来ユーザーが明示許可した場合だけのopt-inであり、この会話では使用しない。以下は保存された手順で、現在の実行許可を示さない。

```text
python tools/verify-cellular-hotspot.py --serial SERIAL --out tmp/PRIVATE --hold-seconds 45
```

必要なのはroot USB ADB、HTTP80の既存ADB forward、`artifacts/ncc-native-wifi-sta` の検証済みstandalone bundle、未接続のWindows Wi-Fiアダプタ。アダプタ名が異なる場合は`--interface`で指定する。既存PC Wi-Fi接続を置き換えず、既存host routeも変更しない。

## 手順と証拠

1. STA ONのままPCの一時profileを追加しAPへ関連付ける。DHCP gateway/DNS、HTTP UI、observed stationを確認してpreparedを公開する。
2. prepared成立後だけ固定`trial_wifi_sta.py --mode roundtrip --hold-seconds 45`を起動する。任意shell commandや任意回線制御を受け取る引数はない。
3. 正式OFF callback/native値一致のoff-readyを待つ。offConfirmed/offAt/restoreDeadline/holdSecondsを検査し、期限内にfresh AP状態がreadyかつactual upstream=cellularになるまで待つ。
4. actual IFがusb0または検証対象QMAP子であること、専用MASQUERADE ruleがAP subnetとそのIFに一致することを確認して測定gateを開く。
5. PCはAP宛UDP/TCP DNSと、IP_UNICAST_IF＋AP割当送信元bindしたWindows標準TLS/hostname/chain/revocation検証付きHTTPSを実行する。測定直前・測定中・直後のactual cellularと期限、対象IFのNAT packet増加を確認する。希望preferenceだけで4G経由と判定しない。
6. traffic終了後もPCを関連付けたまま、正式controllerの復元完了を待つ。controllerはPCの終了を待たず固定OFF期限で元ONへ復帰し、元SSID/id/IP/defaultと純正・NCC PID/SDK/APの15秒安定確認を優先する。
7. completeのrestored=true後にPC cleanup gateを開き、作成した一時profileだけを削除して切断する。AP設定/PSK、保存NCC STA profileのhash、AP/Web/broker PIDも前後照合する。

coordinatorは回線制御として固定の正式STA trialだけを使う。AP設定、APN、main route、host route、保存PSKは変更しない。controllerが異常終了したり確認期限を超えたりしても、その場で強制killや別OFFを追加しない。復元が未確認なら結果をfailed/needs-reviewとして残す。

## phase契約

- PC prepared: prepared/association/dhcpGateway/dhcpDNS/httpUI/clientObservedが全てtrue。
- OFF ready: offConfirmed=true、offAt/restoreDeadlineはUTC付きtimestamp、holdSecondsは要求した30または45。
- Proceed: proceed=true、expectedUpstream=cellular、expiresAt。期限切れは通信成功へ進めない。
- Traffic complete: passed、upstream、開始/終了時刻、UDP/TCP DNS、standardTLSHTTPS、interfaceBound。off窓外・fallback・空proofは拒否する。
- STA complete: 正式controllerが全復旧証明に合格した場合だけrestored=true。
- PC cleanup: 上記restoration確認後に公開する。失敗時にもgateを解決し、PC profileの片付けを確認する。

raw account/SSID/PSK/IP/CM provenance/通信証拠はfreshな`tmp/PRIVATE`だけへ保存する。stdoutは成功・復元・確認項目のmetadataに限り、実機serialやcredentialを埋め込まない。

## host検証

PC準備前にwriterを起動しない、4Gが無い場合に測定しない、fallback/期限超過/不正proof/NAT無増加/復元失敗/PSK変化を成功にしない、測定対象IFとAP subnetが一致するruleだけを集計するfixturesが合格した。Windows変更の既定拒否、PowerShell直起動拒否、内部RealIO直接生成拒否、wrapperへのopt-in伝播もhost fixtureで確認した。拒否確認ではflagを渡さず、実機・Windowsネットワークを操作していない。

## 実機で確認済みの範囲

2026-11-12のroot coordinator実行で、正式STA OFF中のactual cellular、AP clientからのUDP/TCP DNS、IF/source bind＋標準証明書検証付きHTTPS、専用NAT packet増加（+11）、元STA復帰、PC一時profileの片付けが合格した。summaryはpassed/restored/pcCleaned=true。試験開始前と完了後のWindowsアダプタはDisconnectedだった。生証跡はprivateなtmp記録に保持する。以後、上記ユーザー禁止に従ってWindows変更を停止した。

今後NP1本体だけで実施するIF/source bind DNS/TLS probeは、NP1自身の4G出口確認として区別する。それだけでAP clientの転送・NATを再検証したとは扱わない。
