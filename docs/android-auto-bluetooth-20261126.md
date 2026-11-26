# Android Auto: Bluetooth経路の調査（2026-11-26）

市販ナビの無線Android Autoは、スマートフォンがナビとBluetoothでペアリングし、ナビのRFCOMMサービス（UUID `4de17a00-52cb-11e6-bdf4-0002a5d5c51b`）でWi-Fiの接続情報を受け取ってから、ナビのWi-FiへTCPで接続する手順です（[android-auto.md](android-auto.md)）。NP1でこの手順を行うための調査結果をまとめます。RFCOMM上の手順とその後のセッションは `aap.PhoneWireless` として実装・試験済みで、残りはBluetoothの操作です。

## NP1のBluetooth

- カーネルにBluetoothはなく（`/sys/class/bluetooth` なし）、純正の `NEventWatcherCom` が独自スタック（`libanwbtstack.so`、`libAnwManagerMA.so`）でUART経由のコントローラーを占有している。BlueZは使えない。
- 純正の操作はセッションD-Busに公開されている（PICSで使用中の `NPBTLEHandler` と同じ方式）。`SPF.EventBase.NPBTManagerHandler`（管理）、`NPBTSPPHandler`（SPP）、`NWlanHandler`（Wi-Fi）がある。
- 呼び出しは `SPF.EventBase.v1_0.CallFunction(u32 1, 呼び出し元PID, 関数番号, au [], ay 引数, u32 応答長)`。結果は `SPFEvent` シグナル（管理系は `/SPF/EventBase/Evt042`、本文はPID・0・データ）で届く。

### 管理系の引数（`libNDeviceLibCom.so` の `NPBTManagerHandlerProxy` から）

先頭24バイトは共通: `u32 連番, u32 0, u32 関数番号, u32 応答番号(=関数番号-1), u64 クライアント識別子`。続く引数:

| 関数 | 番号 | 引数（+24以降） | 実機の応答 |
| --- | --- | --- | --- |
| SetBluetoothState | 0x00 | u32 状態（1=開始, 0=停止）, u8, BDアドレス, u32 リセット種別 | 未実行（開始済みのため不要） |
| GetLocalDeviceInfo | 0x05 | なし | 拒否（0） |
| Waiting（発見・接続可能化） | 0x09 | u32 走査モード（2=発見+接続可能）, 4バイト詰め | 成功。HCI Write Scan Enable=03 になる（再起動で02に戻る） |
| Inquiry（機器探索） | 0x0b | u32 種別, u8 時間, u32 件数 | 拒否（種別0〜2とも） |
| RequestPairing | 0x0d | BDアドレス6バイト | 未実行 |
| ReplyUserConfirmReq | 0x12 | BDアドレス（+0x18）, u8 承認（+0x1e）, 計0x20バイト | 成功（ペアリング完了を確認） |
| SDPSearch | 0x15 | BDアドレス, u32, u32 | 未実行 |
| Connect | 0x19 | BDアドレス, u32 件数, プロファイル情報×件数（各24バイト） | 未実行 |
| SetProfile | 0x02 | u32 プロファイル集合, UUID情報×最大6（各0xC2バイト）ほか | 未実行（全体を置き換えるため） |
| GetBtManagerStatus | 0x1f | なし | 成功。Evt042で状態（本体のBT名とBTアドレスを含む）が届く |
| GetConnectableProfile | 0x21 | なし（同期、応答8バイト） | 0（成功フラグ0） |

### クラシックBTの状態（2026-11-26 実機確認）

- 前回「クラシックBTが無効」と推測したのは誤り。純正スタックのHCIログ（`/pio_log/rw/ERASABLE/btsnoop_hci.log`、約25分ごとに切り替え）を見ると、起動時に必ずクラシックBTを初期化し、Write Scan Enable=02（接続可能・発見不可）、クラス 0x340408、名前は本体名にしている。起動直後には登録済みの機器3台へ接続を試みる。
- Waitingが拒否されたのは引数の長さ違い（0x20バイトが正しい）が原因。正しい形式で送ると受理され、Write Scan Enable=03（発見可能）になる。BLE（PICSの受信）は止まらなかった。`SetBluetoothState` は不要。
- 解析用PCで見えなかったのは、PC側のアダプターがLE専用設定（BR/EDR無効）だったため。BR/EDRを有効にすると、問い合わせでNP1が見え、SDPでSerial Port（RFCOMM 10）、iAP2（`00000000-deca-fade-deca-deafdecacaff`、11）、`931c7e8a-540f-4686-b798-e8df0a2ad9f7`（12）が見える。
- 管理側の要求はシーケンスID（関数番号<<16 | 応答番号）で処理され、状態バイト（0=開始済み、1=停止中は StartBT のみ、2=StopBT のみ）で受け付けを決める。実機は0（すべて受付）。

### ペアリング（解析用PCをナビ役にして確認）

- PCからペアリングを始めると、NP1の管理イベント（Evt042、種別 0x040d、+0x30にBDアドレス）で数値比較の確認要求が届く。
- 純正の通信アプリ（Communication）は、自分の登録モード中でなければ約70ms後に拒否を返す（`CCOM_SRV_BT_Register::RecieveConfirmEvent` の `m_RegistType == NONE` → NG）。それより先に `ReplyUserConfirmReq(承認)` を送ると受理され、ペアリングが完了した（PC側で Paired/Bonded、NP1で完了イベント 0x080d）。`aa-bt-probe -listen-only -auto-confirm` で再現できる。
- ただしリンクキーはNP1側に残らない。再起動後の認証では、Communicationが登録済み機器として持つ古いキーが使われて失敗した。リンクキーの保存と起動時の `SetLinkkey`（0x10）による再設定は自前で行う必要がある。完了イベントの+0xc8には登録済みのキーが入っており、新しいキーの取り出し方は未確認。
- 純正の通信アプリ自身にも無線Android Auto（ナビ側）と無線CarPlayの処理（`CCOM_SRV_BT_AAP`、`AAPModeOn` など）があり、登録モードの開始は通信アプリへの要求（AIPI）で行っている。

### 再起動の記録

試験中の 03:15:56（UTC）に本体がメモリー不足になり、OOMでDriveRecが停止して純正の監視が再起動した。BT関連プロセスのメモリーは小さく、ペアリング操作との直接の関係は見つからなかった。ただし当時、本体のtmpfs（RAM）に試験用ファイル（`/tmp/aa-test`、地図タイルのキャッシュを含む）と試験用サービスが残っていた。本体の空きメモリーは起動直後でも約1GBしかないため、試験で本体に置くファイルとプロセスは最小限にし、終わったら消す。

### SPP（RFCOMM）接続

- スタックはUUID指定のSPPクライアント接続を持つ（`ANW_SPPConnect(アドレス, UUID 16バイト, …)`、チャネル指定版 `ANW_SPPConnectEx`）。
- 純正の接続要求（Connect 0x19）は、プロファイル番号で接続先を選ぶ。通信アプリ（Communication）は 0x0a〜0x0e を「VSSPP1〜5」（ベンダー定義SPP）として使い、各枠のUUIDは `SetProfile` で登録している。Android AutoのUUIDは純正のどこにも入っていない。
- `SetProfile` はプロファイル構成全体を置き換えるため、そのまま呼ぶとスマートフォンアプリ連携やAlexaの登録を消すおそれがある。純正の現在値を読み出して同じ内容に1枠を加えるか、`NEventWatcherCom` 内から `ANW_SPPConnect` を直接呼ぶ（DriveRecと同じ実行時注入）かのどちらかが必要。後者はLTE/Wi-Fiも管理するプロセスへの注入になるため、影響確認の手順を用意してから行う。
- 通信アプリには `HasWirelessAAP`・`HasWirelessCarPlay` や「WAAP_SPP」の処理、Pioneerナビの型番が含まれる。純正にも無線Android Auto関連の連携があった可能性があり、使えればUUID登録の手がかりになる。

### SetProfileでAndroid AutoのUUIDを追加し、RFCOMM接続を確認（2026-11-26）

- 受付処理（`NEventWatcherCom` 0x121d00 → 0x121b40）の検査:
  - 設定オブジェクト+0x84（BT状態から決まる）が0のとき、つまりBT停止中だけ受け付ける。起動後は必ず拒否される。純正の通信アプリは起動時、BT開始前に設定している。
  - プロファイル集合（+0x18）は 0x13fbb で絞られる。UUID枠のビット（7〜11=VSSPP1〜5、16）があれば、+0x4a8 の件数が1〜6でなければならない。
  - UUID情報は+0x1cから0xC2バイトずつ。各枠の+0に自分側UUID（SDPで公開）、+0x10に相手側UUID（接続時にSDPで検索）、+0x20に名前（UTF-16、最大0x50文字）。
  - DID（bit13）は、+0x4ac の8バイトと+0x4b4 のu16を使う。+0x4ac の先頭u32が1以下であること、+0x4ca の有無フラグが1であることが必要。
  - 全体は0x4d0バイト。
- 純正の現在値は、NEventWatcherComのメモリーから読み出した（読み取りのみ）。
  - プロファイル集合 0x21bb（基本0x3b、VSSPP1=iAP2、VSSPP2=`931c7e8a…`、DID）。
  - DIDはベンダー0x0150・製品0x0200・版0x0001。
- 手順:
  1. `SetBluetoothState(0)`（StopBT）でBTを停止する。
  2. 純正の値を保ったまま、VSSPP3に自分側=独自UUID・相手側=`4de17a00-52cb-11e6-bdf4-0002a5d5c51b` を加えた集合0x23bbで `SetProfile` を呼ぶ。
  3. `SetBluetoothState(1)` でBTを再開する。
  - 3つとも受理され、内部の枠表にVSSPP3が登録された。既存の枠は変わらなかった。
- `Connect(相手, プロファイル0x0c=VSSPP3)` の結果:
  - NP1がナビ役PCを呼び出し、SDPで`4de17a00…`を検索してRFCOMMで接続した。PC側のBlueZプロファイルに接続が届いた。
  - NP1から始めるペアリングでは、確認要求が管理イベント0x1405で届く（相手から始める場合は0x040d）。これに `ReplyUserConfirmReq` を返すと進む。
- 問題点:
  - 手動でBTを停止・再開すると、純正側が再開後の処理をしない。具体的には、本体名とWrite Scan Enable=02の設定、BLE走査（PICS受信）の開始が行われなかった。NCCのbrokerを再起動しても戻らず、本体の再起動で元に戻した（設定も純正に戻る）。
  - 実運用では、純正の起動手順の中に設定を差し込む（純正のSetProfileより後、StartBTより前）か、BT再開後の純正処理（BLE走査開始など）を正しく起こす方法が必要。
- 次は、SPPハンドラーの `Write(データ, 長さ, プロファイル)` と受信イベントでWPP（無線AAの手順）をやり取りする。

### 手動でBTを停止・再開すると純正の処理が戻らない理由

- BTを止めたのは、`SetProfile` がBT停止中しか受け付けないためで、それ以外に止める理由はない。
- 起動時のHCIログでは、スタック初期化（Write Scan Enable=03、仮の名前 `qcs610-odk-64`）の約0.5秒後に、Write Scan Enable=00 → 本体名の設定 → Write Scan Enable=02 → LEのプライバシー設定・広告（スマートフォンアプリ用UUID入り）が続く。手動再開後のログでは、初期化までは同じで、その後の手順がすべて欠けていた。
- これらは通信アプリ（Communication）が行っている（`Waiting`、`LEStartAdvertising` などを呼ぶ）。BTの開始・停止の完了イベントは要求した側（今回は調査ツール）にだけ届くため、通信アプリはBTが戻ったことを知らず、続きの手順を実行しなかったと考えられる。PICSの走査（NCCのbrokerがBLEハンドラーで行う）も再開できなかった。

### BTを止めずにAndroid AutoのUUIDを登録する方法（未検証）

- 接続要求（Connect、VSSPP）は、接続のたびに純正の枠表（0x60バイト×6、+0x48に有効フラグ、+0x49に自分側UUID、+0x59に相手側UUID）から相手側UUIDを読み、SDP検索に使う（`NEventWatcherCom` 0xdfbb8 → 0x131890）。`SetProfile` を受けた後の変化も、この表の該当枠の33バイトだけだった。
- そこで、BTを止めずに `/proc/<pid>/mem` からこの表の空き枠（VSSPP3）だけを書く。ディスクには何も書かず、再起動で純正に戻る。表は、iAP2の枠（VSSPP1）の内容と、残りの枠が「有効」か「空（0と0xff）」であることで特定する。実装は `backend/internal/androidauto/stockbt/profile.go`（`SetVendorSlot`）。
- 接続側（NP1からナビへ）ではSDPの公開（自分側UUID）は不要。設定オブジェクトのプロファイル集合（+0x88）を変える必要があるかは実機で確認する。

### SPPの送受信（`NPBTSPPHandlerProxy` から）

- 送信は関数0、引数は0x808バイト: `u32 連番, u32 0, u64 0`、+0x10からデータ（最大0x7ed バイト）、+0x800にu32 長さ、+0x804にu32 プロファイル。応答は1バイト。
- 受信イベントは、状態が0x210（`Evt528`、本文0x30バイト: +0x20 u8、+0x24 u32 状態、+0x28 u32）、データが0x211（`Evt529`、ヘッダー0x40バイトの後に0x7edバイトのバッファー。+0x20と+0x30のu32が長さとプロファイル）。どちらが長さかは実機で確認する。
- 純正では音声アプリ（Voice、Alexa用のVSSPP2）がこの送信を使っている。

### リンクキーの取得と再設定

- スタック（`libanwbtif.so`）は新しいリンクキーを `<設定ディレクトリ>/linkkey` に書くが、そのディレクトリは読み取り専用の領域（`/usr/local/etc/config/BT`）とみられ、再起動で失われる。純正のイベントにも新しいキーは入らない（完了イベント+0xc8のキーは、以前から登録されていた古いキー）。
- 純正のHCIログ（`/pio_log/rw/ERASABLE/btsnoop_hci.log`）には、HCI Link Key Notification（イベント0x18、アドレスは逆順）としてキーが残る。ペアリング完了後にここから読み、自前で保存する（`stockbt.FindLinkKey`）。
- 再設定は `SetLinkkey`（関数0x10、0x30バイト: +0x18 BDアドレス、+0x1e キー16バイト）。

### 実機での確認手順（未実施）

`aa-bt-probe` に試験用のモードを追加した。解析用PCでは `tmp/aa-research/hu_wpp.py`（AAのRFCOMMサービスを登録し、接続されたらWifiStartRequestと試験用のWi-Fi情報を送る）を動かす。

1. `aa-bt-probe -slot 2`: BTを止めずに、VSSPP3へAndroid AutoのUUIDを書く。
2. `aa-bt-probe -connect <PCのアドレス> -profile 0x0c -auto-confirm -spp-send 0102 -seconds 40`: 接続と送受信の形式を確かめる。PICSの受信が止まらないことも確認する。
3. `aa-bt-probe -connect <PCのアドレス> -profile 0x0c -auto-confirm -wpp -seconds 60`: WPPの手順（Wi-Fiには接続しない）を通す。
4. `aa-bt-probe -find-linkkey <PCのアドレス>` でキーを読み、再起動後に `-linkkey` で戻して再接続できるか確かめる。

### ナビのWi-Fiへの接続（未実装）

- 本体のwlan0は純正のwpa_supplicantによるSTA、アクセスポイントはwlan1（hostapd）で、両方を同時に使える。NCCにはwlan0の接続を切り替える機能（`adapters.WiFiController`、確認なしで90秒後に戻す）と、LTEとWi-Fiを切り替える回線監視がある。
- ナビのWi-Fiにはインターネットがないため、投影中もLTEを既定の経路のままにし、回線監視がWi-Fiへ切り替えないようにする必要がある。brokerにナビ用の一時接続（保存しない、投影終了で元へ戻す）を追加する方針。

## 次の手順

1. ~~クラシックBTの有効化~~（有効だった）。~~発見可能化とペアリング~~（確認済み）。
2. 新しいリンクキーの取得方法（完了イベントの別の位置、またはHCIのLink Key Notificationに相当する管理イベント）を特定し、保存と `SetLinkkey` での再設定を実装する。Web UIに発見可能化・確認コード表示・承認・登録済みナビ一覧を加える。確認コードは確認要求イベントから取り出す。
3. RFCOMMは、`SetProfile` の現在値の読み出し方法を探すか、注入方式の安全手順（失敗時に元へ戻す、PICS・LTE・Wi-Fiの監視）を決めてから実装する。
4. ナビ役の試験環境: 解析用PC（BlueZ 5.85）でAndroid AutoのRFCOMMサービスを登録し、USB Wi-Fiでアクセスポイントを立てる。内蔵Wi-Fiは外部接続用のため変更しない。USB Wi-Fiは別作業で使われている場合があるので、使う前に状態を確認する。

市販ナビは、スマートフォン側の証明書の検証で本体の自己署名証明書を拒否する可能性が高い（[android-auto.md](android-auto.md)）。Bluetooth経路の作業は、実際に使うナビの機種と証明書の扱いを確認してから優先度を決める。

## 調査ツール

`backend/cmd/aa-bt-probe` は、純正BTのハンドラーへ1回だけ要求を送り、応答とその後のイベントを表示する。純正の状態を変える関数を送る前に、上の表と影響を確認すること。

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o aa-bt-probe ./cmd/aa-bt-probe
# 本体で（例: 状態取得）
./aa-bt-probe -func 0x1f -payload 03000000000000001f0000001e0000000100000000000000 -seconds 3
```
