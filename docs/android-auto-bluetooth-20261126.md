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
| GetLocalDeviceInfo | 0x05 | なし | 拒否（0） |
| Waiting（発見・接続可能化） | 0x09 | u32 走査モード | 拒否（0） |
| Inquiry（機器探索） | 0x0b | u32 種別, u8 時間, u32 件数 | 拒否（種別0〜2とも） |
| RequestPairing | 0x0d | BDアドレス6バイト | 未実行 |
| ReplyUserConfirmReq | 0x12 | BDアドレス, u8 承認 | 未実行 |
| SDPSearch | 0x15 | BDアドレス, u32, u32 | 未実行 |
| Connect | 0x19 | BDアドレス, u32 件数, プロファイル情報×件数（各24バイト） | 未実行 |
| SetProfile | 0x02 | u32 プロファイル集合, UUID情報×最大6（各0xC2バイト）ほか | 未実行（全体を置き換えるため） |
| GetBtManagerStatus | 0x1f | なし | 成功。Evt042で状態（本体のBT名とBTアドレスを含む）が届く |
| GetConnectableProfile | 0x21 | なし（同期、応答8バイト） | 0（成功フラグ0） |

解析用PCから見ると、NP1はBLE広告（`62cedda5-…7830`）だけが見え、クラシックBTのSDPには応答しなかった。書き込み系の要求が拒否されるのは、クラシックBTが無効（BLEのみ有効）のためと推測している。有効化は `SetBluetoothState` だが、スタックの再初期化を伴いうるため、PICS（BLE受信）やAlexaへの影響を確認できる状況で試す必要がある。

### SPP（RFCOMM）接続

- スタックはUUID指定のSPPクライアント接続を持つ（`ANW_SPPConnect(アドレス, UUID 16バイト, …)`、チャネル指定版 `ANW_SPPConnectEx`）。
- 純正の接続要求（Connect 0x19）は、プロファイル番号で接続先を選ぶ。通信アプリ（Communication）は 0x0a〜0x0e を「VSSPP1〜5」（ベンダー定義SPP）として使い、各枠のUUIDは `SetProfile` で登録している。Android AutoのUUIDは純正のどこにも入っていない。
- `SetProfile` はプロファイル構成全体を置き換えるため、そのまま呼ぶとスマートフォンアプリ連携やAlexaの登録を消すおそれがある。純正の現在値を読み出して同じ内容に1枠を加えるか、`NEventWatcherCom` 内から `ANW_SPPConnect` を直接呼ぶ（DriveRecと同じ実行時注入）かのどちらかが必要。後者はLTE/Wi-Fiも管理するプロセスへの注入になるため、影響確認の手順を用意してから行う。
- 通信アプリには `HasWirelessAAP`・`HasWirelessCarPlay` や「WAAP_SPP」の処理、Pioneerナビの型番が含まれる。純正にも無線Android Auto関連の連携があった可能性があり、使えればUUID登録の手がかりになる。

## 次の手順

1. クラシックBTの有効状態と走査モードを `GetBtManagerStatus` の応答で特定し、`SetBluetoothState` の影響（PICS受信、Alexa、再接続）を実機で確認しながら有効化を試す。
2. 有効化できたら、Waiting（0x09）でNP1をナビ（試験では解析用PCのBlueZ）から見つけられるようにし、ペアリング要求のイベントと ReplyUserConfirmReq（0x12）で承認する。Web UIに発見可能化・相手の確認コード表示・承認・登録済みナビ一覧を加える。
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
