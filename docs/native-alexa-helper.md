# 純正Voice経由のAlexa設定helper

純正の連携ガードを維持するため、Setting ID85を直接変更しない。既存VoiceのCommonFreeStyle function0x0b00へget/changeを送り、対応する返信だけで状態を取得する。host検証に加え、root担当のcontrolled trialでgetと0→1→0変更・復元に合格した。この担当から実機ABI呼出し・設定変更は行っていない。Web worker内での最終API検証はroot担当が別途行う。

## 独立成果物と段階

- source: `tools/native/native-alexa-helper.c`、独立runtime `native-alexa-runtime.h`、純粋frame validator `alexa-frame.h`
- build: `python tools/build-native-alexa-helper.py`、pure C parserのみ `--test-only`
- output: `artifacts/native-alexa-helper/ncc-native-alexa-helper` とsource SHA付きbuild-info。
- launcher: `tools/device/native-alexa-trial.sh preflight|get|roundtrip <0|1>`。
- persistent材料: `/overlay/owner-apps/np1-control/experimental/ncc-native-alexa-helper`。launcherが固有/runへcopyして実行する。既存setting helper/control serviceを置換・停止しない。

検証済みsetting helperのsource/outputは維持し、07bcc版のguardを別runtimeへ分岐した。共通のSHA/header/ownership追跡は既存portable coreを使用。Alexa専用のown receiver作成例外を、実験段階で既存の設定変更へ混ぜない。

最初にpreflight-only、次にgetを実測する。getとcleanupが通るまでroundtripを実行しない。ON_FIXED=2は読取り専用で、applyへ渡さない。roundtripは実機担当が元0/1と異なる値を指定し、受取後に元値へ条件付き復元する。

## Voiceへの正規リクエストと返信

`AIPI_Common::RequestCommonFreeStyleCommand` (`0x2794c0`) の引数は、宛先NString、context uint32×2、function uint16、command uint16、const std::string&。宛先Voice、function0x0b00、command4空payloadをgetに使用。changeはcommand3、ON=0はproto3 default空payload、OFF=1は08 01。純正Voiceの設定handlerがリンク中の変更拒否を処理する。

contextはhelper固定marker0x4e434341と呼出しごとのnonce。Voice返信のCommonExtIF `0x27a5d8` はrequest+0x4c/+0x50のcontextをコピーし、header+0x30 sender PID/+0x34 sequenceへSendReplyする。NPMComの固定DBus proxy名を奪う経路ではない。

応答はAIPI_Eventの14byte header、bounded NString、2context、function/command、uint64 payload length、protobuf。validatorはevent ID0x80001f02、期待Voice PID、nonce、function0x0b00、command0x8004または0x8003、frame長256以下、NString65文字以下、payload32byte以下、終端/長さ/enum/重複fieldを検証する。対象応答が受信できた場合だけ、proto3空payloadをON=0として扱う。timeoutや未応答をONへ置換しない。

changeは事前getがexpected-wire0/1と一致する場合だけ送信し、change結果trueと期待値、続く新しいgetの一致を要求する。拒否やON_FIXEDを直接storage writeで回避しない。helper read-backだけではSetting.DATへの永続保存や実際のAlexa音声認識を合格にしない。

## helper専用返信queue

- NStandard `NProcessManager::GetCurrentProcessID` (`0x35820`) はgetpidへtailcallする。AIPI_Base ctor `0x276030` はこれを自己PIDとして保持する。
- ExtIF ctorはrecv8192/send8192で構築。`Initialize` (`0x2740c8`) は `AIPI_server_%X` (自己PID) を作り、NProcMsgReceiver::Createを呼ぶ。純正AppManagerへの登録や新しいstock service instance作成はしない。
- NProcMsgReceiver::Create (`0x31278`) は `NMSG_MEM_%S` を加え、1channel/recv8192に対しpayload8224をAllocする。128byte header込みのfileは8352byte。
- 対象は `/tmp/victoria/victoria_NMSG_MEM_AIPI_server_<自己PID大文字HEX>.map` の1fileだけ。開始前に不存在を要求し、stale/既存fileを再利用・上書きしない。
- `open`例外は固定path・flags0xc2・NStandard return offset0x3247c・Initialize中だけ。実O_EXCL/NOFOLLOW/CLOEXECで作り、そのFDのdev/inodeを追跡する。fchmod0660、fchown(uid=-1, vendorが解決したgroup)、ftruncate8352も固定callsiteと同じFD identityに限る。その他のtruncate/ownership変更は停止する。
- own Allocを追跡し、他の既存mapは従来どおりOpen前のrefcountを保存する。未知のglobal map/semaphoreを作らせない。各PLTのguard bindingをGet前にpointerで照合する。

main threadはCheckMsgでnonemptyを確認し、ReceiveMsg(timeout0)で1メッセージを取得する。consumerはhelper自身だけで、Voiceはwriterなので、consumer間の競合は作らない。空なら10ms sleep、最大200回。StartMsgWait/threadを呼ばず、予期しないcond_wait/thread生成はguardで停止。mutex/semのwaitは既存の500ms上限を維持する。

## 終了と失敗

純正Voiceの返信SendMsg(pid) (`0x275cc0`) は一時AIPI_ProcObserverをConstruct→Connect→send→Destroyする。helperは受取後、own queueのheader+0x70 refcountが自分の1へ戻ることを最大1秒確認してからreceiver destructorを呼ぶ。vendor FreeMemoryが0になったown mapをdestroy/unlinkする経路は、固定path/inode/callsite0x31f44だけ許可する。

正常・失敗で自分のmutex/mappingをvendor APIで解放し、既存共有mapのrefcount、自己所有数ゼロを検証する。own created inodeだけを後片付けし、既存stock mapや他PIDのqueueを削除しない。DSO finiは通さず、global SEM_UNDO自己分はkernelに戻させる。private stderrへ失敗段階と固定ABI診断を残す。失敗時に途中状態が完全一致しない場合は成功扱いにせず、private記録を見てから次の試験を判断する。

launcherは純正8serviceのPID/state、50/51/85の保存値、4global semを前後比較する。roundtrip失敗でもfresh getを試み、現在値が自分の要求値と一致したときだけ元0/1へ戻す。別値/未知/固定中は盲目的に復元しない。外部SIGKILL timeoutを付けない。非root Webから直接実行するAPIはまだ無い。

## host検証

実際のC frame validatorをWasmへcompileしてNodeで実行。correlation/PID/function/command/length、ON/OFF/FIXED、getのdefault ONとchangeのdefault拒否、途中切れ/長過ぎ/重複/未知fieldを検証しPASS。AArch64 compile、vendor-free startup、全既存guardとown fchmod/fchown/ftruncate/unlink/Allocのdynamic export、shell構文に合格。実機get/change成功やAlexa認証成功を示すものではない。

## 実機の確認済み範囲とWeb接続

root担当の83519版controlled試験でpreflightと実getが合格し、wire0を受信、own receiver削除・共有refcount cleanupを確認した。次のroundtripはwire0→1→0、共有Setting native85=0→1→0、Setting.DATのpersistent544=0→1→0を実測照合。純正8service PID/state、50/51/85保存値と4global semaphoreは試験後に基準へ戻った。private試験記録は `tmp/alexa-trial-evidence/` に保持し、raw記録をGitへ追加しない。

この成功に基づき `adapters/alexa_control.go` の固定83519 helperを使うcontrollerを実装した。public/broker側 `HandleAlexaRemote` とroot socket側 `HandleAlexaLocal` は別関数とし、固定 `/alexa-settings` へ転送する。GET/PUTの全操作で既存nativeControlMuと共通file lockを取得する。helper実行のたびにSHAを照合し、固定Voice unit PIDをbounded systemctlで取得する。vendor childはcontext killしない。結果はroot専用fileに保存し、wire値とcleanup両方がある場合だけ受理する。

PUTはstrict `{expectedValue:0|1|2,enabled:boolean}`。fresh getでの一致、純正linkguard、native85とdisk544の変更前一致、変更後の両値保存、最終live getを要求する。wire2は必ず変更不可。未応答・helper失敗・保存未確認をONや成功へ置き換えない。Amazon認証済みやBluetooth接続中との判定に流用しない。

frontendにはNP1 SERVICE内のAlexa画面を追加した。320/390px×light/dark、fixed ONのreadonly、OFF→ONの確認済み結果、未取得・競合時の非成功表示と再取得、二重送信禁止を合成fixture7件で検証し合格。Goのpure protocol3件に加え、strict HTTP schemaとhelper結果/guardの4件、vet、Linux ARM64 compile、Vue型検証も合格。Web経由の実機API動作と実際の呼びかけ認識は別の確認項目である。
