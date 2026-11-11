# 純正設定vendor helperの設計と試験入口

このhelperはBluetoothのフレームを推測して再送する代わりに、純正 `OnMobilePhoneSetting` が実行する `AL_SettingService::SetStatus` とAIPI通知の組合せを使う。Webの書込みにはまだ接続していない。対象は後続車検知(ID50)・感度(ID51)だけ。

## 成果物と再現

- ソース: `tools/native/native-setting-helper.c`、portable core・firmware/PLT allowlist・AArch64 startupも同directory。
- ビルド: `python tools/build-native-helper.py`。通常のUI/Goビルドは変更しない。
- 成果物: `artifacts/native-helper/ncc-native-setting-helper` とsource hash付き `build-info.json`。vendorバイナリは同梱しない。
- ホスト検証: 同じC coreをWasmにcompileしてNodeで実行。SHA256既知値/55・56・64・129・5000byte境界、厳密な2項目native enum、header破損/未初期化、mutex再帰・順不同解放・上限、共有memoryの再open/重複/上限を検証。
- ELF検証: AArch64、vendorの起動時DT_NEEDEDなし、初期化/cleanup/lock/memory guardのdynamic exportを検証。
- constructor解析: `python tools/native/audit-vendor-elf.py`。pinned vendor11 libraryの202 init-array entry、再配置、direct callとindirect branchの一覧を `tmp/native-settings/constructor-audit.json` へ出力。binaryを実行しない。direct-call一覧だけで全indirect callやOS libraryの安全を証明したとは扱わない。

試験の順序は以下。所有者用persistent材料はnoexecの `/overlay/owner-apps/np1-control/experimental/ncc-native-setting-helper` に置き、launcherが固有 `/run/np1-native-setting-trial.XXXXXX/` へcopy・chmod0700して実行する。

```sh
sh native-setting-trial.sh preflight
sh native-setting-trial.sh vendor-read
# native保存値で指定。例は、現在値が1であると確認された感度を低へ変更後、元へ戻す試験。
sh native-setting-trial.sh roundtrip tailgating-sensitivity 0
```

`preflight` はvendor DSOをロードしない。fixed file SHA256・O_RDONLY snapshot・既存SysV semaphore照会だけ。旧71392f8c版で実機preflightはroot担当が合格し、vendor未ロード・純正7service PID/state維持を確認。最新版のhashはbuild-infoで照合する。

`vendor-read` と `roundtrip` はlauncherが明示的に `NCC_NATIVE_TRIAL=1` を設定した場合だけ進む。このレビュー担当は実機でこれらのABI呼出し・writeを実施していない。

## 初期化を避ける根拠とguard

1. 起動時にはlibc/libdl/libpthreadだけをlink。vendor DSOはpreflight後のdlopen。Settingとvendor11 DSOのSHA256を照合し、未知FWでoffset/ABIを使用しない。
2. payload initialized=1、marker+4=1、updates-disabled(+12)=0、offset44/count86、全7168byte payloadの2回一致を要求。
3. NStandard ctor `std_lib_init`(0x1ee28)には初回clientで `RemoveAllTemporaryFiles`(0x1f25c)へ進む経路がある。`NSTDLIB_EXECUTING_FLAG/WAITCLEAN_FLAG`、`NPCOMMONLIB_EXECUTING_FLAG/EXECWAIT_FLAG` の4個について、既存keyだけをsemget(flags0)しGETVAL>0を要求する。vendorが新しいsemaphore/fileを作ろうとする経路はprocess-local wrapperで止める。
4. global cleanup・所有権変更・semaphore IPC_SET/SETVAL/RMID・unlink/rmdir・thread creation・受信待ちを遮断。stockコードやファイルをpatchしない。NSystemErrorのnarrow/wide両入口もhelper内で停止させる。
5. 元EnableSettingはstartup mode照会と未初期化ロードを含む。このhelperでは同じsymbolをprocess-localにinterposeし、既存snapshotがreadyのときだけtrueを返す。default/load/initは呼ばない。
6. これでもGet/Setに必要なdata pointerは欠落しない。GetStatus内部0xdbc10は毎回、0xdbc58でvendor lock、0xdbc6cでPMEMをOpen、0xdbc70でNULL検査し、その返却pointerからcount/offset/valueを読み、0xdbc88でCloseする。SetStatus内部0xdc080も0xdc0d4 lock→0xdc0e8 Open→0xdc0ec NULL検査→count/offset/write→0xdc114 Close。Enableでpre-openしたpointerを使う構造ではない。
7. NStandardを先にロードし、そのOpen/FreeMemory実体を解決してからAplLibをロードする。NStandard自身のinit-arrayはlocal mutex/空のmemory-object/定数/stdlib runtimeのみで、初回PMEM Openが必要ならbootstrap guardで失敗させる。AplLibのstatic manager/mutex objectは通常ctorで構築される。
8. 対象DSOにDT_SYMBOLIC/DF_SYMBOLICが無いことを静的確認。RTLD_DEEPBINDは使わない。ロード後にpinned PLT slotがhelperのEnableSetting/cleanup/mutex/Open/FreeMemory/semop guardへ解決したことを実pointerで照合し、違えばGet/Set前に停止する。

## AIPIを伴うapplyの順序

- generic `AIPI_ExternalInterface` をhelper内だけで構築し、CSSET proxyの所有者を用意する。純正AppManagerへの登録や純正サービス再起動は行わない。
- ExternalInterface ctor(0x276ac8)はローカルobjectを初期化してm_pInstanceを設定する。receiver Create、StartMsgWait、event threadは呼ばない。
- CSSET GetInstance(0x2e27b8)はm_pInstanceを前提としてRegisterProxyするため、これを飛ばした単純なdlopen+SetStatusは採用しない。
- `AIPI_Core::Connect(proxy,100)` が成功0を返すことをSetStatus前に要求。100はservice enumではなくmsの再試行予算。0x276244〜0x27627cでfloor(n/100)+1回、失敗時usleep100000を行うので100では最大2回/約200msの接続試行になる。
- 接続先はNProcessManagerの既存Setting PIDから解決し、NProcMsgForwarderで既存純正message queueへ接続する。自前の共有queueや偽のSetting processを作らない。
- vendor GetStatusで現在値を確認し、要求expectedValueと一致したときだけvendor SetStatusを呼ぶ。さらにvendor GetStatusと独立O_RDONLY snapshotの両方で要求値との一致を確認。
- native50は0=ON/1=OFF、51は0=低/1=標準/2=高。wire enumの値は受け付けない。privacy等はallowlist外。
- SetStatusは更新・dirty flag・AIPI_CSSET通知を行うが、通知先の機能実動作まで戻り値が保証するわけではない。helperは `stored-readback=matched; recipient-functional-check-required` と出力し、純正サービス/対象機能の別検証を必要とする。

## waitの上限と失敗時のlock/refcount

| 箇所 | 根拠・対策 |
| --- | --- |
| shared/private pthread mutex | vendorのlock/timedlock/trylock/unlockをhelper内でinterpose。通常lockも実pthread_mutex_timedlockの500ms deadlineで実行。timedlockは要求deadlineと500msの短い方。成功取得を再帰回数ごとに追跡し、通常unlockで除去 |
| SysV semop | 操作配列を変更せずkernel semtimedopの相対500msへ渡す。IPC_NOWAITのEAGAINはvendorの既存client分岐に必要なため保持。blocking操作失敗はguard停止。SEM_UNDO bitを除去しない |
| NStandard ready待ち | 既存ready>0をpreflightで確認。0x1f2c4から最大100回×10msのGETVAL loop。後続semopにも上記500msを適用 |
| NProcSharedMemory Open | EACCESのretryは100回×10ms。missing/新規作成はguard停止。内部mutexも500ms |
| Core Connect | 上記100ms予算による最大2回の100ms sleep。mutexも500ms |
| queue AddMsg | NStandard 0x309f8。ring-buffer容量を判定し、不足時はerrorを返す。cond待ちはしない |
| AIPI send retry | CoreがBase::Sendへretry5回・sleep50msを渡す(0x2751fc等)。Base::Send(0x275340)はqueue busy時だけ有限retry。各内部mutexも500ms |
| reply/受信 | 使わない。SendMsgSync/MsgWaitLoop/StartMsgWaitを呼ばない。予期しないpthread_cond_wait/timedwaitは実待ちに入る前に停止 |

NProcSharedMemoryはheader+0x70の共有refcountをOpen時に増やし、FreeMemoryで減らす。これはSysV SEM_UNDOと別物であり、rawexitだけで自動復帰するとは扱わない。

helperはOpenに成功した**自分のNProcSharedMemory object**を追跡し、FreeMemory/Close/destructorの通常解放で記録を消す。guard停止ではまず自分が取得したmutexを解放し、残っている自分のmappingをvendor FreeMemory経由で回収する。設定map/headerの直接writeでcounterを巻き戻さない。cleanup途中の別失敗でも、その時点で自分が持つmutexは再解放して非ゼロ終了する。回収自体が失敗した場合を成功扱いせず、private read-backを確認して盲目的に再試行しない。

global runtimeのclient参照は、静的データで確認した+1/SEM_UNDO操作を保持するため、guard停止時にもkernelが自己分を戻す。通常のdlclose/finiは通さない。ExtIF destructorと追跡mappingのvendor FreeMemoryを、全DSOの同期objectが生きた状態で明示実行し、所有lock/mappingゼロおよび全対象headerのrefcount一致を確認してrawexitする。DSOのprivate heap/FDはkernelが回収し、global client参照はSEM_UNDOで自己分を戻す。

vendor shared mutexのrobust性には依存しない。vendor区間ではHUP/INT/TERMをdeferし、取得成功から追跡登録までの隙間で終了しない。lockに入る可能性がある区間は上記内部wait上限を使う。外部timeoutからSIGKILLする設計は採用しない。SIGKILL/電源断の一般的な全状態復旧保証はできず、OS I/O停止までwall-clockで保証したとは扱わない。

## launcherの後処理と合格範囲

`native-setting-trial.sh` は専用flock、純正7serviceのPID/ActiveState/SubState、元の2項目値、helper/source SHA、前後のprivateログを保持する。roundtripの後処理は現在値を再取得し、試験で要求した値と一致する場合だけexpectedValue付きvendor APIで元へ戻す。別値・不明なら盲目的な復元をしない。失敗・signal時もread-backとservice比較を行う。

PID/state一致だけで機能合格にしない。root担当がvendor-read後、次に1項目roundtrip後、それぞれ純正Communication/DriveRec/ImageRecog/Voice/QMMFの機能確認を行う。getter・元値復帰・callback到達/対象機能・必要なら再起動後の永続性が確認されるまでWeb writableを開放しない。

## controlled read試験で判明した互換修正

root担当の実機試験では、最初のvendor-readはload時に停止し、dlerror診断で `libNSystemInfoLib.so` が見つからないことを確認した。純正SettingにはDT_RPATH=/usr/local/libがあり、helperには無かった。helper子プロセスだけ `LD_LIBRARY_PATH=/usr/local/lib` を付与し、本体ld.so/systemd環境は変更していない。

次の試験ではGetStatus50/51まで進んだが、dlcloseの終了順で `vendor-fini` に入り、NStandard内+0x3dd74/+0x31e24が破棄済みmutex(kind=-1)へ入ろうとしてEINVAL22を返した。全7service PID/stateとafter preflightは維持。500ms不足ではなかったため待ち上限は変えていない。

修正版はprocess-lifetime DSOとして保持し、ExtIF destructor→明示的vendor FreeMemory→所有資源ゼロ・refcount一致→rawexitの順とする。自分が初めてOpenする前に各mapのheader+0x70をO_RDONLYで記録し、全回収後に同じ値へ戻ったことを要求する。4global semaphore値もpreflightに記録し、launcherのbefore/after比較でSEM_UNDO自己分の復帰を確認する。他の純正actorによる変化があれば判定不能として成功扱いにしない。

loader detailは384byte上限・制御文字除去。mutex診断はrc/固定stage/操作/呼出しlibrary+offsetとraw mutex wordsをprivate stderrだけへ保存する。無効pointerを直接dereferenceせず、自己process_vm_readvで診断する。これらのraw診断をGitや公開画面へ載せない。

## 変更通知の受取と永続化を区別する観測

固定firmwareの静的根拠は次のとおり。受取側が処理した証拠と、helperが共有値を書き戻せた証拠を区別する。

- Setting `0x999f0` は `AIPI_CSSETRequestSettingStatusChangedEvent` の `+0x4c/+0x50` からID/保存値を取り、`CSSET_SRV_SET_StandardSetting -- OnAIPI_CSSETRequestSettingStatusChangedEvent ID:[%d];Status:[%d] --` を出す。続いて3000msでtimerを再始動し、backup要求フラグを立て、`NotifyCSSETSettingStatusChanged` で購読先へ通知する。これはhelper側の送信ログとは異なる受取証拠になる。
- timer ctor `0x993e8` のintervalも3000ms。timeout `0x99348` はbackup要求をqueueに積み、既存backup thread `0xde6c0` が `0xde510` のDoActionから `SaveSettingData` を実行する。ACCoffでもbackup要求を積む経路がある。
- `AL_FilePathBase::GetPartition(kind1)` は `/pio_data`。Backupは `/SYSTEM/SETTING/Setting.DAT` を構成する。既存ファイルは `SettingTemp.tmp` へrenameし、新しい内容をWrite/Flush後に一時退避を削除する。helper自身にこの保存処理を実行させない。
- Set内部 `0xdc160/0xdc164` は保存値とpayload+8のdirty=1を更新する。BackupのComposeは `0xdbf38` で値をcopyする途中、`0xdc020` でdirty=0にする。**dirty=0はディスクへのWrite/Flushより前なので、永続化成功の単独証拠にはならない。**
- roundtripでは受取ログの対象ID/要求値、3秒以降の固定保存ファイルの変更、復元後の保存値をprivate記録で照合する。mtimeだけでも任意の他設定更新が混ざるため不十分。保存format中の対象値または復号済み固定項目のread-backが必要。再起動後の保持は別試験として扱う。

root担当のcontrolled試験ではbd9f3b6b版のvendor-readがexit0。純正7service PID/state、4global semaphore、自己mappingのheader refcountは前後一致した。続く感度1→0→1試験は共有値変更と原状復帰が確認されたが、apply/restoreともSDK内openのtruncate/exclusive保護でexit78となり、成功にはしていない。診断版は保護条件を変えず、失敗時のstage/flags/caller library+offset/pathを最大192byte・制御文字除去でprivate stderrへ記録する。任意path許可や直接map writeによる回避はしない。

## APN共通helperへの入力境界（制御本体は未接続）

`tools/native/apn-input.h` はvendor APIを実行せず、private FDから渡す予定の固定入力を検証する。root-only recovery serializer担当と次の316byte形式で合意した。

- 先頭8byteはASCII `NCCAPN1\n`。
- 続く154byte recordを2つ。先頭4byteはprofileIndex/auth/PDP/reservedで、profile順は1→2、auth=0..3、PDP=0（WDS IPv4）、reserved=0のみ。
- APN/user/passwordは各50byte固定NUL終端・zero pad。最大49文字、ASCII32..126、引用符とbackslashを拒否する。APNは非空。純正liblteapiがATコマンドの引用符内へescape無しで埋め込むため、この制約を復元元にも適用する。
- 長さ違い、順序違い、未対応PDP、終端後の余計な値も拒否。途中で失敗した場合、出力structureの資格情報をvolatile zeroizeする。入力元bufferの消去は呼出し側も行う。
- argv/env/logへ資格情報を展開しない。公開JSONにはbackupの可用性と件数だけを返す。

`python tools/build-native-helper.py --test-only` はこの同じC parserをWasmへcompileしてNodeで実行する。長さ、2profile順、auth/PDP、49文字境界、NUL padding、AT区切り、制御文字・非ASCII、失敗時zeroizeを検証済み。設定helper ELFへはまだ組み込んでおらず、APNの実機制御成功を示すものではない。NEventWatcher callback/受信と共有lockの所有追跡、変更前2profileの完全backupと正規復旧経路を確認してから接続する。

## 既存SDKログmapのcreator probe

感度roundtripのopen診断では、flagsはO_TRUNCではなく `0xc2`（O_RDWR|O_CREAT|O_EXCL）、callerはNStandard+0x3247c、対象は既存 `PMEM_AL_LOG_BUF` mapだった。AL_LogCore ctor `0x90740` がpayload size0x1205cでAllocするため、128byte header込み73948byteになる。

NStandard `0x32478` のopen失敗後、errno17は `0x32870→0x32684` へ進み、creator専用のfchmod/所有権変更/header初期化を迂回する。helperはこの固定path・固定callsite・完全一致flagsに限り、O_RDONLY|O_NOFOLLOW|O_CLOEXECで既存fileを開き、arm64 fstatでregularかつ73948byteと確認した場合だけEEXIST/-1を返す。実O_CREAT/O_EXCLは発行しない。確認後に削除される競合があってもhelperが新規作成することはなく、後続Openのmissing guardで失敗する。

それ以外のexclusive/truncate操作は従来どおり拒否する。この変更はログファイルの初期化や任意pathの許可ではない。対応版を実機で試すのはroot担当とし、適用・元値復帰・受取/保存確認が揃うまでWeb writeは未開放とする。

固定backup formatは `tools/native/setting_backup.py` でhost側から読取れる。先頭16byteはuint32のmarker=0/offset=16/revision/count=86、続く86件はlittle-endian `(persistentID:uint32,value:int32)`、全704byte。Composeが使う静的ID配列（libAplLib+0x189a80）のnative index50/51はpersistentID506/507であり、その後ID昇順にsortされる。readerは既知ID集合、昇順、重複なし、長さと2項目enumを厳密確認し、出力を50/51に限定する。未知formatは取得不能とする。合成データの3件テストは合格、実機保存copyでの照合はroot試験待ち。

対象機能側も固定ImageRecogの静的経路を照合した。`0xa7f48` がGetStatus50/51を読み、検知保存値0=有効・1=無効、感度0..2を検証し、`0xa80fc` で `CIMGN_CommonLib::set_setting_value(CIMGN_TailGatingSetting*)` へ渡す。`0xa9718` の運用loopはflag条件中に `0xa9794` からこの関数を呼び、100000us sleep後に反復する。したがって保存値から認識エンジンへ設定を渡す経路は確認できる。ただし実機がそのloopの有効状態にいることや、実車の接近検知発報はこの静的照合では未検証とする。
