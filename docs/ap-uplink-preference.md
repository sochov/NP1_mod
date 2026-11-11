# ホットスポットの共有回線優先設定

2026-11-12、source実装。ユーザー指定により、サービス開始停止機能の反映・再起動後に配備と実機検証を行う。この変更を理由に実機のAP/STA/4Gを操作していない。

## APIと互換性

`GET /api/v1/network/hotspot` の `data.settings.preference` に `cellular`（4G優先）または `wifi`（Wi-Fi STA優先）を返す。保存configにfieldがない場合は従来どおり `cellular`。明示した空文字、未知値、大小文字違いは拒否する。

優先設定だけを変える要求:

```json
{"expectedRevision":4,"settings":{"preference":"wifi"}}
```

これは既存のenabled、SSID、WPA2 passphrase、standaloneFallbackを温存し、revisionを1つ進める。旧clientのenabled/ssid/passphraseを含む全体更新も受け付け、preference未指定なら現在の選択を維持する。passphrase未指定・空文字は従来どおり温存し、公開JSONに値を出さない。stale expectedRevisionでは書込み前にREVISION_CONFLICTを返す。Atomic保存の直後にprivate config全体を読み戻して一致を確認する。

`data.sharing.preference` はworkerが使用する優先設定。実際の転送元は `data.sharing.upstream.kind` で表し、fallbackを希望回線と混同しない。`wifi`希望で4G fallbackを使う場合も、actual kindは `cellular` のまま。両方不成立ならwaiting/ready=falseを返す。

## 選択と反映

選択順だけを変更し、候補の証拠を弱めない。

- `cellular`: freshなEC25/GobiNet/IPv4/defaultと検証済みDNSがある4Gを優先し、不可なら実接続STA。
- `wifi`: COMPLETED/実IPv4/route/DNS/接続identityを確認したSTAを優先し、不可なら検証済み4G。
- 両方不可・stale・DNS未確認は、既存のowned source-policy/blackholeを維持する。

優先候補がreadyなら、遅いfallbackの照会を行わない。設定は回線の利用順であり、純正STAやCMを開始停止して希望回線を強制的に作る操作ではない。main/stock route、system resolver、純正設定を変更しない。

SSID/enabled/passphrase等が同一でpreferenceだけが変わった場合、AP service・hostapd・DHCPをrestartしない。既存workerが2秒loopで新revisionを読み、上流選択とowned NAT/DNSだけを更新する。APのassociationを作り直さない。

保存APIは、新revision・preference・AP/DHCPのfreshなworker stateを最大8秒待つ。WAN自体が未成立でも、workerが希望を読み込みAP/DHCPを維持してwaitingを正しく返せば設定反映は確認できる。確認不能時は以前のconfigへ戻し、以前のrevision/preferenceも最大6秒hot read-backする。計14秒で、brokerの20秒HTTP期限に保存/read-backの余裕を残す。rollback自体が確認できない場合は別errorにし、service restartで隠さない。同じpreferenceを明示して保存する場合もrevisionを更新し、APをrestartしない。SSID/PSK/enabledを変更する通常更新は従来の制御経路を維持する。

## host検証

旧config欠落default、未知値拒否、preferenceだけの部分更新とsecret温存、stale revision拒否、private保存/read-back、旧全体要求と明示disable、radio変更のhot適用拒否、古いworker state拒否、以前revisionへのrollback適用、両優先順とfresh fallback、DNS未確認時の選択禁止をunit testで確認する。新優先設定の実機save/切替・AP client維持は、サービス制御の配備後に検証する。

## Web反映の実機検証tool

`python tools/verify-ap-preference.py --serial SERIAL --out tmp/PRIVATE_NEW_DIRECTORY` を使う。接続済み・認証済みでDHCP IPv4が判明するAP clientを最低1台必要とし、空のclient一覧を継続合格にしない。tool自体はPCのWi-Fi接続やNP1サービスを起動停止しない。

serialを明示した独立ADB forwardからHTTP80へ接続し、通常Web session/CSRFでpreferenceだけをwifi→cellular→originalの3回PUTする。同じ値の最後の保存もCAS revisionを進めてworkerにackさせる。AP/Web/broker PID、AP interface/hostapd/DHCPのownership、SSID/PSK/enabled、既存clientのassociationとIPv4 lease、sharing.preference/revision、現在の実上流wifi fallbackを照合する。

finallyでは自分が要求したrevision/valueと一致するときだけ原状preferenceへ戻す。成功応答が失われた場合も自分のexpectedRevision+1/valueを確認して復帰し、第三者revisionは上書きしない。証跡・config・leaseはfreshなtmp directoryへ保存し、stdoutは合否とbool/count/enumだけ。helperやdevice設定のパスは固定で、任意systemd操作や任意commandは受け取らない。作成した一時ADB forwardだけを終了時に削除する。

toolのhost fixturesでも3回のpartial PUT/CAS、secret非出力、応答喪失後の原状復帰、外部revisionのno-write、DHCP client欠測/重複拒否を確認した。
