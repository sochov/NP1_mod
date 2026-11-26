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
