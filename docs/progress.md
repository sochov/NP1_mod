# 開発状況

2026-11-11 開始。仕様 v1.2 に従って Sprint 0 → P0 → 読み取り機能の順に実装する。

## 最新の確認状況

2026-11-12、サービス試用でQMMF停止後に本体が再起動するとの報告を受け、保存core/journalを読取り解析した。3件とも約8秒後のNEventWatcherAV内SIGSEGVと純正corefilterのreboot経路が一致した。エージェントからこの再起動や再現停止を実行していない。カメラ利用5アプリ/AV/QMMFの実行時依存をGET可否とPOST直前再取得で保護し、親停止と子起動を条件付きにした。純正unit・corefilterの異常復旧は変更しない。Web/brokerだけを更新してAP/modem/native workerのidentityを維持し、実機GETでガード・理由と対象PID/起動ID不変を確認した。ID/actionの永続監査を操作前に残すようにし、Goのコマンド未実行テストとUI13件も合格した。

2026-11-12追加：純正26サービスの個別操作をシステムの本体画面へ追加し、通常配備でWeb UIを再起動した。OpeningのWeb停止・起動・再起動と他25サービスの状態/PID/Invocation維持を確認。純正の相互Requiresへの伝播を防ぎ、専用のnon-robust singleton lockは死亡・無参照・固定形式を検査して同FSのprivate領域へ退避する。16プロセスの固定名を静的確認し、unit・純正バイナリやmutex内容を書き換えない。最初のWeb起動試験ではbackup directoryの書込み権限不足を検出し、brokerのapluser所属で修正して再試験した。

ホットスポットにはWi-Fi STA/4Gの優先設定を追加した。初回fieldで正常稼働中のloopにconfig reloadがない問題を検出し、active tickへreloadとverifyを集約して修正した。AP専用更新手順はWeb/brokerを再起動しない。修正後は実APクライアントを保持したwifi→cellular→originalの3部分PUT、元値復帰、AP/hostapd/DHCP/Webのidentity、SSID/PSK、正のDHCP leaseの維持が合格。WindowsのAP IF/送信元を固定した標準TLS HTTPS200とNAT packet +30も合格した。現在の実上流はWi-Fiであり、4G接続時の共有試験は継続項目。

最終の画面回帰はsynthetic等121件合格・opt-in9件skip、実機camera出力の停止による1件の失敗を検出した。その後、エージェントから本体rebootを要求していない期間にbootID変更を観測した。所有者から電源を入れ直したとの回答を受け、操作と照合した。純正サービス・両export・両録画の継続書込みが自動復帰した。復帰後の実機hardware/API smoke（両カメラJPEG・SD原本Range・純正26active）とbrowser3件（一般画面/サービス・優先設定/Tailscale）は合格した。前bootのjournalが残らないためcamera停止の詳細原因まで断定せず、意図的なエージェント再起動試験の追加回数には数えない。

2026-11-12追記：Tailscaleの所有者アカウント・登録名をdashboardと専用画面へ追加し、専用画面の機器名変更を実機へ配備した。通常は機器名だけの簡潔なフォームとし、別途固定された登録名がある場合だけホスト名と区別する。Web一時名往復で登録名まで反映、元名復元、所有者/IP維持を確認。本番画面のGET限定browser試験も合格した。経路検索/コエ2種類を含む純正14設定のWeb変更・保存・復元、通常APのWi-Fi上流共有と再起動後の標準TLS HTTPSも確認済み。

- LEDの連続変更409はsystemdの起動回数制限が原因。常駐workerが設定を読み替える方式へ変更し、実機9回連続変更・同一PID・元設定復元が合格した。
- 画面連動はブラウザ内で横長5領域を面積平均し、彩度/明るさ/コントラスト/反転/平滑を調整。モバイル操作と共有音声を含む関連19browser/algorithm試験が合格。実機でも5RGBのworker反映、12frame更新、1500ms無更新と明示停止で保存設定へ復帰を確認した。
- 任意の共有音声を24kHz/mono/PCM16LEとして送信し、NP1のPulseAudio primaryに専用streamを作成。実機の短い合成音源・専用stream観測・無更新2秒終了を確認した。物理的な音の聴感や各OSの音声共有対象は未確認。
- 純正設定は既存2項目に追加9項目を統合。11項目のWeb変更・純正の永続保存read-back・元値復元を確認。Alexaも独立したWeb変更・保存・復元を確認済み。
- 最新版の通常再起動で、常時AP、HTTP80/HTTPS443、両カメラJPEG、SD原本Range、純正サービス、自動OTA取得抑止を確認。前方raw exportの停止は再起動後に解消した。
- 現在GPSと履歴cacheを分離し、測定区間を非root永続storeへ保持するsourceを配備。再起動後もactive/storage available/定期pollを確認した。有効GNSSがないため実走行集計は0件であり、実走行や365日の成功とはしていない。
- APはLAN管理専用から、常時テザリングへ変更中。実4G経路を優先し、接続済みSTAを代替利用する。使用中の上流を表示し、NAT/DNS/実クライアント疎通を検証する。
- APNは純正owner経由の同値profile試験と経路復旧を継続。Webは長時間処理を非同期jobで追跡し、revisionだけを専用workerへ渡す設計。反映前にprivate保存・元設定証跡を保持する。

## 運用ルール

- 実機変更は必ずリポジトリ内の再実行可能な sh スクリプトから実施する。
- 実行前に対象・影響・バックアップ・復旧・成功判定を記録する。
- docs_private、資格情報、個体識別子、位置・映像・実機生ログはコミットしない。
- 純正サービスの実行物、rootfs、raw block、SIM契約情報は置換しない。所有者が要求した設定は、検証済みの純正APIまたは限定flag経由で変更する。
- 未検証機能は有効化しない。実測と仮説を分ける。

## Sprint 0

- ローカル Git main を作成。docs_private の ignore を検証済み。
- root ADB、AArch64 / kernel 4.14.206、overlay noexec、SD vfat を確認。
- 純正 Communication / DriveRec / ImageRecog / Voice / QMMF は active、failed unit なし。
- STA 接続中、AP bridge は DOWN。AP+STA の同時動作は未検証。
- カメラ export は存在するがファイルサイズは 3,112,960 byte。先頭 3,110,400 byte の扱いを実測検証する。
- /dev/net/tun、qmicli、QCMAP CLI は存在。Tailscale は未導入。

## 完了条件

仕様の最終受け入れ表は docs/test-matrix.md で個別管理する。
24h/72h、移動中測位、別SIM疎通、Tailscale認証、NAS接続は外部条件が必要。実施していない試験を成功としない。

## 管理基盤・初回実機検証

- Go静的AArch64 + Vue/TypeScript。非root workerとpeer UID照合付きroot brokerを分離。
- ユーザー追加指定を優先しHTTP 80 / HTTPS 443、Webパスワード不要へ変更。管理IF限定bindと専用INPUT chainを実装。
- Tailscale公式arm64を導入し、所有者の登録後Running/Onlineを確認。実PCからtailnet経由のHTTP 200・ブラウザー操作・直接経路pingを実測。
- proc/sysfs、純正7 unit、STA STATUS、SD一覧/MP4 Range、front/in JPEG、表示設定/backup、SSE、監査を実装・実機検証。
- 白/グレーと中立ダーク配色、大きく太字のメニュー。左上ブランドとOwnerアイコンを削除。カメラの許可modalを廃止。
- NV12は純正H.264の色と一致。video-range変換とプレビュー限定の色補正を追加。純正録画にはH.264 1080p + AAC 48kHz stereoを実測。
- build/test/vet、npm audit、postbootテスト、1440/390px E2Eとカメラ直接開始をPASS。
- 配備のCRLF/legacy PTY、/etc rename ESTALE、純正INPUT DROPに対処。変更はすべてスクリプト化し、失敗時の自動撤去も確認。

## 継続中

- 提供されたLED gaming-modeとHR01 radio-mapを未改変でresources/referencesへ保管し、実装への取り込みを進める。
- 純正アプリは利用不可との回答。APのnative制御経路を継続調査。
- QCMAP_CLIはSIGTERMでDisableMobileAPを呼ぶことを逆アセンブルで確認したため実行しない。
- H.264ライブ共有・PulseAudio共存、GNSS/モデム観測、連続ロガー、NAS、長期/再起動試験は未完了。

## ダッシュボード・再生画面の改善と再起動確認

- ユーザー観点のレビューを3担当で分担。走行距離/走行時間を同一枠にし、7/30/90/365日切替、90日は週、365日は月集計。欠測と実測0を分離した。
- 通信量はNCCによる記録開始以降の受信＋送信。Wi-Fi/LTEを区別し、当月全体の通信量と誤認しない記録開始日時を表示する。
- 接続状態、GPS、CPU/RAM/温度、SDとアプリ保存領域の空き容量を整理。カーネルやファイルシステムはトップから除外。
- 録画ライブラリをドラレコ再生へ変更。確定済みMP4だけを索引し、日付・時間軸・録画区間から再生。純正録画のH.264/AACを再エンコードせず配信する。
- GNSSは純正録画のNMEA sidecarを読取り。有効時刻/測位のない実機データは距離0に変換せず欠測とする。過去の位置を現在位置として扱わない。
- LEDは提供参考に沿う5ゾーンの単色/レインボー/消灯を実装。短時間の実機試験でworkerのコマンド受理、更新、終了時消灯命令を確認。物理的点灯状態の読戻しは非対応。
- `/etc` overlayの永続upperとliveの不一致を修正。実機再起動1回でWeb 80/443、登録済みTailscale、カメラ/SD、純正サービスの自動復帰を確認。再起動後の実PCからtailnet経由HTTP 200も確認した。
- 320/390px、PC、ライト/ダーク、欠測/実測0/長いSSID/長期間グラフをブラウザー試験で確認。画面レビューの詳細は `usability-review.md`。
- 未解決: 純正所有ATポートを奪わずに取得するLTE観測経路、常時AP制御、継続位置ロガー、H.264ライブ/マイク、NASおよび24/72時間試験。

## 現状確認用UIの再配備

- 4GとWi-Fi STAの集計を分離。GobiNet物理親IFだけを4Gへ計上し、子IF・bridge・AP・Tailscaleを重複加算しない。専用の軽量カウンターAPIを2秒周期で採取する。
- 4Gを主表示し、設定可能な月間上限から記録分を引く残量ドーナツ、上下bps、Wi-Fi補助欄を追加。未設定/欠測/ゼロ/超過/更新停止を分離した。
- パンくず、ページタイトル、メニュー検索、OVERVIEWカテゴリと上部バーを削除。接続状態・Tailscale・再読込みを左下へ集約。リンクは右上ExternalLinkで統一。
- メニューはNETWORK / DRIVE RECORDER / NP1 SERVICE / MANAGE。SD、LED/音声、USB、ログ、設定/バックアップはシステムのタブに統合。旧画面hashもタブへ対応させる。
- SD使用率だけの警告は削除。残量・使用率は本体情報として中立表示。
- 原本再生、モバイル操作、通信上限/速度、欠測、設定競合などブラウザー17試験とGo tests/vet、実機SD/カメラ/純正サービス検証が合格。
- 独立NAS clientの単発Get Signal Infoが実機で成功し、純正6サービスのPID/state維持を確認。Web常駐observerは現在取得を確認できておらず、画面では未知のままとする。登録/PLMN/バンド追加照会はopt-in実装のみで実機未検証。
- 独立APの20秒試験はENABLED、後処理、純正PID/state、STA、IPv4/IPv6経路・rule維持まで合格。DHCP/クライアント接続/常時APはまだ未完了。
- NP1設定は純正共有mapの安全な読取りを解析中。現状配備UIは解析中・設定変更不可と明示し、仮の成功表示を出さない。

## 設定操作・管理APの実機確認

- NETWORKを4G/Wi-Fi・AP/Tailscaleへ整理し、通信量は4G画面の詳細へ、接続端末はWi-Fi/APへ統合。4G専用画面はAPN編集を中心に作り直した。保存済み本体profileと現在PDPのAPNを区別する。
- 固定NAS GETから登録/PLMN/バンド/RSRP等を専用workerで継続取得。独立WDS GETでprofile1をroot0600へ保存、固定profile一覧でindex1のみ存在を確認した。profile2を空のprofileと偽らない。既存profile1は編集用private設定へ非上書きで取り込んだ。回線へのAPN適用は未完了。
- 後続車検知と感度は純正SetStatus/CSSET通知経由で変更し、純正Settingの永続保存fileと元値復元を確認。専用root workerへ分離し、Web API往復も両方合格した。
- Alexaは純正VoiceのPID宛て要求/応答でget/changeでき、専用queue撤去と全8サービス維持、native85と保存544のON→OFF→ONを確認。Web変更は一度未確認応答が発生したため原因を調べ、成功扱いを保留している。元ONへの復元は確認済み。
- 常駐管理APはWPA2/SSID編集、DHCP、associated端末一覧を実装。socket返信先・子process確定待ち・own DHCP fd照合を修正し、Windows実機のWi-Fi接続→DHCP→HTTP管理画面→端末検出が合格。STAなし維持とNATは次段。
- 常時ロガーを個別許可なしで起動し電波記録の増加を確認。今の実機は有効GNSSがなく、位置なし観測を蓄積する。SD FIFO削除後の走行集計保持は別途対応が必要。
- 導入時に純正の新規OTA取得抑止flagを作成し、privateに初期状態を保存。設定で再有効化可能（非推奨）、進行中download/verify/activateや純正serviceは停止しない。
- 距離/時間の棒と累計線、日/週/月、全期間/期間指定、日別詳細と電波マップ、動的ライト/ダークを配備。現画面49browser試験とAlexa7browser試験が合格。実機の原本MP4 Range・両カメラJPEG・純正サービスも再確認した。
- Tailscaleだけに既存ADB5556の到達を許可し、PCからroot shellを確認した。新設定とともに再起動後の確認を継続する。

## 追加の往復・復旧試験

- AlexaのWeb操作で重複していたlive照会を減らし、各phaseのprivate診断を残した。Web経由のON→OFF→ON、Voice再照会と保存544の一致が9.8秒で合格した。
- STA検索中も管理APを保持し、Windows119回連続HTTP/station確認、90秒自動復旧・手動復旧、STA未接続中のAP worker再起動を確認。既定standaloneを有効にするsourceは配備待ち。
- APN ownerのprivate D-BusへNameを奪わず接続し、固定0aの返答・宛先PID/seq一致を確認した。固定同値profile1の0b試験はprofile1全39TLVとprofile2不在を維持したが、Wi-Fi default1本を削除した。元Wi-Fi証跡/IPv4/7純正PIDを照合し、欠落した元defaultだけを復旧、HTTP/AP/Tailscale復帰を確認。firewall差分は時刻コメント/counterだけで実rule変更なし。APN Web適用はこの副作用を扱うトランザクション実装まで開放しない。
- 既存の30秒履歴cacheを現在GPSへ流用すると新しい測位を取り逃すため、最新4sidecarを軽く読むLiveIndexを別に実装。1秒cacheと測位時刻の検査をhost検証済み、実機配備を継続する。
- まとまった変更をcommit `6a2afab` に保存し、Gitへ入るsourceだけの隔離clean buildも確認した。private資料・素材・実機識別値は除外を維持。新しい拡張native/APN/長期保持作業は次のまとまりとして継続する。
