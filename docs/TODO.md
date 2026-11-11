# TODOと進捗

2026-11-11。実機で確かめた範囲と、コードだけの状態を区別する。未確認の設定変更を成功扱いしない。元の開発仕様とユーザー追加指示を継続して実装する。

## 実機確認済み

- [x] Git作成、private資料/credential/生ログ除外、まとまり単位のコミット。
- [x] HTTP80/HTTPS443、Webパスワード不要、管理IF限定。
- [x] Tailscale登録・遠隔Web・再起動1回後の自動復帰。
- [x] 原本H.264/AAC再生、日付/時間軸、任意秒seek、2カメラ/音声、重複境界の連続再生。
- [x] front/in JPEG、表示直接開始・停止、video-range色変換。
- [x] 通信量の4G/Wi-Fi分離、2秒差分速度、上限設定UI。
- [x] 独立NAS clientで回線登録/PLMN/バンド/信号値、専用workerから継続取得。
- [x] 純正設定18項目のread-only取得、純正vendor getter、所有lock/map/refcount復帰、stock維持。
- [x] 独立AP20秒ENABLED、純正PID/STA/route維持・所有物だけ撤去。
- [x] 純正の後続車検知感度を変更→保存file確認→元値復元。共有map参照数/sem/純正7サービスを維持。
- [x] 検知感度・検知ON/OFFともWeb API変更・永続保存・復元を確認。
- [x] 最新UI/走行複合グラフ/走行詳細/テーマ/4G専用/Wi-Fi統合画面を実機配備。
- [x] 常時ロガーの実測電波蓄積。GPS未測位では位置を作らない。
- [x] 純正OTAの新規取得抑止を初期適用。設定画面から有効化可能（非推奨）。
- [x] 常駐管理APへWindows実端末がWPA2接続→DHCP取得→HTTP UI表示、associated端末としての検出を確認。
- [x] Alexa純正Voiceのget、ON/OFF変更・保存・元値復元。受信専用queue撤去と純正8サービス維持。

## 優先して継続

- [ ] 純正Bluetooth設定をWebで代替。その他設定の適用経路を拡張。
- [ ] 運転/ナビ/案内・コエ/プライバシー/車両・取付の画面分割と、証明済みenum表示を反映。
- [ ] Alexaウェイクワードの専用画面/APIを統合し、Webからの変更・保存・復元を確認。
- [ ] 4G専用画面の全面刷新、APN2profile編集/保存/実適用/接続確認/元設定復旧。
- [x] WDS元profile1はroot0600保存/Loadを確認。固定profile一覧でindex1のみ存在、profile2不在を確認。
- [ ] 復旧profile1をAPN編集へ取り込み、純正の適用経路を実装。profile2の新規作成と既存復元を区別。
- [ ] Wi-Fi/APにSTA scan/認証/接続/保存/rollback、AP SSID/暗号/DHCP、接続端末を統合。
- [x] 独立AP+DHCPの30秒維持、13種の純正/STA/経路の前中後一致、終了後の所有IF撤去。
- [x] STA Web接続確定と、接続不能な新規SSIDからの復旧・所有profile削除を実機確認。
- [ ] 常時APのSTAなしfallback・再起動・NAT/転送を段階確認。unit sandboxの返信socket/own DHCP fd照合を修正して実client疎通済み。4G PDP IFは現在未成立、STAへNATして代用しない。
- [ ] 日/週/月の距離・時間棒＋累計線、全期間/custom、日別/走行別の詳細を有効GNSS記録で確認。
- [ ] GNSS取得時のロガーと365日集計の永続性を確認（SD FIFO削除後の集計保持を含む）。
- [ ] 配備済みのマップlight/dark/system修正・最新画面を一括回帰試験。
- [ ] 本体OTAの初期OFF・設定の取得を再起動後も確認。
- [x] Tailscale IFだけに既存ADB5556を許可し、実PCからrootシェルを確認。
- [ ] Tailscale経由ADBの再起動後維持。

## 残る検証と拡張

- [ ] H.264ライブ共有。追加encoder実験は失敗後停止中で、原因未確認のまま再実行しない。
- [ ] マイク/音声制御。出力monitorをマイクと偽らない。
- [ ] 有効GNSS/実走行、別SIM、NAS転送と復旧。
- [ ] 10回再起動、24h/72h、電源断・SD交換・低容量・network切断の検証。
- [x] 現在の画面49件のbrowser試験（320/390px・テーマ・操作失敗・取得不能を含む）。変更後の古い2件を更新して再試験合格。
- [ ] 追加中のAlexa/AP/NAT画面のレビュー、APIと公開用docsの同期。
- [ ] GitHub公開前の秘密値/個体情報/ライセンス/clean build検証。
- [ ] 端末別通信量（低優先。実測station counterを使用）。
- [ ] OBD2モジュール連携（低優先。接続方式・取得できる車両情報・走行記録との連携）。
