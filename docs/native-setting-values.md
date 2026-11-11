# 純正設定の保存値と表示ラベル

2026-11-11。実機ABI呼出し・設定writeなし。`native_labels.go`は純粋なラベル/options関数であり、設定変更を許可する機能ではない。呼出し側は既存のSetting/libAplLib fingerprintと共有map形式検査を通した後だけ使う。未確定enumは空options/unknownのまま返す。

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

ID50/51の既存Tailgated両方向変換も関数へ引き継いだ。保存0=ON/1=OFF、感度0/1/2=低/標準/高であり、wire enumの1/2や1/2/3とは異なる。これは単純な0=OFFのbool表示を避けるための共通のラベル層である。

privacyの機能は、公式取説でもcloud upload等を抑えSD常時録画は維持すると説明される。上表のImageRecog branchと整合するが、実機でwriteして通知や保存を確認したという意味ではない。[公式プライバシーモード](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/0B8E75E68E7D26E248A6E1D7598C4B06.php)

## 範囲が分かってもまだラベルにしない値

- **44 案内頻度**: Setting入力変換表`0x197030`はJSON1→保存2、0→1、2→3。保存1/2/3のどれが少/標準/多か、逆消費branchまで未確定。初期値2だけでは推定しない。
- **62 初心者向け**: 共通bool表の入力までは追跡済みだが、独立した逆消費branchが未確認のため今回は人間向けON/OFFを出さない。
- **68 経路優先度**: Settingの0〜5→0〜5表とNavigation `0x19cc88`のidentity table `0x436fe0`は確認。SDK内`0x244cf8`にはrecommended/time/distance/mainRoad/tollの文字列→0/1/2/3/4変換があるが、保存5のpersonal分岐を含む全経路の接続が未確定なのでoptionsへ固定しない。公式の6選択肢順から番号を割り当てない。[公式ルート優先度](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/656757A9F2C2ACB139458C7EE0B23EED.php)
- **77/78 コエ**: Settingの変換表`0x1970a0`以降にはcloud100〜112→保存0〜12の13組がある。公式画面の6声だけから全保存値を割り当てることはできない。既定値77=0/78=1と公式の既定声の一致だけも十分な証明ではない。[公式コエ替え](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/A0D3DA9332568C1DD070744452989ABA.php)
- **79/80 発進/信号通知**: 公式は効果音と音声/効果音のみ/通知しない。3択の意味を単なるboolへ変換せず、ImageRecog初期化の消費branchと通知種別を継続照合する。[公式サービス設定](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/B5B0132AC5AE41FCEDA56C6AFB5714D4.php)

## 材料と再現

所有端末からNavigation、Voice、InfoMtgEngine、VirtualPassenger、ImageRecog、VoicePlay2ライブラリを読み取りコピーした。実行していない。`resources/native-labels`と`tmp/native-labels`はGit除外。VOICEPLAYのstatic/dynamic DBを参照する実装は確認できたが、具体声名と保存値の対応表はまだ取得していない。VirtualPassengerのResourceフォルダにはcertのみで、声名のローカル設定表は見つかっていない。

`tools/inventory_np1_setting_consumers.py`はホストでELFのPLTとAArch64 BL/Bを走査し、GetStatusの呼出し位置・近傍命令・EH frameの関数範囲をprivate JSONへ出す。近傍IDは候補にすぎず、表の意味は関数全体と入力/消費branchを手動照合した。依存はpyelftools/capstone。vendor ELF実行や実機の設定変更は行わない。

```powershell
python tools/inventory_np1_setting_consumers.py resources/Setting resources/native-labels/Navigation resources/native-labels/VirtualPassenger resources/native-labels/ImageRecog
cd backend
go test ./internal/adapters -run NativeStorage -v
```

Goテストは反転値、未確定ID/範囲外値のunknown維持、呼出し側からのoptions改変が他の呼出しへ波及しないことを確認する。
