# 本体OTA取得方針の導入・復元

ユーザー要件「導入時に本体自動更新をOFF、設定から再有効化（非推奨）」の実装。通常の純正設定ファイルを推測編集せず、純正Updateが明示的に消費する固定presence flagだけを管理する。rootfs/raw block、署名・更新payload、純正バイナリ・サービスは変更しない。この担当は実機変更を実行しておらず、以下は実機担当が実施する手順である。

## 消費の静的根拠

対象Update SHA256は `567f4a5a28f1989facd1f0d56ddd9a5e5391e7fbbf171dd6fa1166998b95f779`。GetDownloadList `0x9fcf0` はAL_FilePath GetPartition(kind1)に `/SYSTEM/Update/no_ota_download` を追加する。対応AL_FilePath実装のkind1は `/pio_data`。`0x9fe04` のSearchFileがregular fileを表す1なら `0xa019c` へ分岐し、HTTP取得 `0xa0044` に到達せず、result eventをpostしてfalseを返す。file内容は読まない。method(AUTO/manual)による分岐より前に検査するため、正確な表示は「新しいOTA更新データの取得を停止」とする。

このflagは既に始まったdownload、verify、activateを中断しない。音声・ナビ・一般cloud通信を包括遮断せず、UpdateやAppManagerのstop/maskも行わない。firmware UpdateとlibAplLibのSHAをcoreで照合し、未知版は変更しない。

## 変更・backup・復旧

- 操作対象は `/pio_data/SYSTEM/Update/no_ota_download` のみ。ディレクトリは既存かつsymlinkではないことを確認し、純正ディレクトリを新規作成しない。
- owner記録は `/overlay/owner-apps/np1-control/private/ota-policy/ledger.json`、directory0700/file0600。初回installで元の存在、最大4096byteの全内容、mode、UID/GIDを保存する。公開応答に内容やowner IDを含めない。
- 元flagが存在すればinstallではそのfileを変更しない。存在しなければO_CREATE|O_EXCLで空のflagを作り、regular fileの存在と内容/metadataをread-backする。
- 管理記録は現在fileのdev/inode/mtime/data/mode/ownerも追跡する。set/enable/rollback/uninstallの前に一致を要求し、未知の既存flagや第三者が差し替えたflagを名前だけで削除しない。全操作を専用flockで直列化する。純正/他root actorを同じlockへ強制するものではないため、外部変更を検出した場合はconflictとして操作を止める。
- 再install時は管理中のユーザー設定を維持する。Webから意図的に再有効化した設定をupgradeで勝手に無効へ戻さない。
- rollback/uninstallは元が不存在なら自分の一致するflagだけを除去、元が存在したなら保存した内容/mode/ownerを復元する。private backupは削除せず残す。flag内容を別の純正設定へ書き戻さない。
- 書込み/ledger更新/read-backの途中エラーは成功扱いにしない。電源断等で記録と現物が一致しなければ自動削除せず、private記録を確認する。通常のファイルシステム原子的renameだけで全電源断に対する復旧を保証したとは扱わない。

## 再現手順と中央統合

`tools/device/ota-policy.sh` はrootで固定NCC binaryを固有/runへcopyして実行する。persistent材料のnoexecを回避し、自分のcopyだけを後片付けする。純正8serviceのPID/stateを前後比較し、private runtimeへ保存する。

```sh
sh ota-policy.sh status
sh ota-policy.sh install
sh ota-policy.sh enable    # 新規取得を許可。非推奨
sh ota-policy.sh disable
sh ota-policy.sh rollback  # 導入前の存在/内容/ownerへ復元
sh ota-policy.sh uninstall # 同じ復元。private backupを保持
```

中央CLIは `--mode ota-policy --ota-action <action>` から `otapolicy.Run(action)` を呼ぶ。初回installはbackup/trap準備後、rollback/uninstallはownerディレクトリ除去前に上記actionを実行する。必要なroot workerだけに `/pio_data/SYSTEM/Update` とownerのprivate ledger directoryへの書込みを許可する。既存非root Web workerへ書込み権限を付けない。

HTTPはroot境界で `HandleOTAPolicy` を固定 `/device/ota-policy` に配線する。GETはObservation、PUTは `{expectedToken,disabled}` の厳密schema。古いtokenや他actorの変更は409、未知FW/未管理は変更不可。Webには `OTASettings.vue` をSystem設定へ配置し、未取得時の操作と二重送信を禁止する。

## 検証

host上で初回default OFF、upgradeでの再有効化維持、元fileの内容/metadata復元、stale token、外部flag変更、marker無しの削除拒否、未知FW拒否を単体テスト。HTTPは欠落/null/型違い/余計なfield/複数JSONを拒否するテスト。Go test/vet、Linux ARM64 crosscompile、Vue型検証、shell構文検証に合格。実機のflag生成・read-back・原状復元、純正機能、再起動後維持は実機担当が別途確認する。
