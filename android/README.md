# NP1 Control Center (Android)

Pioneer NP1 Control Center の Web UI API (`/api/v1/...`) をネイティブに操作する Android アプリです。  
Echo Show 5（Fire OS / 古い Android・横画面）から Pixel 11 Pro などの最新スマートフォン（縦画面）まで対応します（`minSdk = 24`, Android 7.0 以降）。

## 主な機能

- **Web UI 同等の全機能をネイティブ実装**
  - **ダッシュボード**: 走行距離・時間グラフ、GPS現在地・録画状態・軽量ライブ映像、4Gデータ通信、ネットワーク（AP / STA / Tailscale）、リソースモニター（4G/Wi-Fi速度・CPU・RAM・温度）、本体状態
  - **4G**: キャリア・バンド・電波品質（RSRP/RSRQ/SNR）、通信量・月間上限設定、APNプロファイル 1/2 の編集・保存・適用
  - **Wi-Fi / AP**: インターネット回線切替（自動 / Wi-Fi / 4G固定）、周辺Wi-Fi検索・接続・90秒確定/ロールバック、ホットスポット設定と接続端末一覧
  - **Tailscale**: 状態・アカウント・管理IP表示、ワンタップでのアプリTailscale接続先登録、認証開始、機器名変更
  - **ライブ**: 前方・車内・両方のライブ映像表示（360p / 1080p、色調切替）
  - **ドラレコ再生**: Media3 ExoPlayer による原本 H.264/AAC ストリーミング再生、2カメラ切替、再生速度変更、24時間タイムライン、GPSトラック連動、原本ダウンロード
  - **走行履歴**: 期間別グラフ、GPS経路＋RSRP電波品質マップ、日別区間詳細から録画再生へジャンプ
  - **ナンバー**: 日本式ナンバープレート描画一覧・検索・地図・検出画像モーダル（枠表示）、NumberShare（設定・初回登録・キュー再送・手動アップロード・履歴・車両検索）、条件ルール編集・VOICEVOX発話・本体試聴・LED通知設定、認識状態
  - **純正設定**: Alexaウェイクワード、SDカードフォーマット、純正設定、操作・診断アクション
  - **システム**: 本体情報・純正サービス管理、ルーティング、SDカードファイルブラウザ、H.264詳細録画設定、前面LED/本体音声/VOICEVOX API、USB、操作ログ、NASバックアップ、OTA抑止・表示設定
- **リアルタイム・ナンバープレート検出ポップアップ（アプリ独自機能）**
  - どの画面を開いていても、新規検出したナンバープレートを即座に日本式プレートデザインでポップアップ表示（既定 1 秒間隔ポーリング、最短 0.5 秒）
  - ツールバーからワンタップで **`条件一致のみ` / `すべて` / `OFF`** を切替可能
  - 条件一致ルールタグ、NumberShare 危険車両ハイライト、前方カメラ検出画像サムネイル（枠付き）、自動消去カウントダウンバー、チャイム音（ON/OFF可能）、テストポップアップ表示機能を搭載
- **接続プロファイル（NP1 ホットスポット & Tailscale IP）**
  - 既定の「自動」モードでは、まず NP1 ホットスポット (`http://192.168.226.1`) へ接続を試み、応答がない場合は事前設定した Tailscale IP へ自動でフォールバックします
  - ホットスポット固定 / Tailscale 固定 / カスタムURL も選択可能
- **Echo Show 5 / スマートフォン両対応のレスポンシブUI**
  - 横画面・Echo Show 5 などのコンパクト横長画面では左サイドレール＋2カラム構成＋画面常時点灯（スリープ防止）
  - スマートフォン縦画面では上部ツールバー＋下部4グループタブ＋ボトムシート

## ビルド方法

```powershell
cd android
$env:JAVA_HOME = "C:\Program Files\Android\Android Studio\jbr"
$env:ANDROID_HOME = "K:\Android"
.\gradlew.bat testDebugUnitTest assembleDebug
```

生成された APK は `android/app/build/outputs/apk/debug/app-debug.apk` に出力されます。

### 端末へのインストール (ADB)

```powershell
adb install -r android\app\build\outputs\apk\debug\app-debug.apk
```
