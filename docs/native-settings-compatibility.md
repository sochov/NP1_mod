# 純正設定の互換性

| 項目 | 区分 | 経路・read-back | 保存/復旧 |
|---|---|---|---|
| STA状態 | READONLY | 純正wpa_supplicant STATUS。safe keyだけ公開 | 設定変更なし |
| AP状態/開始/終了/SSID | EXPERIMENTAL | NWlanSystem / CCOM内部symbol、GATT0501資料 | ABIとイベント、read-back、再起動持続性未確定。操作無効 |
| APN / PDP | EXPERIMENTAL | QCMAPとNPMCom内部symbol | QCMAP占有、SIM条件と永続形式未確定。操作無効 |
| 音量/mute | EXPERIMENTAL | GATT0400資料、PulseAudio存在 | 未実装・read-back未検証 |
| SD状態/閲覧 | READONLY | mountとファイルhandle | 原本は変更しない |
| SD format | DANGEROUS | GATT0601/0001資料 | Web操作に含めない |
| camera export | READONLY | ImageRecog前方/車内NV12 | 再open、表示停止でworker停止。QMMF新規sessionなし |
| 録画モード/駐車監視 | UNSUPPORTED | 保存形式・純正語彙・apply未確定 | 操作無効 |
| GPS/6軸 | EXPERIMENTAL | 純正ライブラリ/sidecar候補 | 位置ロガーOFF、無断保存なし |
| LED | EXPERIMENTAL | sysfs存在、readがEIO | mode/value/status保存・純正復帰未検証。write無効 |
| USB role | READONLY | sysfs peripheralを実測 | 代替管理/自動復帰が成立するまでwrite無効 |
| firmware更新 | DANGEROUS | 情報参照のみを今後実装 | raw block/updateは操作禁止 |
| account/check-in | ACCOUNT_DEPENDENT | 純正クラウドとローカル機能は別 | 偽装・契約制限回避なし |
| maintenance/UART | READONLY | ガイドと既存recoveryを参照 | 今回変更なし |
