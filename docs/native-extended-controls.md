# 純正binary設定9項目の独立検証

既存50/51およびAlexaの検証済みhelper/control serviceを変更せず、追加候補だけを別targetに分けた。2026-11-11、vendor-free preflightと下記9項目の実機変更・保存・復元を個別に検証し、すべて合格した。その後Webのwritable判定へ固定mapping/hashを追加し、`tools/verify-native-control.py --setting ID`で9項目すべてのWeb変更・永続保存read-back・元値復元も合格した。

検証した独立helperのSHA-256は `cb3a55d5bd1a004821fe1252646a54d7eaf840a49c11adb059b334edbb67325b`。既存50/51の07bcc helperとAlexaの83519 helperは変更していない。

## 入力・消費・保存の証拠

保存値0が有効側、1が無効側。JSON boolをそのまま共有値として扱わず、Settingの共通変換表0x197020（true→0/false→1）とconsumerの逆方向を照合した。詳細の解析資料は [保存値の根拠](native-setting-values.md) を参照。

| Web ID / native ordinal | 0 / 1の意味 | 入力側 | consumer | persistent key |
| --- | --- | --- | --- | --- |
| privacy-mode / 8 | ON / OFF | Setting hard-key toggle 0x93880、SetStatus8=0/1 | ImageRecog CheckPrivacyModeDisable、保存値非0ならupload可 | 529 |
| driving-support / 39 | 有効 / 無効 | JSON driving_support→SetStatus 0x97db8 | Navigation 0xdc524で保存1なら支援skip | 495 |
| service-cabin-camera / 49 | 許可 / 許可しない | allowed_using_in_camera→SetStatus 0x981ec | VirtualPassenger 0xa3484/0xf1390、0:OK/1:NG | 505 |
| traffic-check / 61 | 自動確認する / しない | auto_traffic_check→SetStatus 0x97e9c | Navigation 0xdf2d4、非0ならdisable | 519 |
| traffic-congestion / 63 | 考慮する / しない | traffic_congestion→SetStatus 0x97f6c | Navigation getter 0x19caa8、0をtrueへ | 521 |
| traffic-regulation / 64 | 考慮する / しない | time_regulation_congestion→SetStatus 0x97fd4 | Navigation getter 0x19cb08、0をtrueへ | 522 |
| ferry / 65 | 利用する / しない | use_ferry→SetStatus 0x9803c | Navigation getter 0x19cb68、0をtrueへ | 523 |
| toll-road / 66 | 利用する / しない | use_toll_road→SetStatus 0x980a4 | Navigation getter 0x19cbc8、0をtrueへ | 524 |
| smart-ic / 67 | 利用する / しない | use_smart_ic→SetStatus 0x9810c | Navigation getter 0x19cc28、0をtrueへ | 525 |

persistent keyはlibAplLibのBackup Composeが使う静的配列0x189a80（86件）のordinal位置から確認。ビルド時にも所有実機から取得したprivate ELFの表とheaderの全対応を照合する。保存ファイルはordinal順ではなくpersistent key昇順なので、固定offsetへ直接書き込まない。

privacyは追加条件がある。純正hard-key toggleはGetStatus23が0なら「initial setup not finished」としてSetStatus8を呼ばない。このhelperもprivacy操作前に既知のsetup完了値23=1を要求する。初期化・format・setup解除を提供しない。これはstock key同様の事前条件検査であり、別actorのformatと同時実行する設計ではない。

## 別targetと制約

- `tools/native/native-extended-core.h`: 新しい固定allowlistとpersistent key、privacy guard、保存read-backの純粋C関数。
- `tools/native/native-setting-extended-helper.c`: macroでmain名だけを変えて、検証済みsetting helper sourceをincludeする。既存source/default ELFを変更しない。
- `python tools/build-native-extended-helper.py`: `artifacts/native-extended-helper/ncc-native-setting-extended-helper` を生成。`--test-only` はvendor資料なしでWasm pure C testsを実行可能。
- `tools/device/native-extended-trial.sh`: 一度に1項目のcontrolled roundtrip。
- `tools/device/native-extended-stage.sh` / `tools/stage-native-extended.py`: SHA検証付きの独立stageと、private記録の取得・host側の独立照合。

ID/enumは上表9件・native0/1だけ。50/51、85、声、頻度、route-search、取付、format/resetはこのtargetのallowlist外。元の50/51は07bcc helper、Alexa85は純正Voiceのlinkguard経路を使い続ける。

追加でconsumer自身のSHAを照合する。Navigation=692f35e7097ec07947a3a8e264a5275696ada5b2224ab36f5a440fe8befbcc21、VirtualPassenger=9d722e6bad8bbe512eb2f7fa73c9cfef5030cb97273476514b52fe641f5383cf、ImageRecog=02a37de0a0a246bd90612e8f4fea77d20aa9556b18259ffc1f9babd9805b4f47。preflightは3者、applyは対象consumerだけを検証する。既存Setting/11DSOのpin、4sem、初期化header、全mutex/mapping ownership、既存map refcount照合も引き継ぐ。

通常applyは共有値と保存値の双方がexpected値であることを要求し、既存vendor GetStatus→SetStatus→CSSET通知→GetStatus/独立snapshotで照合する。既存guardが未初期化/default load、global IPC作成、cleanup、未知file truncateを止める。純正mapやSetting.DATを直接書かない。

## 実機手順・復旧

stable control workerを停止しない。rootと試験時間を調整し、共通 `/run/np1-control-native/control.lock` をnonblocking取得する。単なる別の試験専用lockへ分けない。新helperはnoexecのowner experimental領域へ材料として置き、trialが固有/runへcopy・chmodして実行する。

```sh
python tools/stage-native-extended.py --serial <ADB_SERIAL> --action preflight
python tools/stage-native-extended.py --serial <ADB_SERIAL> --action roundtrip --id driving-support --next 1
# 以下はstage経由で呼ばれる実機側scriptのselector。
sh native-extended-trial.sh preflight
# 初期値0を確認した場合、まず制限側1へ変更し、同じscriptが0へ戻す。
sh native-extended-trial.sh roundtrip driving-support 1
# privacyは初期値1なら0(ON)へ変更し、元の1へ戻す。
sh native-extended-trial.sh roundtrip privacy-mode 0
```

前後の8core service PID/stateに加え、Navigation/VirtualPassengerのPID/stateを記録して同一性を比較する。10者すべてのactive/非ゼロPIDを事前要求する。consumerの稼働は案内・経路再計算の機能試験に代わるものではない。

helper preflightで9共有値と9保存値、setup23、既存50/51/85、4global semを取得し、変更前・4秒後の適用値・復元後を照合する。Setting.DATのbefore/applied/after全copyもprivate0600へ保存する。3秒debounceより後のdisk反映を確認し、helperの値一致だけで成功にしない。

失敗時もfresh共有値を読み、自分が要求したNEXTと一致する場合だけexpectedValue付きvendor APIで元へ戻す。途中失敗でmapだけNEXT、diskは元の値という場合を復元不能にしないため、launcherの復元だけ `NCC_EXTENDED_RESTORE=1` を渡す。この場合もmemory==期待currentを要求し、diskは期待currentまたは復元先の既知元値のどちらかだけを許可する。未知・範囲外値や別actorの値なら盲目的に復元しない。通常のWeb applyへこの復元例外を渡さない。

全値/sem/PIDが基準へ戻らない試験は失敗・判定不能とする。実車のrouting、通知音、cloud利用の抑止等は別の機能検証であり、保存値roundtripだけで全てを合格にしない。

host側はshell終了成功だけでは合格にせず、実行helperのSHA、共有値・保存値の往復、適用中に保存86項目の対象1件だけが変わったこと、復元後Setting.DAT全704byteの一致、10者のPID/state、4sem、apply/restore双方の所有mutex・mappingゼロとrefcount一致を独立検査する。生記録はignored `tmp/native-extended-evidence/` のみに保存する。

## 実機試験結果

| 項目 | native / diskの往復 | 対象以外の保存値 | 復元・10 service・4sem・cleanup |
| --- | --- | --- | --- |
| driving-support39 | 0 → 1 → 0 | 変更なし | 合格 |
| service-cabin-camera49 | 0 → 1 → 0 | 変更なし | 合格 |
| traffic-check61 | 0 → 1 → 0 | 変更なし | 合格 |
| traffic-congestion63 | 0 → 1 → 0 | 変更なし | 合格 |
| traffic-regulation64 | 0 → 1 → 0 | 変更なし | 合格 |
| ferry65 | 0 → 1 → 0 | 変更なし | 合格 |
| toll-road66 | 0 → 1 → 0 | 変更なし | 合格 |
| smart-ic67 | 0 → 1 → 0 | 変更なし | 合格 |
| privacy-mode8 | 1 → 0 → 1 | 変更なし | 合格 |

Communication / DriveRec / ImageRecog / Voice / qmmf-server / NEventWatcherCom / Setting / Update / Navigation / VirtualPassengerの10者が各試験の前後でactiveかつ同じPIDだった。既存50/51/85とsetup23も前後一致。ここで合格したのは純正設定の受理・debounce後の保存・復元であり、走行時の案内や外部cloudの実挙動は未試験。

補助の `tools/hardware_verify.py --camera` はWeb認証、summary、SDのMP4部分取得206、path traversal拒否を通過した後、前方camera streamが503となり未合格だった。source別の再確認では車内JPEGはHTTP200で取得できた。前方の純正exportは約68分前から更新停止、車内exportは1秒前更新で、既存の15秒stale検査が前方を拒否した。停止時刻は追加9項目の試験開始より前だった。guardや純正サービスは変更せず、設定保存試験の合格とは分けて扱う。

## host検証

実際のC allowlist/guard/parserをWasmへcompileし、9件の全0/1組合せ、未知ID、文字列/浮動小数/範囲外/command混入の拒否、privacy setup0/2拒否、persistent key、704byte境界/重複/順序、partial applyの条件付き復元baselineを検証し合格。AArch64 ELF/vendor-free startup/exported guard、private ELFのpersistent table照合、shell構文も合格。07bcc setting helperと83519 Alexa helperのSHAが変わっていないことを確認した。
