# Wi-Fi接続・管理AP画面

2026-11-11。Wi-Fi/APを専用画面へ統合し、接続端末の独立メニューを廃止した。旧`#clients`は`#wifi`へ移動する。モバイルではWi-Fi接続／管理APへの移動ボタンを用意する。

## STAの変更境界

既存純正wpa_supplicantの`/run/wifi/wpa_supplicant/wlan0`へUNIX datagramで接続する。新しいsupplicant、stock設定ファイルの直接編集、SAVE_CONFIG、再構成、daemon停止は行わない。`VendorStatus()`は互換のまま、private `vendorWPA(command)`をtyped controllerからのみ使用する。HTTPに任意commandを受け付ける入口はない。

[公式control interface](https://w1.fi/wpa_supplicant/devel/ctrl_iface_page.html)に沿い、LIST_NETWORKS、STATUS、SCAN/SCAN_RESULTS、ADD/SET/SELECT/REMOVE_NETWORKを使う。LIST_NETWORKSのcanonical headerは`network id / ssid / bssid / flags`、各データ行はtab区切り。SCAN要求直後は過去結果の場合があるため、UIは「直近の検出結果」と表示する。

新規networkは`id_str=ncc-<UUID>`、SSIDはUTF-8の1〜32bytesをhexへ変換し、制御文字を拒否する。対応認証はopenとWPA2 Personal。passphraseは8〜63 printable ASCII、または64hex PSK。WPAのPSK parserは最初と最後の引用符の間をそのままコピーするため、Go/JSON形式のbackslash escapeは追加しない。パスワードを変更してしまう誤escapeを避ける。根拠は[upstream config.c](https://w1.fi/cgit/hostap/plain/wpa_supplicant/config.c)の`wpa_config_parse_psk`（公式control説明もSET_NETWORKが設定ファイル形式を用いると明記）。NCCのrequest/reply/auditには秘密を出さず、username/passwordのGETも提供しない。

## 2段階transactionと復旧

1. 切替前STATUSのnetworkID、LISTの同ID/SSID、切替先、作成したownIDをprivate journalへ保存。
2. SELECT_NETWORK後はpendingとなり90秒タイマーを開始する。この段階ではNCC保存profileを作成しない。
3. ブラウザーで確定すると、COMPLETED・targetID・SSID・新規時id_strの一致を再取得し、private保存を行う。
4. 未確定、期限切れ、SELECT失敗、broker再起動時は呼出元HTTPから独立したcontextで元IDへSELECTする。元IDのSSIDも確認し、再利用された別networkを選択しない。旧接続のCOMPLETED状態までread-backしてから復旧完了とする。
5. REMOVEは今回追加したIDかつid_str一致の場合だけ。既存networkは削除しない。既に消えたownIDはLISTで不在を確認する。元ID復帰や所有権を確認できなければjournalを残し、失敗と表示する。

`RecoverWiFi()`はbroker起動時に呼び出される。再起動後の新規接続より先にjournalの復旧を試す。プロセス再起動とwpa自体の再起動でnetworkIDの永続性は保証されないため、ID/SSIDが変わったときは推測復元しない。NCC保存profileを起動時に接続するunitはroot統合担当であり、本controller単体で自動接続済みとはしない。

保存先は`/overlay/owner-apps/np1-control/private/wifi/`、directory0700、JSON0600。`profiles.json`には確定したSSID/security/secretだけを持ち、GETはpasswordConfiguredだけを返す。`pending.json`は期限・元/先ID・新規profileの秘密を含むため同じprivate保護を用い、確定・検証済み復旧時に削除する。最大32保存profile、128稼働network、256scan結果、入力/応答サイズ上限、単一JSON、symlink拒否を設ける。

## API

| Method/path（/api/v1省略） | 契約 |
|---|---|
| GET /network/wifi/networks | status、networks、saved、transaction。networks.idは一時的なwpa数値ID、saved.idはNCC UUID |
| POST /network/wifi/scan | `{}` → scanRequested/results/resultsAreCached。新scan完了の証明ではない |
| POST /network/wifi/connect-new | `{ssid,security:'open'|'wpa2',password}` → pending transaction |
| POST /network/wifi/connect-saved | `{networkID:number}` または `{profileID:string}` の排他的二択 |
| POST /network/wifi/confirm | `{transactionID}` → verified current state後confirmed |
| POST /network/wifi/rollback | `{transactionID}` → verified old state後rolled-back |
| GET /network/hotspot | APStatus Observation。settings、enabled、dhcpReady、channel、clients等 |
| PUT /network/hotspot | `{expectedRevision,settings:{enabled,ssid,passphrase?}}` → ApplyAPの独立read-back結果 |

Wi-Fi切替でHTTP経路が一時的に切れる場合は管理AP/Tailscale/USBへ戻る。UIは自動成功表示せず、再取得でpendingを継続表示する。APの変更は既存`ApplyAP`のprivate保存・専用unit restart・ENABLED/DHCP確認・失敗時旧設定復旧を使用する。インターネット共有は別段階であり、LAN管理用APの動作と区別する。

## AP接続端末

workerが**所有するhostapd**の`all_sta`を1秒以内で取得し、`[ASSOC]`のstationだけを現在接続中とする。`[AUTHORIZED]`は別bool。自分のdnsmasq leaseにある期限内MAC一致のIPv4/hostnameのみ補完する。leaseが残っているだけの端末を接続中に含めない。signal/connected_timeは取得できたときだけ表示する。

all_sta失敗/形式不明ならclients=null、clientsAvailable=false。独立したhostapd STATUSのnum_sta実測値がある場合、そのcountは保持する。詳細取得失敗でAP自体を停止しない。runtime statusは最大64KiB、clientsをpublic JSONへ二重掲載しない。per-station通信量の累計はまだ実装していない。

## 検証と未実測

typed controllerのfake transport testsは、秘密非公開、PSK文字列の保持、期限/取消/再起動復旧、ownID限定削除、旧接続read-back、競合/不正入力を検証する。HTTP testsは任意command入口が無いことを確認する。AP parser testsは関連付けとleaseを区別する。

4G7件（configured APN/current PDPの分離2件を含む）＋Wi-Fi/AP5件のPlaywright合成API試験、320/390px/desktopスクリーンショットを確認済み。root担当の実機Web試験では、既存接続先への接続確認/確定と、存在しないtest SSIDへの切替要求→取消→元ID/SSIDのCOMPLETED・network一覧完全復元を確認した。さらに独立AP fieldtrialで90秒自動復旧と手動復旧、未接続中のAP worker再起動、Windows AP HTTP連続119回の維持を確認した。private保存profileはこれらの試験では変更しない。新しい利用可能なSSIDへの接続確定/保存、再起動後の自動接続、clients詳細の全項目一致、NATは別途確認する。UI/単体試験だけを実機成功と表現しない。
