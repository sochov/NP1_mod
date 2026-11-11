# 4G画面とAPN保存設定

4G専用画面をダッシュボードから分離し、回線登録・無線指標、APN編集、通信量補助欄の順に配置した。モバイルでは同じ順に縦へ並ぶ。登録済み/PS attachedだけではデータ接続を推定しない。現在APN/WAN IPv4は `apnCurrent.current=true` の観測値だけを使用する。SNRをSINRへ名称変更しない。欠測は0として扱わない。

## 保存と適用の境界

NCC保存先は `/overlay/owner-apps/np1-control/private/apn/config.json`。0600、最大8 KiB、regular fileのみ。公開GETはユーザー名・パスワードそのものを返さず、それぞれの設定有無だけを返す。プロファイル1/2の用途は未確定なのでprimary/fallback等と命名しない。

- GET `/network/cellular/apn`: revision、profiles、savedAt、appliedRevision、appliedAt。broker統合層がapplyAvailable/applyReason/applyInProgressを付加する。
- PUT 同URL: expectedRevisionと2個のprofiles。IDはprofile1/profile2固定、enabled、apn、auth、変更時だけusername/password。省略・空文字は保存済みcredentialを維持、clearUsername/clearPasswordで明示消去。保存だけではモデム/純正設定を変更しない。
- POST `/network/cellular/apn/apply`: expectedRevision。UIは verified=true かつ appliedRevision一致の場合のみ反映成功を表示する。applyAvailableは未指定時false。

`APNApplyProfiles(expected)` はroot限定helperに渡す秘密snapshotを取り出すだけで、実機変更しない。`MarkAPNApplied(expected)` は独立read-back後に統合層からだけ呼ぶ。保存revisionと適用revisionは別管理し、未適用を隠さない。APIにAPNSecretProfileを返さず、同型のcredentialはJSONタグでも除外する。

実機適用は純正ConnectMComAPNのIPC経路、適用前バックアップ、revision pin、管理経路維持、read-back/rollbackをroot担当が確認してから有効化する。ダミー成功やCSV保存だけを適用完了としない。本ファイルの単体試験は実機適用成功を意味しない。

## 値と入力制約の根拠

純正libNDeviceLibCom `NPMComSystem::ConnectMComAPN` (`0xa4668`) はAPN/user/passwordの各NStringを49文字以下、authを0〜3で検査する。liblteapi `0x34a0〜0x35a0` は整数をそのまま `AT+QICSGP=%d,1,"%s","%s","%s",%d` に渡す。文字列のエスケープはないためquote/backslash/制御文字を無条件に通せない。現在のNCC検証はAPNをDNS形式49 ASCII文字以下、credentialを49 ASCII文字以下の英数字と `._-@:+%=`（先頭ハイフン不可）に限定し、受け付けられない値を黙って書き換えない。

auth表示0=認証なし、1=PAP、2=CHAP、3=PAPまたはCHAPは、Quectel公式の[QICSGP説明](https://quectel.com/content/uploads/2021/03/Quectel_BG96_TCPIP_AT_Commands_Manual_V1.1.pdf)と純正の整数透過を根拠とする。NP1実機で各認証方式の接続成功を試験したという意味ではない。プロファイル構造/実機getterの範囲は [communication-apn-path.md](communication-apn-path.md) を参照。

## 検証

`go test ./internal/adapters -run APN -v` は秘密の公開JSON除外、private保存、revision競合、保存/適用の分離、危険文字、symlink/複数JSON拒否を検証する。`frontend/tests/cellular-page.spec.ts` は合成APIで保存≠適用、未確認結果のエラー保持、credential空欄維持、320/390pxレイアウトを検証する。映像/credential/実機生dumpを公開fixtureに入れない。
