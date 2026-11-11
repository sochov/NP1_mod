# TODOと進捗

2026-11-12更新。実機で確かめた範囲と、コードだけの状態を区別する。未確認の設定変更を成功扱いしない。元の開発仕様とユーザー追加指示を継続して実装する。

## 今回の更新

- [x] H.264の前方/車内同時ブラウザー再生、共通表示切替、モバイル配置。
- [x] H.264の録画ローテーションをまたぐ210秒の前方/車内同時HTTP配信・全フレーム復号・純正26サービス維持。
- [x] ブラウザーMSEの210秒同時再生。録画切替前後も同じ2配信を維持、前方5587/車内3332frame、その後の切替・390px配置を確認。
- [x] 消音・音量補正・発進通知・信号通知の本番Web変更・保存・元値復帰。
- [x] NASの種別別ON/OFF、Wi-Fi STA/Tailscaleごとのプロトコル・保存先保持、SSID/リアルタイム設定、進捗・中断/再開のAPI/UI。
- [x] 指定NASへのSMB原本転送と全体照合、実中断と検証済みprefixからの再開。SSID不一致時の自動転送抑止。
- [x] Tailscale先の既存共有へ約19MBの原本保存・全体照合。既存ファイルの削除・上書きなし。
- [x] NASの新しい録画を優先する自動転送・指定SSID一致。約103MBの保存・確定先全体照合・試験後の元設定/OFF復元・純正26サービス維持。
- [ ] NASの実際の緊急録画データ、再起動/経路切断復旧。
- [ ] NASのWeb/broker再起動後の約237MB途中再開。journal復元・検証済みprefixから再開済みだが、旧10分上限で確定先照合中にtimeout。上限30分へ修正・配備済み。ユーザー指示でNAS試験を終了し、確定先の再照合は未実施。
- [x] mainのGitHub push、MIT・第三者ライセンス本文/NOTICE・秘密値/全履歴・隔離source build検査。追加変更でも検査を維持。

## 実機確認済み

- [x] 利用者のQMMF停止後の本体再起動3件を解析。利用側NEventWatcherAVのSIGSEGV→純正corefilterのreboot経路を保存ログで確認。カメラ利用5アプリ・AV/QMMFの停止/起動ガードを追加しWebだけ更新。実機GETで禁止表示と全対象PID/起動IDの不変を確認し、異常再現は追加しない。
- [x] システム画面に純正26サービスの個別起動・停止・再起動を追加しWeb UIを更新。OpeningのWeb停止→起動→再起動と他25サービス維持を確認。16プロセスの専用singleton lockは死亡・無参照を検査して退避する。全26件の機能別往復を試験済みとする意味ではない。
- [x] ホットスポットのWi-Fi STA優先／4G優先を追加。優先設定だけはAPを再起動せず反映。実クライアントを接続した3回のWeb部分PUT、元値復帰、SSID/PSK・AP/DHCP/Webプロセス・DHCP接続維持、標準TLS付きHTTPSとNAT増加を確認。実上流はWi-Fiであり、4G共有の完了とは区別する。
- [x] Tailscaleのアカウント・登録機器名を両画面へ表示し、個別ページで名前変更可能にした。実機で一時名変更/登録名反映/元名復元、所有者/IP維持、本番両画面表示を確認（2026-11-12）。
- [x] 経路検索とコエ2種類のWeb変更・保存・元値復元を追加し、純正設定14項目を確認。コエのcache取得済みとは区別する。
- [x] テザリング対応版の再起動後も標準TLS付き外部HTTPS、DNS、NATカウンター増加、Tailscale root ADB、OTA抑止、走行collectorの自動復帰を確認。

- [x] LED連続操作409修正。起動回数制限が原因で、常駐workerが設定を取り込む方式へ変更。消灯を含む9連続変更・同一PID・元設定復元を実機確認。
- [x] 最新版の通常再起動1回。常時AP/HTTP80/HTTPS443/純正サービス/両カメラJPEG/SD原本Range取得を確認。
- [x] 通常導入版の常時テザリングをWi-Fi上流で実機確認。APクライアントのDHCP/GW/DNS、UDP/TCP名前解決、標準TLS検証付き外部HTTPS200、NATカウンター増加、AP/Web/broker PID維持が合格。
- [x] 正式なSTA OFF→45秒観測→元ON復帰で、実クライアントから4G上流のUDP/TCP DNS・標準CA/hostname検証付きHTTPS200を確認。対応cellular MASQUERADE +11、AP/Web/broker PID・SSID/PSK・元STA設定不変と復帰後15秒安定を確認。以降のWindows Wi-Fi変更は禁止し、追加のクライアント試験は指定解析PCの空きUSB Wi-Fiで行う。
- [x] 純正CMの実行file/argv/UID/GID/cgroup/netns/PID/starttime/所有FDを照合し、同時稼働中の独立NAS固定GET3種類を実機確認。既定workerへの統合を実装・host検証済み、本番配備後に継続取得を確認する。WDSはprofile1読取り・private保存/read-backが合格、profile2の不存在証明は追加対応中。
- [x] 発進通知・信号通知と、消音・音量補正は専用helperと本番Webの実機変更/保存/元値復元を確認。master音量は意味と可変範囲の確認まで現在値のみ。
- [x] 画面連動LEDの実機5RGB命令、12frame連続更新、保存設定不変、期限切れ/明示停止で元設定復帰を確認。物理色の光学読取は行っていない。
- [x] 画面共有音声のPCM APIからPulseAudio primary専用streamへの出力と無更新2秒終了を実機確認。ブラウザの共有音声接続は合成音源で検証。実音声の聴感は別途確認。
- [x] レビュー後の最新版を実機へ再配備。本番UI/APIから合成映像と音声を同時送信し、実機RGB反映・保存設定不変・停止時復帰まで確認。
- [x] 拡張9項目もWeb経由で変更・永続保存確認・元値復元。既存2項目と合わせて11項目の操作経路を確認。

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

- [ ] 【今後・低優先】4GとWi-Fi STAの同時維持・優先切替。利用者指示により現段階の調査/opt-in試作で区切る。選択/NAT/DNS plannerは両ready時に4Gを選ぶが、純正CommunicationはSTA接続時に4G CM/egressを停止する。QMAP GET/mux対応は実機合格、独立WDS Start/Stop・専用IP/routing・AP統合は未実施。[途中調査](owned-cellular-data-call.md)を保存。STA OFF中の4G共有成功を両接続要件の完了としない。
- [ ] 個別起動した純正アプリのPROCESS_START_WAITを正式Notify経路で解除する。開始通知helperを追加しImageRecogのWeb再起動を確認済み。他4対象の個別往復は未確認。singleton退避と別の起動barrierであり、systemd activeだけを起動完了にしない。
- [x] SD原本と同じH.264の前方・車内ライブ映像を再圧縮せず表示。録画切替をまたぐ継続試験は上段で管理。

- [ ] 純正Bluetooth設定をWebで代替。その他設定の適用経路を拡張。
- [x] 運転/ナビ/案内・コエ/プライバシー/車両・取付の画面分割と、証明済みenum表示を反映。
- [x] Alexaウェイクワードの専用画面/APIを統合し、Webからの変更・保存・復元を確認（処理の重複照会削減後に9.8秒で往復合格）。
- [ ] 4G専用画面の全面刷新、APN2profile編集/保存/実適用/接続確認/元設定復旧。
- [x] WDS元profile1はroot0600保存/Loadを確認。固定profile一覧でindex1のみ存在、profile2不在を確認。
- [x] 復旧profile1をAPN編集へ非上書きで取り込み、保存済み設定と現在の通信APNを区別。
- [ ] 純正APN適用をトランザクション化。同値0bでprofile1全39TLV/profile2不在は維持したがWi-Fi default削除を観測。元SSID/IP/PID照合後に欠落default1本のみ復旧し、Tailscaleも復帰済み。実egress確認と条件復旧を内蔵する。
- [x] Wi-Fi/APにSTA scan/認証/接続/保存/rollback、AP SSID/暗号/DHCP、接続端末を統合。
- [x] 独立AP+DHCPの30秒維持、13種の純正/STA/経路の前中後一致、終了後の所有IF撤去。
- [x] STA Web接続確定と、接続不能な新規SSIDからの復旧・所有profile削除を実機確認。
- [x] STA検索中の管理AP維持、90秒自動復旧と手動復旧、STA未接続中AP worker再起動、Windows119回連続HTTP/station確認。元WiFi/AP/7サービス・PC一時設定の復元も確認。
- [x] standalone既定ONのsource配備と通常再起動後のAP自動復帰。
- [x] 常時テザリングのNAT/DNS/転送と使用中の上流表示を配備。Wi-Fi・4G両上流の外部HTTPS・所有NATカウンターを実機確認。長時間/failover/再接続は継続する。
- [ ] 日/週/月の距離・時間棒＋累計線、全期間/custom、日別/走行別の詳細を有効GNSS記録で確認。
- [ ] GNSS取得時のロガーと365日集計の永続性を確認（SD FIFO削除後の集計保持を含む）。
- [x] 現在測位を30秒履歴cacheから分離する軽量LiveIndexを実装・host検証。最新4file/1秒cache、SD removal/交換・未来/過去測位を検証。
- [x] LiveIndexと重複防止・日別/走行別の非root永続storeを配備。再起動後active/storage available/定期pollを確認。現状有効GPSがなく走行件数は0。
- [ ] 配備済みのマップlight/dark/system修正・最新画面を一括回帰試験。
- [x] 本体OTAの初期OFF・managed/writable設定を再起動後も確認。
- [x] Tailscale IFだけに既存ADB5556を許可し、実PCからrootシェルを確認。
- [x] Tailscale経由ADBの再起動後維持。実tailnet IPv4へのroot ADBと、試験後USB HTTP/HTTPS forward復帰を確認。

## 残る検証と拡張

- [x] 試用中の本体起動ID変更は所有者から電源入れ直しと回答があり照合済み。純正26サービス・両camera・原本録画・本番UIの自動復帰を確認。意図的なエージェント再起動試験の追加回数には数えない。
- [x] 既存SD書込み中のH.264を受動取得してライブ配信。追加encoderを起動しない。
- [ ] ライブのマイク/AAC音声。消音・補正・通知制御と共有音声の出力は確認済み。出力monitorをマイクと偽らない。
- [ ] 有効GNSS/実走行、別SIM、NASの切断・再起動復旧。
- [ ] 10回再起動、24h/72h、電源断・SD交換・低容量・network切断の検証。
- [x] 24h/72h用の読み取り専用継続監視toolを作成・fixture検証し開始。実際の連続正常時間だけを加算し、純正停止・本体再起動・取得エラー・長い採取間隔でリセットする。開始済みと24h/72h合格を混同しない。
- [x] 現在の画面49件のbrowser試験（320/390px・テーマ・操作失敗・取得不能を含む）。変更後の古い2件を更新して再試験合格。
- [ ] 追加中のAlexa/AP/NAT画面のレビュー、APIと公開用docsの同期。
- [x] GitHub push前の秘密値/個体情報/全履歴/ライセンス/隔離clean build検証。追加変更でも継続実施。
- [ ] NASの編集可能な階層構造・メタデータ保存、転送帯域/通信量等の追加ポリシー。現状は指定フォルダー直下に原本名とハッシュを含む名前で保存。
- [ ] 端末別通信量。API/UI・64bit/方向/0と未取得の分離は実装・host/browser試験済み。ただし実機driverがbyte属性を提供せずhostapdが代わりに0を返すため、iw属性未提供時は未取得へ補正。独立した端末別集計の実装を継続する（低優先）。
- [ ] OBD2モジュール連携（低優先。接続方式・取得できる車両情報・走行記録との連携）。
