# 純正音声設定と常駐workerの限定更新

純正Bluetooth設定に相当する音声設定は、Setting所有プロセスのCommonFreeStyle `function=0x0400` を利用する。独自に設定ファイルや共有mapを書き換えない。変更対象は消音と音量補正だけとし、マスター音量は読取り専用にする。

## 型と意味の根拠

固定対象のSettingは `d5b876fd8e5a3c18037b25812f5d989351045ecd024b40a99c551c4d00f7e0ac`。関数登録は `8e2dc..8e2e4`、返信は元request contextを渡す `8e1c4`。CommonFreeStyle command 1は取得、3は消音、4は補正。返信はそれぞれ `8001/8003/8004`。PID・nonce・宛先名・function・command・長さを照合する。

| 項目 | native ID | 永続キー | native値 | wire値 |
|---|---:|---:|---|---|
| マスター音量 | 0 | 465 | 取得した整数を表示 | 変更未対応 |
| 消音 | 1 | 466 | **0=消音する、1=音を出す** | 2=ON、1=OFF |
| 音量補正 | 22 | 468 | 0=OFF、1=弱、2=強 | 確認済み固定enum |

消音の0/1は一般的な真偽と逆だが、推測ではない。SettingのGET `8e7bc..8e7f4/8e8b4` はnative0をwire2(ON)へ変換し、変更側 `8ec28→8ec40..8ec48` はwire2をnative0として `SetStatus(1)` へ渡す。wire1は `8ed04..8ed0c` でnative1へ変換する。Webの `mute.enabled=true` は「消音する」を表す。

永続キーはpinned AplLib `189a80` の86要素tableから導出する。native IDに一定数を加算した推測対応は使わない。

## 実装と検証

別target `tools/native/native-audio-helper.c` は既存Alexa runtimeの受信queue所有・相関・cleanup guardを再利用する。Setting PIDのexe/starttimeを各送受信の前後で固定し、取得結果と純正共有設定の0/1/22が一致してから変更する。変更はfresh取得値によるCAS、応答後GET、共有値、永続キーのread-backまで確認する。未知値、timeout、競合は成功扱いしない。

```powershell
python tools/build-native-audio-helper.py
python tools/stage-native-audio.py --serial SERIAL --action preflight
python tools/stage-native-audio.py --serial SERIAL --action get
python tools/stage-native-audio.py --serial SERIAL --action roundtrip --id mute --next 0
python tools/stage-native-audio.py --serial SERIAL --action roundtrip --id volume-correction --next 2
```

生成helper SHA256は `3775648059142273549bebb1aa15806b25b9ee2cfb888747862e0f5df1f51156`。2026-11-12の限定fieldでは消音1→0→1、補正1→2→1を確認した。全704byteのSetting.DAT、10純正serviceのPID/Invocation/状態、本体起動ID、4semが原状に戻り、helper所有queue/mapping/mutexも解放された。音声の実際の出音・走行中の補正体感はこの保存試験の合格範囲に含めない。Web経由の往復確認は別に行う。

Web APIはGET `/device/native-audio`、PUT `/device/native-audio/mute` と `/device/native-audio/volume-correction`。PUTは整数 `expectedValue/value` の固定schemaのみ。Settingが利用者により停止中なら変更不能の理由を表示し、自動起動しない。画面はLED/音声内の `NativeAudioSettings.vue`。320/390px、明暗両方、消音反転、CAS競合、連打、readonly、503、応答不一致をfixtureで8件検証した。

## 自作native workerだけの更新

`tools/device/native-worker-update.sh` は通常installと異なり、AP・modem・Web・broker・純正unitを停止しない。stageのSHA/selftestを確認し、root所有0700の復旧先へ元binary・unit・optional helperと元の存在状態を保存する。atomic copy後にdaemon-reloadと `np1-control-native.service` のみを再起動する。

更新中はRuntimeDirectory外の固定update lockを排他取得する。通常native制御側は同じlockを共有取得してから従来control lockを取るため、RuntimeDirectory再作成で旧lockと新lockが分裂する窓でも操作を拒否できる。子vendor処理をtimeoutでkillして共有mutexを残す方式は使わない。

失敗時は元unit/binary/helperを復元し、自作nativeだけを再起動する。元が存在しなかったfileは所有ledgerと今回stage内容が一致する場合だけ削除する。前後の純正・他自作workerのPID/Invocation/状態、本体起動IDを照合し、利用者が停止したunitもそのまま尊重する。host mockでは通常更新・完全rollback・busy時無変更・他unit driftの4件を確認済み。Linux固有flockの実動作と実機更新はfieldで別に確認する。

Web往復ツール `tools/verify-native-control.py` と `tools/verify-native-audio-control.py` は `--serial` を必須とし、固定33unitと起動IDを前後照合する。安定inactive/PID0も正常なbaselineとして保存する。変更ownerのSettingだけはactiveを要求し、起動して試験を通すことはしない。映像認識側停止時の通知設定保存試験を、通知機能そのもののlive合格として扱わない。

2026-11-12、通常workerでは別UIDのSettingに対する `/proc/<PID>/exe` の読取りがsandboxのcapability制限で失敗していた。専用native workerへCAP_SYS_PTRACEを追加し、所有プロセスの同一性検査を省略せずに通す。純正・Web/AP/modemを操作せずnative workerだけ更新後、消音と音量補正のWeb変更・永続read-back・元値復帰が両方合格した。発進通知・信号通知のWeb往復も合格した。各試験で固定33unitと起動IDを前後照合した。
