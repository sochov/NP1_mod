# NAS登録・回線・バンドの追加取得候補

2026-11-11時点のread-only調査。追加QMI照会やデバイスopenは実施していない。既に確認したNAS専用CID/QMUX省略ABIを使えるが、個々の応答対応は実機未検証。初期候補は登録照会0x0024と無線バンド照会0x0031。どちらも入力TLVなしのNAS Get要求であり、設定・探索・indication登録を必要としない。[公式libqmi NAS定義](https://github.com/linux-mobile-broadband/libqmi/blob/main/data/qmi-service-nas.json)

## 登録・PLMN

Get Serving System（0x0024）応答:

| TLV | 内容・検証 |
| --- | --- |
| 0x01 | 登録、CS接続、PS接続、選択網の各1byteに続き、RAT件数1byteとsigned 1byte配列。最低5byte、件数ぶんの長さを厳密に確認 |
| 0x12 | MCC/MNC各little-endian uint16、名称長1byte、名称bytes。範囲・文字列長・制御文字を検証 |
| 0x27 | MCC/MNC各uint16とMNC第3桁の有無1byte。0x12と同じ網かを確認して桁数へ使用 |

登録値1だけが登録済み。0/2/3は未登録・探索中・拒否、4または未知値はunknown。PS接続と登録を別に扱い、登録だけでインターネット疎通成功にしない。RAT値8がLTE。これらの値は[公式NAS enum](https://github.com/linux-mobile-broadband/libqmi/blob/main/src/libqmi-glib/qmi-enums-nas.h)で確認した。

MNCの先頭0を落とさない。0x27がない場合は数値だけから2/3桁を推定せず、MCC/MNC数値と桁数不明を保持する。名称は末尾TLV文字列全体とは異なり、sequence内の可変文字列なので既定の1byte長を持つ。[公式文字列generator](https://github.com/linux-mobile-broadband/libqmi/blob/main/build-aux/qmi-codegen/VariableString.py)

同じ0x0024のindicationでは一部TLV番号が応答と異なるため、response flagとtransactionを必ず照合する。名称を事業者の固定データベースで補完する場合は、別の推定情報として扱う。

## バンド

Get RF Band Information（0x0031）の通常リストはTLV0x01、件数1byteの後に各要素5byte（RAT、band enum、channel）。拡張TLV0x11はchannelがuint32のため各7byte。両方ある場合は拡張を優先し、同一RATの重複を合算しない。[公式NAS定義](https://github.com/linux-mobile-broadband/libqmi/blob/main/data/qmi-service-nas.json)

band enumはLTE band番号そのものではない。例はB1=120、B3=122、B18=143、B19=144、B28=158。飛び番があるため単純な引算では変換できず、確認済みの明示mappingで扱う。未知enumはunknownとし、生enumは調査用情報として保持する。[公式band enum](https://github.com/linux-mobile-broadband/libqmi/blob/main/src/libqmi-glib/qmi-enums-nas.h)

複数のRATや搬送波が返る場合は一覧で保持し、根拠なく最初の要素を主セルと断定しない。LTEのchannelをEARFCNとして扱う場合もRATと未知値を検証する。

## セル情報

Get Cell Location Info（0x0043）は接続セルと周辺測定の候補。LTE内周波情報はTLV0x13で、PLMN 3byte、TAC、global cell、EARFCN、serving ID、各閾値、件数と可変長セル測定配列を含む。近隣配列の長さ検証と、接続セルIDに一致するセルの選択が必要。配列の先頭を接続セルと仮定しない。PLMNはこの場合BCD形式で、0x0024のuint16形式と共用parserにしない。[公式NAS定義](https://github.com/linux-mobile-broadband/libqmi/blob/main/data/qmi-service-nas.json)

この応答には互換性注記・v2項目があり、構造が単純なバンド照会を先に検証する方針が適切。セル関連値は位置の手掛かりになるため、実データは公開ログ・Gitへ含めない。QMIエラーや省略TLVは欠測とし、別Get要求に失敗しても検証済みの信号値を成功扱いの登録状態で埋めない。

## 追加実装時の境界

固定parserと照会処理は `modem.ReadGobiNetStatus(ctx)` に実装した。既存 `ReadGobiNetSignal(ctx)` は検証済み信号Getだけを実行し、追加Getを暗黙に開始しない。新関数は別の単発mode/scriptから明示的に試験するためのopt-in入口である。登録/PS接続はpointer boolで欠測を保持し、PLMNは桁数が確定した場合だけ文字列化する。bandはLTE enum明示変換のみ、3G/未知値をLTE bandへ変換しない。複数の異なるbandは一覧として保持し、単一bandと断定しない。

3照会はtransaction 1/2/3に固定し、個別成否を `queries.signal/servingSystem/rfBand` へ出す。任意の照会失敗で他の実測値を消去せず、失敗項目を作り値で補完しない。各応答待ち最大2秒、全体context最大8秒。kernel内待機への外側helper制限は引き続き必要。

Windows上Go単体テスト・vetとCGO0 Linux/ARM64 compileを合格。信号経路が追加Getを送らないこと、固定transaction、成否分離、MNC桁数、登録unknown、可変長境界、sparse band mapping、3G除外を検証した。追加Getの実機実行は未実施であり、配備前の単発試験はroot担当が判断する。

NAS3、固定Get IDの明示allowlist、各transaction別照合、Operation Result・TLV bounds・重複拒否、既存4096byte制限、外側helper時間制限、通常closeによる自分のCID解放を維持する。新規Get候補を一般的な任意QMI送信APIへ拡張しない。実行前後に純正通信・録画等のPIDと実機能を確認する。
