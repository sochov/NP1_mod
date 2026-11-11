# 前車発進・青信号の通知設定

## 確定した値と処理

純正SettingのPersonalSetting入力と、ImageRecogが通知を生成する際の消費処理を照合した。保存値の意味は2項目とも同じで、0=通知しない、1=効果音と音声、2=効果音のみ。boolへ変換しない。

| 固定Web ID | native ID | Setting.DATの固定key | 対象 |
|---|---|---|---|
| `front-car-start` | 79 | 538 | 前の車の発進通知 |
| `signal-change` | 80 | 539 | 青信号の通知 |

入力は `np1s[対象].drive_support.move_front_car` / `signal_change`。Settingの変換表 `0x197160` / `0x197180` はそれぞれ `(0,0),(1,1),(2,2)` のidentity。`SetStatus` の呼出し `0x98350` / `0x983c8` でnative79/80へ渡す。保存key538/539はpinned libAplLibの `0x189a80` にある86項目のordinal→persistent key表で導出し、単純なordinalのoffsetとして扱わない。

ImageRecogは `0xa817c` / `0xa81ac` のGetStatusで79/80を消費する。保存0はchimeを無効化、1/2は有効化し、1だけmessage flagを有効化する（`0xa83ec〜0xa8430`）。実際の通知経路 `0xa77c4〜0xa77d0` / `0xa7aec〜0xa7af8` でAIPIへ渡し、libAIPI `0x36b588` / `0x36b630` でchime/messageの引数になることを確認した。

この根拠は保存値の意味と受取側の処理を確認したもので、走行中の前車発進・青信号検出そのものを試験したものではない。変更操作で検出イベントを偽装しない。

## 独立したhelper

既存の2項目、追加9項目、6択3項目、Alexaのhelper・SHAを変更しない。新targetだけが79/80の0〜2を受け付ける。

- source: `tools/native/native-setting-notification-helper.c`、`native-notification-core.h`
- build: `python tools/build-native-notification-helper.py`
- output: `artifacts/native-notification-helper/ncc-native-setting-notification-helper`
- SHA256: `729728f8c245eddbed9e7acd54f4e33e99142c724fe26f4b896df3d87083d655`

既存のvendor初期化guard・500ms mutex/sem待ち・共有mappingの所有追跡とcleanupを再利用し、ImageRecogのfingerprintも照合する。SetStatus・CSSET通知経路を使い、共有mapやSetting.DATを直接書き換えない。

expectedValueの共有値と保存fileが一致することを適用前に要求する。復元用の特別な条件も、fresh共有値が自分の要求値であり、保存値が元値または自分の要求値である場合だけ許容する。第三の値や取得不能を盲目的に上書きしない。

## controlled trial

実機writerはroot担当のみとし、ネットワーク等の変更windowと重ねない。以下は再現用入口で、hostテストを実行したことだけでは実機適用済みにならない。

```powershell
python tools/stage-native-notification.py --serial SERIAL --action preflight
python tools/stage-native-notification.py --serial SERIAL --action roundtrip --id front-car-start --next 2
python tools/stage-native-notification.py --serial SERIAL --action roundtrip --id signal-change --next 2
```

`--next` はその場で取得した元値と異なる0/1/2を選ぶ。preflightだけならvendorをloadしない。stageは専用owned markerを要求し、既存helperを置換しない。共有native control lockを取得し、persistent領域の材料を固有 `/run` へcopyして実行する。

適用後4秒の保存debounceを待ち、元値へ条件付き復元する。前後で純正10serviceのPID/state、4global semaphore、preflight値、Setting.DAT全704byteを比較する。適用中の保存fileも、対象の固定key以外に差分がないことをhostで独立に検証する。vendor childを外部timeoutでkillしない。実機dumpとstderrはignored tmp内だけに置く。

## host検証と公開条件

- 実際のC validatorをWasmへcompileし、2ID×全3値、文字列/負数/小数/桁増し/範囲外/旧IDの拒否、固定保存key、backup境界、条件付き復元を検証した。
- AArch64 build、vendor-free startup dependencies、必要なprocess-local guardのdynamic export検査に合格した。
- CLI `--help` とpreflight・roundtripの全合法値/不正引数をhostで検証した。argparseのkeywordは `choices` のまま維持する。
- Pythonの合成証拠テストで、対象のmemory/disk往復、未知enum、無関係の保存値変更、consumer再起動、cleanup欠落を検出することを確認した。合成fixtureは実機成功の根拠にしない。

root担当がvendor-free preflightの後、79/80の各1→2→1を実機で確認した。共有値とpersistent538/539が一致し、適用中は対象1項目だけの変更、復元後は全704byte・純正10service PID/state・4sem・mutex/mapping/refcountが基準へ戻った。これは全3値の実走行発報試験ではない。

この結果に基づきnative workerの固定allowlistへ2項目を追加するsourceを用意した。既存helperと分けた `/run/np1-control-native/notification-helper`、上記SHA・固定key・最大2を使う。unit/build/installはoptional copyとし、素材不在・hash不一致・現在値不明の場合はwritableにしない。HTTPの3択・strict schema・CAS・保存key・未知値拒否もhostテストで確認した。

配備後のWeb往復はroot担当のnetwork試験終了後に行う。現時点の完了範囲はcontrolled実機試験と本番統合sourceであり、Web実機成功と取り違えない。
