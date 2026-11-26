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
| Waiting（発見・接続可能化） | 0x09 | u32 走査モード, 4バイト詰め | 成功。2=発見+接続可能（03）、1=01、0=純正と同じ接続のみ（02） |
| Inquiry（機器探索） | 0x0b | u32 種別, u8 時間, u32 件数 | 拒否（種別0〜2とも） |
| RequestPairing | 0x0d | BDアドレス6バイト | 未実行 |
| ReplyUserConfirmReq | 0x12 | BDアドレス（+0x18）, u8 承認（+0x1e）, 計0x20バイト | 成功（ペアリング完了を確認） |
| SDPSearch | 0x15 | BDアドレス, u32, u32 | 未実行 |
| Connect | 0x19 | BDアドレス, u32 件数(+0x20), プロファイル情報×件数（各0x18バイト、+0x24から）, 全体0x2c8+0x18×件数 | 成功（VSSPP3でナビ役PCへRFCOMM接続） |
| Disconnect | 0x1b | Connectと同じ | 成功 |
| SetProfile | 0x02 | u32 プロファイル集合, UUID情報×最大6（各0xC2バイト）ほか | BT停止中のみ受理（下記）。使わない |
| SetLinkkey / ClearLinkkey | 0x10 / 0x11 | BDアドレス(+0x18), キー16バイト(+0x1e) / BDアドレス | 成功 |
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

### BTを止めずにAndroid AutoのUUIDを登録する（2026-11-26 実機確認）

- 接続要求（Connect、VSSPP）は、接続のたびに純正の枠表から相手側UUIDを読み、SDP検索に使う（`NEventWatcherCom` 0xdfbb8 → 0x131890）。`SetProfile` を受けた後の変化も、この表の該当枠の33バイトだけだった。
- 枠表は0x60バイト×6で、0が通常のSerial Port、1〜5がVSSPP1〜5（純正は1=iAP2、2=Alexa `931c7e8a…`）。各枠の+0x48に有効フラグ、+0x49に自分側UUID、+0x59に相手側UUID。
- BTを止めずに `/proc/<pid>/mem` から空き枠3（VSSPP3）だけを書く（`stockbt.SetVendorSlot`）。ディスクには何も書かず、再起動で純正に戻る。表はiAP2の枠と、他の枠が「有効」か「空（0と0xff）」であることで特定する。
- 実機で、書き込み後すぐに `Connect(ナビ役PC, 0x0c)` が通り、PC側のRFCOMMサービスに接続が届いた。プロファイル集合（設定オブジェクト+0x88）を変える必要はなかった。BLE（PICS）の受信は止まらなかった。

### 接続・SPPの送受信（実機確認）

- 接続の進み具合は管理イベント0x2081または0x2085で届く（+0x30 BDアドレス、+0x54 プロファイル、+0x58 状態: 7=接続中、0=接続、3=切断、4=失敗）。前の接続が残っているとConnectは拒否される（応答00）。Disconnect（0x1b、Connectと同じ形式）で片付ける。
- 送信は `NPBTSPPHandler` の関数0、引数0x808バイト: `u32 連番, u32 0, u64 0`、+0x10からデータ（最大0x7ed バイト）、+0x800にu32 長さ、+0x804にu32 枠番号（VSSPP3は3）。応答は1バイト。
- 受信は `Evt045`（本文+8が0x2d）。本文+0が種類（0x211=データ、0x210=状態）。データは+0x2cに長さ、+0x3cに枠番号、+0x4cからデータ。送信のたびに状態イベント（+0x2c=1、+0x30=1、+0x34=枠番号）が届く。
- 解析用PCをナビ役にして、WPPの手順（WifiStartRequest → WifiInfoRequest → WifiInfoResponse → WifiStartResponse → WifiConnectionStatus）が最後まで通った（`aa-bt-probe -wpp`、Wi-Fiには接続しない）。

### ペアリングの確認要求と発見可能化（実機確認）

- 確認要求の種類は、相手から始めたペアリングが0x040d、NP1が接続するときの認証が0x0405（スタック再開直後は0x1405）。完了は0x080d、認証失敗は0x0805。どれも約70ms後に通信アプリが拒否を返すので、受け付けるときはすぐに `ReplyUserConfirmReq` を返す。
- `Waiting` の走査モード: 2で発見・接続可能（Write Scan Enable=03）、1で01、0で純正と同じ接続可能・発見不可（02）。`CancelWaiting` は00（接続も不可）になるので使わない。ペアリング受付の終了は `Waiting(0)`。

### リンクキーの取得と再設定（実機確認）

- スタック（`libanwbtif.so`）は新しいリンクキーを `<設定ディレクトリ>/linkkey` に書くが、そのディレクトリは読み取り専用の領域（`/usr/local/etc/config/BT`）とみられ、再起動で失われる。純正のイベントにも新しいキーは入らない（完了イベント+0xc8のキーは、通信アプリが以前から登録していた古いキー）。
- 純正のHCIログ（`/pio_log/rw/ERASABLE/btsnoop_hci.log`）にはHCI Link Key Notification（イベント0x18、アドレスは逆順）としてキーが残る。ペアリング後にここから読んだキーは、ナビ役PCが保存したキーと一致した（`stockbt.FindLinkKey`）。
- `ClearLinkkey`（0x11）でスタックのキーを消すと、Link Key Requestに「キーなし」と答えてペアリングに進む。その状態で `SetLinkkey`（0x10、0x30バイト: +0x18 BDアドレス、+0x1e キー16バイト）で保存済みのキーを渡すと、そのキーで認証し、確認なしで再接続できた。
- BlueZは、ペアリング済みの相手から確認なしのペアリング（Just Works）を求められると拒否する。PC側の挙動であり、NP1側の問題ではない。

### 投影サービスへの組み込み

- 設定 `bluetooth`（「登録済みのナビへBluetoothで接続する」）を有効にすると、投影サービス（`internal/androidauto/bluetooth.go`）が純正BTに接続し、枠3へUUIDを書き、登録済みのナビのリンクキーを渡す。純正プロセスが再起動したら同じ準備をやり直す。
- 「ナビを登録」（`POST /api/v1/android-auto/bluetooth/pairing`）で2分間だけ発見可能にし、ナビからのペアリングを承認する。完了したらHCIログからキーを読んで `bluetooth.json`（データディレクトリ、0600）に保存し、発見不可に戻す。
- 登録済みのナビへ20秒ごとに接続を試み、接続できたらWPPでナビのWi-Fi情報を受け取る。ナビのWi-Fiへの接続（`Config.JoinWiFi`）はまだないため、ナビへは接続失敗を返して2分後に再試行する。接続中にナビが再ペアリングを求めた場合は承認し、新しいキーを保存し直す。
- 実機で、ベンチ起動した投影サービスにより、登録 → キー保存 → 自動接続 → WPP（Wi-Fi情報の受信）を確認した。スタックのキーを消してからサービスを起動し直しても、保存したキーで確認なしに再接続した。受付中だけWrite Scan Enable=03になり、登録完了で02に戻った。
- systemdのユニットに `CAP_SYS_PTRACE` を加えた（純正プロセスのメモリーへの書き込みに必要）。

### ナビのWi-Fiへの接続（未実装）

- 本体のwlan0は純正のwpa_supplicantによるSTA、アクセスポイントはwlan1（hostapd）で、両方を同時に使える。NCCにはwlan0の接続を切り替える機能（`adapters.WiFiController`、確認なしで90秒後に戻す）と、LTEとWi-Fiを切り替える回線監視がある。
- ナビのWi-Fiにはインターネットがないため、投影中もLTEを既定の経路のままにし、回線監視がWi-Fiへ切り替えないようにする必要がある。brokerにナビ用の一時接続（保存しない、投影終了で元へ戻す）を追加し、投影サービスの `JoinWiFi` から使う方針。

## 次の手順

1. ナビのWi-Fiへの一時接続（broker側）と、TCPでの投影開始。
2. 実際の市販ナビ（中華製を想定）での確認: ナビ側からのペアリング、再ペアリングの要否、証明書の検証。
3. 確認コードの表示（確認要求イベント内の数値の位置は未特定。現在は受付中に自動承認）。

## 調査ツール

`backend/cmd/aa-bt-probe` は、純正BTのハンドラーへ要求を送り、応答とその後のイベントを表示する。純正の状態を変える関数を送る前に、上の表と影響を確認すること。

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o aa-bt-probe ./cmd/aa-bt-probe
# 本体で（例: 状態取得）
./aa-bt-probe -func 0x1f -payload 03000000000000001f0000001e0000000100000000000000 -seconds 3
./aa-bt-probe -slot 3                                  # 枠3へAAのUUID（メモリーのみ）
./aa-bt-probe -connect <ナビのアドレス> -profile 0x0c -auto-confirm -wpp -seconds 60
./aa-bt-probe -find-linkkey <ナビのアドレス>
```

解析用PCをナビ役にするときは `tmp/aa-research/hu_wpp.py` を使う（BR/EDRを有効にし、終わったらLE専用の設定に戻す）。
