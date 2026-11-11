# 純正NP1設定の互換性と実装状況

## 現在の区分

ネットワーク・ドラレコ・SD・本体・LED/音声・USB・ログ・表示設定は既存の各画面で扱う。NP1 SERVICEの「純正NP1の設定」は、それ以外のプライバシー、運転案内、経路と交通情報、お知らせを扱う。

`backend/internal/adapters/native.go` のGETは純正Setting共有領域をO_RDONLYで読み、18項目を返す。後続車検知・感度は純正APIの変更・通知・永続保存・元値復元まで実機確認済みで、専用workerで操作する。感度はWeb API往復も確認済み、検知ON/OFFはWeb往復を追加確認中。その他はread-onlyを維持する。PUTは固定allowlist、整数型・範囲、expectedValueと保存fileを検証し、確認できない場合に成功を返さない。

| 項目 | 区分 | 経路・read-back | 保存/復旧 |
| --- | --- | --- | --- |
| プライバシーモード | READONLY | Setting ID8。固定共有領域、2回一致read | 設定変更なし |
| 運転支援・案内頻度・純正サービスでの車内カメラ利用 | READONLY | ID39/44/49。純正アプリ要求handlerからID対応を確認 | enumの選択肢名が未確定なものは数値として明示 |
| 後続車の接近検知・感度 | READONLY | ID50/51。TailgatedSetting handlerから確認 | 書込み/検知実動作は未検証 |
| 交通確認・初心者モード・渋滞・規制 | READONLY | ID61〜64。PersonalSettingの更新箇所から確認 | アカウント/サービス稼働と保存値を混同しない |
| フェリー・有料道路・スマートIC・経路検索 | READONLY | ID65〜68。同上 | 選択肢名は確認中。推測でON/OFFに変換しない |
| いつものコエ・安心サポートのコエ・前車発進・信号変化のお知らせ | READONLY | ID77〜80。同上 | 保存値だけで通知機能の稼働を断定しない |
| 取付方向/高さ/位置 | EXPERIMENTAL | 純正InstallationSetting、ID31/461/462。別領域の処理あり | この版の読取りallowlist外。推測offsetで読まない |
| マイクON/OFF・集音方向/範囲・車種 | EXPERIMENTAL | ID2/3/4/5/6/7。純正Hardware/MobilePhone handler | 音声画面の担当範囲。値の意味/変更の再取得を未検証 |
| 音量・mute・補正・VP音量 | EXPERIMENTAL | ID0/1/22/48、公式GATT0400資料 | 音声画面の担当範囲。純正設定のapply未検証 |
| STA状態 | READONLY | 純正wpa_supplicant STATUS、safe keyのみ | 設定変更なし |
| 純正APの状態/設定 | EXPERIMENTAL | NWlanSystem/CCOM、公式GATT0501資料 | 純正IPC ABI未確定。独立AP試験は別計画 |
| APN/PDP | EXPERIMENTAL | QCMAP/NPMCom内部symbol | 純正owner・SIM・保存形式未確定 |
| SD状態/録画再生 | READONLY | mount/容量・確定MP4 metadata・原本handle | 削除/移動/formatなし |
| 録画品質・録音・衝撃検知・駐車監視 | EXPERIMENTAL | Setting ID25/26/27/29/30/34/35/83と純正アプリhandlerを確認 | ドラレコ画面の担当範囲。apply無効 |
| SD format | DANGEROUS | 公式GATT0601/0001 | Web操作に含めない |
| camera export | READONLY | ImageRecog出力。QMMF新規sessionなし | 純正録画を占有しない |
| LED | OWNER CONTROL | 所有者用の独立worker | 純正設定とは別。停止は消灯、純正の元表示復元とは異なる |
| USB role | READONLY | sysfs peripheral | 独立管理/自動復帰が成立するまでwrite無効 |
| firmware更新・初期化・保守入退場 | DANGEROUS | GATT情報参照のみ | update/format/reset/保守切替を設定allowlistへ追加しない |
| account/check-in | ACCOUNT_DEPENDENT | 純正クラウドとローカル機能は別 | credentialをWebへ出さず、偽装/契約回避なし |

## 読取り実装の根拠

所有端末からSetting、libAplLib.so、libAIPI.so、libNStandardLib.so等を読み取りコピーし、ローカルELFを実行せず解析した。材料はGit除外のresources、解析出力と生共有領域はtmp/native-settingsに保存。機器ID・SSID・token・純正バイナリは公開ソースへ含めない。

この版のSetting/libAplLib.soのSHA256をallowlistとして照合する。異なる版を同じoffsetで解釈せずunknownとする。

- Setting: `d5b876fd8e5a3c18037b25812f5d989351045ecd024b40a99c551c4d00f7e0ac`
- libAplLib.so: `83bcfe30838001b382e58771fd28eabf17e43e6e72568ee5ad80656d223eae00`

`NProcSharedMemory::Open`の逆アセンブルから、ファイル先頭8byteはpayload size、固定header128byteの後に実データが来ることを確認。実機の既存mapは総7296byte、payload7168byte。payloadのinitialized=1/version=1、valuesOffset=44/count=86を実測し、純正GetStatusの索引処理と照合した。

GETは固定パスをO_RDONLYで最大8193byteだけ2回読み、headerの可変refcount領域を除いたpayload全体の一致を要求する。初期化済み・対応size/version/count/offsetの全条件を満たす場合だけ数値を公開する。共有領域をmmap(PROT_WRITE)したりvendor lockを変更したりしない。書換え中・未対応版・未初期化・未知enumはunknownとして扱う。アカウントやcredentialを含み得る全領域をAPIへ転送せず、固定の項目だけを抽出する。

## getter/setterを直接呼ばない理由と制御helperの到達点

`AL_SettingService::GetStatus(AL_SRV_SET_ID)` は単なるreadではない。最初にEnableSettingを呼び、未初期化の条件では純正初期値ロードに進む。静的に確認した主な箇所は以下。

- GetStatus: libAplLib.so 0xaa8a0。IDは第2引数、内部0xdbc10がstatus領域から索引read。
- EnableSetting: 0xaa770。内部0xdc3d0でinitialized==1なら0xdc4c0へ分岐し、初期値ロードを省略することは確認済み。
- NProcSharedMemory::Open: libNStandardLib.so 0x33380。O_RDWR・共有mmapを使い、vendor headerのrefcountを増加する。getterでもこの副作用はある。
- AL_SettingServiceのconstructor/destructorは対象版では状態なしの小さな関数。これだけで全依存libraryのdlopen constructorの安全性を証明したことにはならない。
- SetStatus: libAplLib.so 0xaa9a8。EnableSetting、現在値の取得、内部0xdc080でvendor lockを使った値の更新、更新dirty flag、`AIPI_CSSET::RequestCSSETSettingStatusChanged(ID,value)` の通知を行う。同じ値なら更新/通知を省略する。
- 通知: libAIPI.so 0x2e43e0。固定eventを構築しAIPI_CSSET singletonのdispatchへ渡す。単なるファイルwriteでは同じ処理にならない。

したがってread-only helperはGoの固定パスreaderとして実装済みだが、write helperを有効化するには、少なくとも次の条件を満たす必要がある。

1. fingerprintと初期化済みheaderを適用直前にも確認し、初期化関数を明示的に禁止する。
2. 純正libraryの通常client初期化・constructorとAIPI接続が、純正プロセスを新規起動/停止・設定初期化しないことを確認する。
3. 正確なID/enum/rangeと引数ABIを固定allowlistで持ち、vendor自身のSetStatus・lock・通知経路を使う。共有mapの推測直接writeは実装しない。
4. expectedValueを現在値と照合し、設定→純正handlerの反映→再取得を確認する。呼出しがreturn trueでも機能成功とは扱わない。
5. 失敗後に現在値を再取得し、単純な盲目的再送をしない。純正主要サービスと対象機能、必要なら再起動後の永続性を別計画で確認する。

この調査では未確定ABIの関数呼出し、Settingの停止/再起動、純正設定・共有領域へのwrite、GATT state-changing commandを実行していない。AP試験と同時にデバイスmutationを行わない。

## API/画面と検証

- GET `/device/native-settings`: Observationとitems。id/label/group/kind/value/valueLabel/status/writable/reason。未取得はnull。現在はkind=info、writable=false。
- PUT `/device/native-settings/{id}`: expectedValue/valueを必須とし、未知キー・非整数・文字列/boolean/null・指数/小数表現・範囲外・複数JSONを拒否。現在値不明503、expectedValue不一致409、未検証の変更403。任意path/enumID/library名を受け付けない。
- UIはgroup別に表示し、read-onlyは値＋理由を表示。将来有効化された項目だけ入力を表示し、変更前値を送り、返された値と要求値が一致するまで成功表示しない。取得失敗/変更結果不明時は編集を無効にして再取得を促す。
- Goテストは対応header/破損header、fingerprint不一致、欠測をOFFにしないこと、固定allowlist/型/範囲/競合/変更拒否を検証。`go test ./internal/adapters -run Native`、`go vet ./internal/adapters`、`vue-tsc --noEmit` 合格。
- 実機mapの形式とGo API取得を確認済み。native setterは後続車検知・感度の往復試験が合格、感度はWeb APIと保存fileの照合も合格。接近検知発報そのものは実走行の別検証とする。

## wire enumと保存値の違い

Settingの埋込FileDescriptorProtoをホスト上で復元し、純正アプリのprotobufのenum定義を確認した。descriptorや純正ソース相当の生出力はtmp/native-settingsのみへ保存している。

後続車検知のCommonOnOffはwire 1=OFF/2=ONだが、純正OnTailgatedSettingRequest/OnTailgatedSettingDetectionNotifyは保存値0=ON/1=OFFへ変換する。単純に0=OFF/1=ONと解釈しない。感度もwire1/2/3と保存値0/1/2の対応が異なり、保存値0=低、1=標準、2=高を純正の両方向変換から確認。getterとsetter両方のbranchを照合した表示だけを有効にした。

プライバシーは保存値0/1の存在とtoggle経路が確認できているが、ON/OFFの意味はこの版では推定しない。保存値として表示する。純正アプリの通信enumをそのまま共有領域へwriteする実装も行わない。

専用vendor helperの初期化guard・wait上限・共有refcount回収・再現可能なroot試験入口は [native-vendor-helper.md](native-vendor-helper.md) を参照。Web書込みは実機trial/read-backが済むまで閉じたままとする。
