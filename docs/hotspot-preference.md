# ホットスポットの優先回線

2026-11-12の修正後実機試験は合格。Windowsの実APクライアントを保持しながらwifi→cellular→originalの3回の部分PUTを行い、AP/hostapd/DHCP/Web/brokerのidentity、SSID/PSK、既知クライアントのDHCP接続、原状復帰を確認した。AP IF/送信元を固定した標準TLS HTTPS200とNAT packet +30も確認。実上流はWi-Fiであり、4G共有実証とは区別する。DHCP未取得の別associated端末にはIPを補わず、既知IPv4の正のlease証拠と全associationを分けて検査した。

実行例：`python tools/verify-hotspot-internet.py --serial SERIAL --out tmp/FRESH_DIRECTORY --preference-roundtrip`。独立した設定試験は `tools/verify-ap-preference.py`。既存接続を持つPCのWi-Fiを勝手に切り替えず、テスト用に未接続のadapterだけを使う。終了時は一時profileを撤去し、設定はCASで元へ戻す。

上記は過去の手順。現在はユーザー指定でWindows Wi-Fiの全変更を禁止し、関連入口も既定拒否にしている。[Windows試験制約](windows-wifi-test-policy.md)に従い再実行しない。以後のAP client試験は解析PCの未使用USBだけを別途確認して使う。

Wi-Fi/AP画面のホットスポット欄で「4Gを優先」「Wi-Fiを優先」を選べる。Wi-FiはNP1自身の接続先（STA）を指す。優先側を利用できない場合は他方を使い、両方が不成立でもホットスポットのLAN/DHCPを維持する。

保存した優先設定と、実際の共有上流は別に表示する。たとえば4G優先でも、現時点で4G Linux出口/DNSを検証できずWi-Fiを使っている場合は「現在の共有 Wi-Fi経由」と表示する。希望した回線を使用中と見せない。未取得の設定をfrontendだけで4G優先と決めつけず、backendから有効なpreferenceを受け取るまで操作を無効にする。

## 契約と保存

- GET /network/hotspot: data.settings.preferenceはcellularまたはwifi。旧private configのfield省略はbackendでcellularへ移行する。data.sharing.preferenceはworker側の優先設定、data.sharing.upstream.kindは実上流。
- PUT同URL: expectedRevisionとsettingsを受け、優先回線単独ならsettingsはpreferenceだけを送る。SSID/enabled/PSKは温存する。
- 優先回線だけの変更はAP/Webを再起動せず、既存workerのloopで反映しrevision/read-backを確認する。SSIDやAP有効状態等の変更は従来の適用経路を使う。

frontendは優先回線用の別操作として扱い、未保存のSSID/パスワードdraftも維持する。適用中は関連するAP設定を無効にし、settingsとsharingのpreferenceが要求値と一致する応答を受けた場合だけ保存確認を表示する。409や未確認200を成功表示しない。

## 検証範囲

frontend buildとpreference8件＋既存Wi-Fi/AP9件のbrowser testsが合格。部分PUT、未保存AP draft温存、希望とactual fallbackの区別、適用中、競合/未確認、設定欠測、320px light/390px darkを確認し、画面を目視した。実機配備・優先設定の往復とAP PID維持、4G/STAの両方が利用可能な場合の実上流切替はhost fixturesだけで合格にはしない。

## 両回線接続時の選択レビュー

`readAPPreferredUpstream`はcellular優先なら4G getterを先に呼び、fresh IPv4/route/DNSの証拠が揃えばSTAを選択しない。選択したIFだけを専用source-policy default/NAT/FORWARD/DNSへ使い、hostの通常defaultがSTAでもそのdefaultへ迂回しない。追加`ap_preference_review_test.go`で両ready時のcellular/default/wifi選択からplanまでを確認し、両接続proofの不変も検証した。

これは両回線のLinux出口が成立した場合の選択規則の確認。純正CommunicationがSTA接続時に4G CM/PDPを停止する状態は、4G優先を保存するだけでは解決しない。STAを切断して4Gを成立させる手法は「両方を接続したまま4G共有」の完成条件にしない。正式なdual-WAN維持制御と同時接続状態での実AP client疎通は別途実証が必要。
