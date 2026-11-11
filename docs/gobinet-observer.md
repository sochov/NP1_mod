# GobiNet NAS観測の調査と実装

## 結論

実機のGobiNetモジュールを読取り専用で取得し、FDごとのCID分離とNAS照会ABIを静的解析で確認した。`modem.ReadGobiNetSignal(ctx)` は新規NAS clientだけを割り当て、Get Signal Infoだけを送信する。実機への追加FD open・ioctl・QMI照会は、この調査および実装担当では実施していない。実装のコンパイル成功を通信共存の実機合格としない。

## 一次資料

Quectelの[Linux USB Driver User Guide V3.1](https://quectel.com/content/uploads/2024/02/Quectel_UMTS_LTE_5G_Linux_USB_Driver_User_Guide_V3.1.pdf)はGobiNetをベンダー提供のドライバとして説明する。ただしこのガイドだけでNP1搭載版のABI一致は断定できない。

Code Aurora由来の[qcusbnet qmidevice.c](https://android.googlesource.com/kernel/tegra/+/b899528390a1b4d13b28177e6b27b4590fab9955/drivers/net/usb/qcusbnet/qmidevice.c)では、openごとのprivate_dataにCIDを保持し、GET_SERVICE_FILE ioctlでclient割当、read/writeはそのCID、releaseは同CIDの解放となる。これは設計の参考であり、NP1版の代用証拠にはしなかった。

実測値の形式は公式libqmiの[NAS schema](https://github.com/linux-mobile-broadband/libqmi/blob/main/data/qmi-service-nas.json)と[CLI実装](https://github.com/linux-mobile-broadband/libqmi/blob/main/src/qmicli/qmicli-nas.c)で確認した。Get Signal Infoは0x004F、LTE TLVは0x14、値はRSSI=int8、RSRQ=int8、RSRP=int16、SNR=int16。SNRは0.1dB単位であり、SINRと同じフィールド名に置き換えない。

## NP1版の静的確認

2026-11-11、接続実機の `/sys/module/GobiNet/version`、srcversion、クラスdev番号、USB親子関係、FD所有者をread-onlyで調査した。

- version: `Quectel_Linux&Android_GobiNet_Driver_V1.6.2.14`
- srcversion: `533BB7E5866E52F63B9ACCB`
- module: `/lib/modules/4.14.206-perf/kernel/drivers/net/usb/GobiNet/GobiNet.ko`
- サイズ: 1,025,145 bytes、AArch64、symtab付き
- SHA256: `cbe36e78b6a174283c9757c2e1fe6e1da242e8cc2c465f19160b5a1205f6db72`
- `/sys/class/GobiQMI/qcqmi0/device` はUSB modem interfaceを指す。採取時点で `/dev/qcqmi0` を開いた他プロセスのFDは見つからなかった。

モジュール本体と詳細解析レポートはGit管理外のtmpに保持する。再現ツールは `tools/analyze_gobinet.py`（pyelftools/capstone使用）。モジュールをロードせず、ローカルELFだけを読む。

確認したコード経路:

| 関数・位置 | 確認した動作 |
| --- | --- |
| UserspaceOpen 0x8ee8–0x8f00 | file+0xc8に新規16byte構造体、先頭CID=0xffff、+8にdeviceポインタ |
| UserspaceunlockedIOCTL 0x89bc、0x8d24–0x8d34 | ioctl 0x8be1を判定し、引数serviceをGetClientIDへ渡し、そのFDのCIDへ保存 |
| UserspaceRead 0x81c4–0x81e0、0x8200 | FDのCIDでReadSyncし、6byte QMUXを除去 |
| UserspaceWrite 0x84b8、0x84d4、0x8554–0x8564 | 入力の前に6byte分を確保し、FDのCIDでWriteSync |
| UserspacePoll 0x8850–0x888c | FDのCIDでFindClientMemし、そのclientキューを確認 |
| UserspaceClose 0x907c–0x90f8 | 同FDのCIDのみをReleaseClientIDへ渡す |

この搭載版のioctl判定は0x8be1/2/3で、参考ソースにある0x8be4 CLOSEは使用しない。GetClientID失敗時にはservice=0x1a(WDA)限定で既存CIDを解放する特殊経路が存在するため、汎用service選択を公開しない。NAS=3固定ではこの分岐に入らない。

## 実装の境界

ドライバ版・srcversion・モジュールSHA256をpinし、デバイスがchar deviceでsysfs devと一致し、USB vendor/productが検証対象であることを確認する。全プロセスのFDを調査できない場合や他プロセスがqcqmi0を開いている場合は拒否する。初期実装はper-CID分離を利用しつつ、同デバイスを開く他プロセスとの同時利用を保守的に避ける。

ioctlは `0x8be1`、引数はポインタではなく整数3。送信はQMUXなしの7byte service header（request flags、transaction uint16 LE、message=0x004f、TLV length=0）。受信もQMUXなしとし、response flags、transaction、message、総長、個々のTLV長、重複TLV、Operation Resultを検証する。最大4096byte、無関係フレームは最大32件。有効範囲外・未取得の数値はnull。SNRフィールドを別に持ち、SINRへ代入しない。

ポーリングは100ms以下ごとにcontextを確認し、照会応答待ちは最大3秒。ただしドライバのopen・CID ioctl・closeはkernel内で待機する可能性があり、Go contextだけで強制中断できない。既知実装のReadSyncもO_NONBLOCKに依存しないため、single readerでpoll ready後だけreadする。実行する場合は外側の単発helperプロセスにも時間制限を設ける。timeoutを理由に純正サービスのFDを閉じたり、driver unbind/rebind/resetを行わない。

WDS接続、WDA設定、APN、SIM、インターフェース登録、セル探索、indication登録、reset、CTL同期などの操作は公開しない。CID割当と解放は一時的なモデム状態を伴うため、完全に受動的なファイル読取りとは区別する。

## 検証

- Windows上の共通parser単体テストとgo vet合格。
- parserのfuzzテストを2秒実行し、1,784,225入力でpanic等がないことを確認。
- リクエスト固定値、符号・SNR単位、未取得null、LTE TLV欠測、失敗Result、長さ不整合、truncated/duplicate TLV、別transactionを確認。
- `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c ./internal/modem` 合格。実機でテストバイナリは実行していない。
- 実機でのNAS照会成功・純正通信共存・録画共存は、この記録時点では未検証。
