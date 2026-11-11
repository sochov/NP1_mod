# 本体自動更新の抑止・調査引継ぎ

2026-11-11。実機変更なし。ユーザー指示により本体自動更新を導入時に抑止する経路を調査したが、以下は適用済みの設定ではない。新たな4G/APN最優先作業へ移るため、現時点を保存する。

## 実体と対象範囲

実機の`app-@Update.service`はgeneric template `/etc/systemd/system/app-@.service`のinstanceで、ExecStartは`/usr/local/bin/Update`。AppManagerをRequires/Afterに持つ。Update ELFは`libfotaapi.so.0`を使用し、firmware/application/rodata更新、download/verify/activate、中断や進捗状態を持つ。

`ab-updater.service`も存在するがunit-file一覧ではdisabled。名前だけでNP1の自動更新所有者と決めつけない。現在動いているapp-@UpdateとAppManagerの起動/監視経路との関係を先に確認する必要がある。

## 有望な既存flag候補

Update ELFの`.rodata`仮想address `0xbba68`に`/SYSTEM/Update/no_ota_download`がある。global constructor `0x2e938`の`0x2e9f0〜0x2ea14`がこの文字列をNString global `0xf6dd0`へ初期化する。周辺にdownloadlist.xml、download完了/更新完了request bodyのパスがある。

この時点では**文字列とglobal初期化を確認しただけ**であり、flagの存在判定/内容判定・AUTO/manualの分岐・実際のmount prefix・既にdownload/verify/activate中の扱いは未確認。ファイル作成やunit maskを実装/実行する根拠にはまだ不足する。優先する次の静的解析は、global `0xf6dd0`の消費点と`CUPD_ENT_DU_HttpClient::GetDownloadList(CUPD_ENT_DU_CheckDataMethod)`、`CHECKDATAMETHOD_AUTO`の分岐。

本物のauto-download制御flagを確認できれば、専用OTAunit停止/maskより優先する。署名、version、payload、rootfs/rawblockは変更しない。進行中の更新を停止しない。Voice/GPS/一般cloud更新をまとめて止めない。

## 保存資料

- `resources/updates/{Update,AppManager,System}`: 所有実機からread-only copyしたELF。実行なし、Git除外。
- `tmp/update-inventory/units-and-paths.txt`: unit/read-only inventory。
- `tmp/update-inventory/{Update,AppManager,System}-strings.txt`: 静的調査用文字列。Git除外。
- `tmp/update-inventory/flag-0x2e938.txt`: flag文字列の初期化関数逆アセンブル。

root担当が設定API/専用flagの真偽、状態read-back、元設定のbackup/rollback、再起動後維持を確認するまで、自動更新無効化の成功表示をしない。

## 消費経路の確定と実装

追加静的解析でGetDownloadList `0x9fcf0` のflag消費が確定した。GetPartition(kind1)=`/pio_data`＋固定相対pathをSearchFileし、結果1ならHTTP list取得前にfalseで終了する。内容の読取りはなく、AUTO/manual共通の新規取得抑止であり、進行中のdownload/verify/activateを中断するものではない。

core `backend/internal/otapolicy`、root用HTTP adapter、`OTASettings.vue`、再現script `tools/device/ota-policy.sh` を追加。backup/復旧/中央配線/検証条件は [OTA変更計画](change-plan-ota-policy.md) に集約した。host検証済み、実機適用と再起動検証はroot担当待ち。旧調査時点の「未実装」と新しい実装状況を区別する。

中央配備で初期停止を実機適用し、Web GETのdisabled/managed/writableがすべてtrueであることを確認した。繰返しのNCCアップグレードでも管理記録と選択を維持する。再起動後の存在確認は引続き受け入れ項目とする。
