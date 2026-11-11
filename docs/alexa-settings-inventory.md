# Alexa連携とウェイクワード設定

2026-11-11に公式説明と所有実機のVoice/Communication/Setting ELF・埋込protobufを静的に照合し、その後ウェイクワード設定は純正Voice経由のcontrolled実機試験とWeb往復に合格した。実装・試験範囲は [native-alexa-helper.md](native-alexa-helper.md) に記録する。本書のそれ以外の通信・連携説明は静的照合であり、Amazonログインや実際の呼びかけ認識の合格を意味しない。アカウントtokenやdevice認証情報は取得・掲載しない。

## ユーザーが設定する対象

Pioneer公式の[Alexaを使うには](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/F902E15245A7DEA85AF57289DB7849BF.php)は、スマートフォンのAlexaアプリでログインしNP1をBluetoothのAutoアクセサリーとして追加する流れを指定している。My NP1のAmazon Alexa画面にウェイクワード認識ON/OFFがある。Alexaと連携中、NP1が未接続の場合のOFF制約も記載される。音声出力はスマートフォンまたは車載Bluetoothオーディオ側である（[機能説明](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/65CDAEC23D439117B43BFEFB1BBBBA58.php)、[操作方法](https://jpn.pioneer/ja/support/manual/np/np-001/usersguide/E474F4F4EC1084D26D7C19004B38DCD5.php)）。

したがってNCCの対象は、純正handler経由のウェイクワード認識設定と連携状態表示。Amazonログイン、OAuth代行、NP1単体でスマートフォンを置き換えるAVS実装ではない。アカウントを勝手に解除せず、連携が必要な場合は公式Alexaアプリの手順を案内する。

## 純正wire契約

Voice ELFに埋込`AlexaAMAFunc.proto`を確認。descriptor file offset `0x234e20`、944 bytes。`tools/inventory_np1_protocol.py resources/native-labels/Voice --out tmp/alexa-protocol-inventory.json`で再抽出可能。descriptor定義は秘密のruntime値を含まない。

外側CommonFreeStyle functionは **0x0b00**。Voice `0x109a3c〜0x109a44`、`0x109bd0〜0x109bd8`、`0x109afc〜0x109b08`の引数で確認。

| command | message | payload |
|---|---|---|
| 1 | AlexaAMASupportRequest | empty |
| 2 | AlexaAMASupportResponse | field1 bool result |
| 3 | AlexaAMAFuncOnOffChangeRequest | field1 enum alexaFuncOnOffType |
| 0x8003 | AlexaAMAFuncOnOffChangeResponse | field1 bool result、field2 enum状態 |
| 4 | AlexaAMAFuncOnOffGetRequest | empty |
| 0x8004 | AlexaAMAFuncOnOffGetResponse | field1 enum状態 |
| 0x8005 | AlexaAMAFuncOnOffNotify | field1 enum状態 |

enumは0=ON、1=OFF、2=ON_FIXED。ON_FIXEDは読み取り結果であり、ユーザーから設定値2を送らない。proto3のdefault ON=0は空payloadでも表現される。helperはcorrelationが一致した応答だけをdecodeする必要があり、timeout/未応答をempty payloadへ置換してはならない。

Voice入力handler `0x109188` はrequest command3/4を比較し、protobufをparseして `CVA_SEV_VAAlexaFuncOnOffEvent` へ配送する。command3は`0x109320〜0x1093a0`で値を内部0/1/2に変換してCHANGE_REQUEST(type0)、command4は`0x109284〜0x109298`でGET_REQUEST(type2)を生成。返信handler `0x109950` は内部type1/3/4に応じchange/get/notifyへ戻す。

## 保存IDと連携時の保護

Voice `0x17ddfc`と`0x17de24`はSetting ID **85 (0x55)** をGetStatusし、`0x17de38〜0x17de3c`は保存値0をenabled=trueへ変換する。setter `0x17e078〜0x17e084`はenabledをXOR1して同IDへSetStatusする。保存enumも0=ON、1=OFF。

ただし**ID85の直接writeでは純正の連携時ガードを迂回する**。実際の純正setter `0x17dfc0` は連携flag `[this+8]` を検査し、trueなら`0x17e044→0x17e100`で変更拒否(result=false)。get `0x17de78`は連携flagがtrueならON_FIXED=2、それ以外は保存設定に応じON/OFFを返す。連携更新handler `0x17e180`はtrueへの更新時にID85=0を強制する（`0x17e254〜0x17e260`）。

連携flagはVoiceのNP1AlexaLinkChangeEvent経路で更新される。`0x104d90`がboolをイベントへ格納し、`0x17e358`は該当イベントを受けて`0x17e180`を呼び、その後ON/OFF通知を送る。ON_FIXEDはこの純正内部連携flagのread-backであり、Amazonアカウントのユーザー名やトークンの検証結果と同一視しない。前回接続状態と現在のスマートフォン接続も別概念である。

## NCC helper/APIの境界

実装済みのbounded owner IPC helperからstock CommonFreeStyle function0x0b00/command4へgetを送り、期限内のcorrelated command0x8004を返す。変更は同function/command3で0または1のみ。change result=trueだけで終了せず、再getで期待ON/OFFを確認する。result=false/ON_FIXEDなら「Alexa連携中のため変更できません」と表示し、ガードを迂回しない。

`backend/internal/adapters/alexa_protocol.go` は純粋request encoder/response parserのみを実装する。最大32bytes、対象command・varint境界・重複field・enum域を検証し、unknownをON扱いしない。実機への配送、stock AIPI constructorやchannel、応答correlation/timeoutはこのファイルに含まれない。未接続/未検証の場合はAPI unavailableのままとし、直接SetStatus85やNCC独自flag保存で成功を表示しない。

公開GETはObservationのdataに `{wakeWord:{value,enabled,fixed}, linkage:{status,source}, writable,reason}` を返す。linkageは確認できた純正guardの範囲だけを返す。account email、MAC、device ID、認証情報を返さない。変更は `{expectedValue:0|1|2, enabled:boolean}` とし、root側で並行操作を排他し、事前getと異なる場合は競合で拒否する。

schemaのcommand1/2はAMA対応の能力照会であり、Amazonアカウントの連携・解除やBluetooth再接続を設定するwriteではない。command3以外に設定を変更する命令を、このdescriptorからは確認していない。未確認のlink flagやcredentialを書き換えて「Alexa利用可能」と表示しない。

## 資料と検証

- `resources/native-labels/Voice`、`resources/alexa/libAlexaMobileAccessoryService.so` はprivate/ignored。後者は10KiB shimであり、名前だけで単体実行可能なserviceと判断しない。
- `tmp/alexa/` は静的文字列、symbol、逆アセンブル調査記録。runtime account値を調査していない。
- `go test ./internal/adapters -run Alexa -v` でwire enum、ON_FIXED、change拒否、malformed/unknownを検証。これは実機適用試験ではない。
