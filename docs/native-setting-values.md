# 純正設定の保存値と表示ラベル

2026-11-11。この文書のenum根拠はvendor ELFの静的解析から得た。実機変更の結果は個別helperの試験記録へ分ける。`native_labels.go`は純粋なラベル/options関数であり、設定変更を許可する機能ではない。呼出し側は既存のSetting/libAplLib fingerprintと共有map形式検査を通した後だけ使う。未確定enumは空options/unknownのまま返す。

## 対応を確認した値

SettingのPersonalInfo入力はJSON値をそのまま共有mapへ写すのではなく、`.data`の変換表を通す。bool変換表`0x197020`は入力1→保存0、入力0→保存1。`UpdatePersonalInfo`の各入力と、Navigation/VirtualPassenger/ImageRecogの逆方向getter/消費branchを照合した。

| Setting ID | 保存値→意味 | 入力側の根拠 | 逆方向/消費側の根拠 |
| --- | --- | --- | --- |
| 8 privacy | 0=ON、1=OFF | Setting `0x93880`、`0x93904〜0x93950`で0/1 toggle。boot通知は`0x9500c`で0の場合に発話処理 | ImageRecog `0x66f70〜0x66f88`、`0x75100〜0x75118`はprivacyが非0の時だけupload可能。後者ログ名はCheckPrivacyModeDisable |
| 39 driving support | 0=有効、1=無効 | Setting JSON driving_support→共通bool表→`0x97db8` | Navigation `0xdc524〜0xdc52c`で保存1なら支援処理をskip |
| 49 service cabin camera | 0=許可、1=許可しない | Setting `allowed_using_in_camera`をboolとして読む`0x96750〜0x9675c`、共通bool表→`0x981ec` | VirtualPassenger `0xa3484〜0xa34a0`が保存byteをconsumerへ渡す。`0xf1390`の実ログ書式にinCamera「0:OK 1:NG」。ユーザー初期化も`0xa362c〜0xa3640`で0へ戻す |
| 61 auto traffic check | 0=自動確認する、1=しない | Setting auto_traffic_check→共通bool表→`0x97e9c` | Navigation `0xdf2d4〜0xdf2d8`は非0でdisableへ分岐。`0x421fb8`はTrafficGuide is disable。別にroute/check-in/phone条件も必要 |
| 63 traffic congestion | 0=考慮する、1=しない | Setting route.traffic_congestion→共通bool表→`0x97f6c` | Navigation getter `0x19caa8`、`0x19cadc〜0x19caf0`は保存0をtrueとして返す |
| 64 time regulation | 0=考慮する、1=しない | Setting route.time_regulation_congestion→共通bool表→`0x97fd4` | Navigation getter `0x19cb08`、`0x19cb3c〜0x19cb50`は0→true |
| 65 ferry | 0=利用する、1=しない | Setting route.use_ferry→共通bool表→`0x9803c` | Navigation getter `0x19cb68`、`0x19cb9c〜0x19cbb0`は0→true |
| 66 toll road | 0=利用する、1=しない | Setting route.use_toll_road→共通bool表→`0x980a4` | Navigation getter `0x19cbc8`、`0x19cbfc〜0x19cc10`は0→true |
| 67 smart IC | 0=利用する、1=しない | Setting route.use_smart_ic→共通bool表→`0x9810c` | Navigation getter `0x19cc28`、`0x19cc5c〜0x19cc70`は0→true |
| 68 route priority | 0=おすすめ、1=距離が短い、2=道が広い、3=到着が早い、4=料金が安い、5=あなたに合わせる | Settingの0〜5 identity変換 | NavigationのSettingService→検索条件変換→SDKpriorityとpersonalize JSONを下記で照合 |

ID50/51の既存Tailgated両方向変換も関数へ引き継いだ。保存0=ON/1=OFF、感度0/1/2=低/標準/高であり、wire enumの1/2や1/2/3とは異なる。これは単純な0=OFFのbool表示を避けるための共通のラベル層である。

privacyの機能は、公式取説でもcloud upload等を抑えSD常時録画は維持すると説明される。上表のImageRecog branchと整合するが、実機でwriteして通知や保存を確認したという意味ではない。[公式プライバシーモード](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/0B8E75E68E7D26E248A6E1D7598C4B06.php)

## 範囲が分かってもまだラベルにしない値

- **44 案内頻度**: Setting入力変換表`0x197030`はJSON1→保存2、0→1、2→3。保存1/2/3のどれが少/標準/多か、逆消費branchまで未確定。初期値2だけでは推定しない。
- **62 初心者向け**: 共通bool表の入力までは追跡済みだが、独立した逆消費branchが未確認のため今回は人間向けON/OFFを出さない。
- **77/78 コエ**: 下記消費側解析で保存0〜5は確認済み内部voice名をreadonly表示する。6〜12は未対応/unknownのまま。Settingの変換表`0x1970a0`以降にはcloud100〜112→保存0〜12の13組があるが、公式画面の6声だけから全保存値や女性1等の表示名を割り当てることはできない。[公式コエ替え](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/A0D3DA9332568C1DD070744452989ABA.php)
- **79/80 発進/信号通知**: 下記の追加解析で3択のラベルは確定した。write・永続保存・復元の検証は別工程。

## 材料と再現

### 68の検索条件への接続

Navigationの`CCLN_VN_SettingService` vtable `0x557048` のslot `+0x70` はgetter `0x19cc88`。GetStatus68 (`0x19ccb8`) → table `0x436fe0` のidentity 0〜5を返す。consumer `0x1b0a58` / `0x1b0c00` はこのslotを呼び、その値を検索条件へ渡す。

通常priorityは`0x1b0910`で保存1〜4をtable `0x438918 = [2,3,1,4]`へ変換し、保存0/5はSDKpriority0になる。SDKの文字列parser `0x244cf8` はrecommended/time/distance/mainRoad/tollをそれぞれ0/1/2/3/4へ変換する。したがって保存1=distance、2=mainRoad、3=time、4=tollであり、SDK番号やスマホの表示順を保存値へ流用してはいけない。

保存5だけは`0x1b0a68` / `0x1b0c10`の比較でpersonal flagを立てる。`0x2125c8`で条件objectのbyte `+0x10`へ保存し、`0x22f040`のgetterから`0x2584e0`でrequest byte `+0x28`へ渡す。serializer `0x256c28`はgetter `0x258498`を呼び、JSON key `personalize` (`0x443fc8`)へboolを書き込む。保存5が単にSDKpriorityの範囲外/defaultというだけではなく、personal検索を明示する別flagを伴うことまで確認した。[公式の検索方法](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/656757A9F2C2ACB139458C7EE0B23EED.php)の6選択肢と整合する。これはreadonly labelの根拠で、write試験の合格を意味しない。

### 77/78の追加消費側解析

`libVoicePlayService.so` の静的初期化 `0x23c00〜0x23c98` はtable `0x55448` からvoice type1→設定77、type3→78のmapを作る。consumer `0x43aa4` / `0x44710` はこの設定IDをGetStatusへ渡し、返った保存値を別mapの内部voice IDへ変換する。`0x23cb8〜0x23e58` では保存0〜5の6件を初期化している。Voice実行体の独立したmapにも同じ6件があり、初期化 `0x8c870` / `0x8d5b0`、consumer `0x1e6ea4` / `0x202b60`を照合した。

| 保存値 | 内部voice ID（純正UI表示名ではない） |
| --- | --- |
| 0 | Mizuki |
| 1 | Takumi |
| 2 | Tsutomu |
| 3 | Sakura |
| 4 | YukiAoi01 |
| 5 | YukiAoi02 |

これらを女性1/女性2等の純正UI名へ推測で置き換えない。raw抽出・初期化asm・詳細の対応表はprivate `tmp/native-labels/voice-storage-consumer-proof.json` 等に保存する。

consumerには未登録のmap keyで例外を出す分岐 (`0x43b54` / `0x447d8`) がある。入力変換表に0〜12の値があるだけで13択を開放してはいけない。このDSOのint→voice-ID mapへのinsert呼出しは静的初期化 `0x23db4` にあり、DL処理で6〜12を登録する経路は調査範囲で見つかっていない。77/78のlabel/optionsは0〜5の確認済み内部名だけを返し、6〜12はunknown。専用helperの実機保存・復元試験後に0〜5だけを制御候補へ追加した（[独立helperと試験範囲](native-choice-controls.md)）。内部voice名の取得は、その声で今発話できることやcache取得済みを保証しない。

### voice選択とダウンロード済みの区別

`VPS_VoiceDataLocalCheck::IsVoiceDataExist` (`0x465a8`) はvoice type→現在設定の内部voice ID→TTS serviceへ変換 (`0x46618〜0x4663c`) し、発話text・language・voice IDを使ってDBを確認する。未登録voice IDは`convertVoiceIdToTTSService` (`0x44808`) でunknown service99となる。

`IsDownloadDataExist` (`0x46868`) は要求URLのhashをDBのキーにし、保存されたpath/sizeと実ファイルのsizeを比較する (`0x46bdc〜0x46c54`)。固定text-ID要求もvoice IDと言語を指定してSQLiteを照会する (`0x460b0〜0x4610c`)。これは発話内容単位のcache確認であり、「この声を選択できる」「声一式がダウンロード済み」「今offlineで任意の発話ができる」を同一のavailableとして扱えない。既存ELFの静的解析だけから、現在端末の音声cache取得状況は判定しない。

### 44/62の調査範囲

44の変換table `0x197030` はJSON0/1/2→保存1/2/3であることを再読取りで確認したが、数値順から少/標準/多と割り当てない。62も入力bool→保存値までで、独立した実消費の根拠は未確定。

取得済みNavigation / Voice / InfoMtgEngine / VirtualPassenger / ImageRecog / Communication / VoicePlay2、および手元のApl・NDevice・NEventBase各ELFに対してGetStatusの直接呼出しを再走査した。この範囲に44/62の固定IDを用いたconsumerは見つからなかった。動的map経由やcloud側の適用が存在しないという証明ではない。Voiceの案内頻度を減らす/戻すAIPI要求も見つかったが、44の保存値との接続は未確定なので別機能として扱う。

Setting `0x95630` には引数w1=設定ID/w2=保存値をJSONに変換する候補がある。ID44は `0x958ec〜0x9594c` で逆変換してguide_frequencyのintにし、ID62は `0x95988〜0x959e0` で保存0→true/1→falseとしてbeginner_driver_modeへ出す。`0x9587c`でJSON文字列を生成する。ただしこの候補への実際のcaller/登録箇所が未確定で、unusedの実装copyも考えられるため、稼働consumerと断定しない。双方向の変換候補と、実運用でこのserializerが使われる証拠は分けて扱う。

追加解析でID79/80の表示値も確定した。どちらも保存0=通知しない、1=効果音と音声、2=効果音のみ。ImageRecogの`0xa817c/0xa81ac`からの消費は、0でdetectorのchime byteを0、1/2で1に設定する。1はmessage flagを1、2は0へ分岐する（`0xa83ec〜0xa8430`）。実通知の`0xa77c4〜0xa77d0`と`0xa7aec〜0xa7af8`はそのflagをAIPIへ渡す。libAIPIの`0x36b588/0x36b630`で第1引数→SetChime、第2引数→SetMessageまで照合し、[公式の3選択肢](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/B5B0132AC5AE41FCEDA56C6AFB5714D4.php)とも一致する。ラベルの追加だけで、この2項目のwrite・保存・復元はまだ検証していない。

所有端末からNavigation、Voice、InfoMtgEngine、VirtualPassenger、ImageRecog、VoicePlay2ライブラリを読み取りコピーした。実行していない。`resources/native-labels`と`tmp/native-labels`はGit除外。VOICEPLAYのstatic/dynamic DBを参照する実装は確認できたが、具体声名と保存値の対応表はまだ取得していない。VirtualPassengerのResourceフォルダにはcertのみで、声名のローカル設定表は見つかっていない。

`tools/inventory_np1_setting_consumers.py`はホストでELFのPLTとAArch64 BL/Bを走査し、GetStatusの呼出し位置・近傍命令・EH frameの関数範囲をprivate JSONへ出す。近傍IDは候補にすぎず、表の意味は関数全体と入力/消費branchを手動照合した。依存はpyelftools/capstone。vendor ELF実行や実機の設定変更は行わない。

```powershell
python tools/inventory_np1_setting_consumers.py resources/Setting resources/native-labels/Navigation resources/native-labels/VirtualPassenger resources/native-labels/ImageRecog
cd backend
go test ./internal/adapters -run NativeStorage -v
```

Goテストは反転値、未確定ID/範囲外値のunknown維持、呼出し側からのoptions改変が他の呼出しへ波及しないことを確認する。
