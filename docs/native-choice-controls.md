# 検索方法・コエ設定の独立helper

2026-11-11。既存の後続車50/51、追加binary設定9件、Alexaのhelper source/ELF/hashを変更せず、3項目専用の `native-choice` targetを作成した。ホストbuild/tests、vendor-free preflight、3項目の実機往復、配備後のWeb API往復まで合格した。実際の声による発話・音声cache取得済み状態は未確認。

成果物: `artifacts/native-choice-helper/ncc-native-setting-choice-helper`。
SHA-256: `1147f6befabb84640fcc07572822ef8b39e8a71c7883ea3704281c588dbbd342`。

## 固定契約と根拠

| ID | ordinal | persistent key | 許可するnative値 | 意味 |
| --- | --- | --- | --- | --- |
| route-search | 68 | 526 | 0〜5 | おすすめ/距離/道幅/到着時刻/料金/あなたに合わせる |
| daily-voice | 77 | 536 | 0〜5のみ | いつものコエ。確認済み内部voice名6件 |
| support-voice | 78 | 537 | 0〜5のみ | 安心サポートのコエ。同じ内部voice名6件 |

enumの入力・消費・personalize JSON・内部voice名は [保存値の根拠](native-setting-values.md) を参照。persistent keyはpinned `libAplLib.so` のBackup配列 `0x189a80`（86件）のordinal位置から得た。buildでもprivate ELFのSHAと3件の対応を照合する。Setting.DATはpersistent key順なのでordinalをdisk offsetにしない。

voiceの6〜12はcloud入力変換表に存在しても、確認済みconsumer selectorの対応がないため拒否する。6つの既知voice名があることは、発話cache取得済みやpaid/account機能の利用可能性を意味しない。このhelperは声のデータをダウンロードせず、再生・発話も要求しない。

## 既存runtimeとの分離

`native-choice-core.h` が3つの固定IDと0〜5の厳密な1文字数値を検査し、`native-setting-choice-helper.c` が変更済みmemory/disk値とのCASとconsumer fingerprintを追加する。既存 `native-setting-helper.c` はincludeして再利用する。

元のmainには上限2の引数parserが2箇所あるため、このtargetのinclude中だけ `small_number` を `ncc_choice_apply_number` に置き換える。元の上限2呼出しだけを上限5として読み、予期しない別上限は拒否する。既存source/default binaryのparserは変わらない。この橋渡しも実CをWasmへcompileして3/4/5を検査する。

- 既存Setting・Apl・AIPI・NStandard等のfingerprint、初期化済みheader、4sem preflight、vendor mutex/mapping/refcount guardを引き継ぐ。
- 追加consumerはNavigation / Voice / libVoicePlayServiceの固定SHAを検査する。preflightは3者、applyは対象のconsumerのみ。
- vendor-free preflightはDSOをロードしない。applyも既存vendor GetStatus→SetStatus→CSSET通知→GetStatusの経路であり、shared map / Setting.DATへの直接writeは行わない。
- 既存workerを止めず、共通 `/run/np1-control-native/control.lock` を非blocking取得する。
- 実行時にhelperだけ `LD_LIBRARY_PATH=/usr/local/lib` と `NCC_NATIVE_TRIAL=1` を渡す。

## 実機試験前の準備・復旧

他のnative/network実機試験とwrite windowを調整する。走行中や案内を利用中には行わない。検索方法は次の経路検索条件に、コエは次の発話内容に影響する可能性がある。rootの開始合図があるまで実機へstage/起動しない。

```sh
python tools/build-native-choice-helper.py
python tools/stage-native-choice.py --serial <ADB_SERIAL> --action preflight
# preflightでfresh元値を確認し、異なる既知値を1つ選ぶ。
python tools/stage-native-choice.py --serial <ADB_SERIAL> --action roundtrip --id route-search --next 1
python tools/stage-native-choice.py --serial <ADB_SERIAL> --action roundtrip --id daily-voice --next 1
python tools/stage-native-choice.py --serial <ADB_SERIAL> --action roundtrip --id support-voice --next 0
```

上記nextは手順例であり、現在値や合格結果ではない。最初のvoice試験は実機の元値が既知0/1ならその2値間を候補にし、追加voiceを試す前に受理・保存・原状復旧とconsumer継続を確認する。元値が未知6〜12なら変更せず止める。route全6択/voice既知6択の許可範囲と、実機で試した値の範囲は別に記録する。

stageは専用markerを持つowner experimental素材だけを書き、trial固有/runへcopy/chmodして実行する。既存07bcc / cb3a55 / Alexa素材を上書きしない。固定ID/0〜5以外のselectorを拒否する。

trialは変更前の共有値・4sem・Setting.DAT全704byte・10純正serviceのPID/stateをprivate保存する。一度に1項目だけ適用し、4秒後の保存を確認して元値へ戻す。EXIT処理もfresh現在値が自分のNEXTと一致した場合だけexpectedValue付きで復元する。元値は固定のdefaultから作らず、開始時のprivate backupを使う。

partial applyで共有値だけ変化しdiskは元値という場合に限り、復元呼出しへ `NCC_CHOICE_RESTORE=1` を渡す。memoryは期待current、diskは期待currentまたは復元先の既知元値のどちらかだけを許可する。通常Web applyへこの復元例外を渡さない。未知値・別actorの値がある場合は盲目的に戻さず、以後の試験を中止する。

host側は実行helper SHA、対象native/diskの往復、適用中に対象1項目だけが変わったこと、復元後704byte一致、10 PID/state・4sem一致、apply/restore双方のmutex/mappingゼロとrefs一致を独立検査する。これらが揃うまではWeb writeを開放しない。保存試験だけで実走行の経路や声の発話機能を合格にしない。

## ホスト検証

```sh
python tools/build-native-choice-helper.py --test-only
python -m unittest discover -s tests -p test_native_choice_trial.py -v
```

実Cによる0〜5の全36組×3項目、旧parser橋渡し、未知ID・6〜12・文字列・数値混入・shell混入拒否、保存parserの境界/重複/範囲外、partial restoration条件が合格。AArch64 ELF/vendor-free startup/exported guard、固定persistent配列、shell構文も合格。hostの証跡検査は合成fixture4テストで、最大値5の往復、無関係設定の変更、stock restart、cleanup欠落を検証した。合成データを実機の成功結果として扱わない。

## 実機往復とWeb統合

vendor-free preflightの後、68は5→0→5、77/78はそれぞれ4→0→4を約90秒で実施した。全3件でnative/disk双方を確認し、適用中の変更は対象1項目だけ、復元後704byte全体・10純正service PID/state・4sem・mutex/mapping/refsが基準と一致した。証跡はignored `tmp/native-choice-evidence/summary.json`。6択すべてを実機で試したという意味ではない。

controllerは専用runtime path `/run/np1-control-native/choice-helper` と上記SHAを固定し、persistent526/536/537・最大5を使う。既存の共通lock・native/disk CAS・7秒保存pollを継承し、legacy helperの11項目はそのまま保持する。unitがoptional素材を専用/runへcopy/chmodする。素材が無い・SHA不一致・現在値が6〜12等の場合にはwritableにしない。

77/78の選択肢は確認済み内部名6件だけを公開する。声種選択とcache取得済みを混同しない説明を維持する。

配備後の `tools/verify-native-control.py` は通常HTTPS APIを通して68の5→0→5、77/78の各4→0→4を確認した。全3件のlive responseでwritable/select、正確な0〜5の6択、現在voice名、6〜12の除外を確認した。Web操作前後を跨いだ全704byte・共有値・10純正PID/state・4semも一致。private証拠は `tmp/native-choice-evidence/web-proof.json` / `web-baseline-proof.json` と各 `tmp/native-api-<id>-roundtrip.json`。その後のread-only実画面確認はrootの予定reboot開始により接続不可となったため、Web APIの成功とブラウザDOMの実画面確認を混同しない。
