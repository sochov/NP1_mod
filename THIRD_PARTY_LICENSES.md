# サードパーティライセンス

NP1 Control Centerの独自コードは [MIT License](LICENSE) です。以下の依存ソフトウェアをMITへ変更するものではありません。それぞれのライセンス本文、著作権表示、追加のNOTICEを保持します。

## 一覧と原文

[コンポーネント一覧](licenses/COMPONENTS.md) に、固定バージョン、ライセンス、利用範囲、上流の配布元、保存したライセンス原文へのリンクをまとめています。[機械可読な一覧](licenses/inventory.json) は原文のSHA-256を含みます。

- npm: `frontend/package-lock.json` の全依存関係。実行時とビルド・テスト用、任意のOS別ネイティブパッケージを含みます。Vite、Rolldown、Playwright、TypeScriptに同梱された追加の権利表示も保存しています。
- Go: `backend/go.mod` が解決するモジュールグラフ全体と、リンクされるGo標準ライブラリ・ランタイム。Goの追加PATENTSも保存しています。
- Python: 解析PCのSSH検証で任意利用するParamikoとその依存関係。NP1配備物にこれらのPythonパッケージを組み込みません。記録されたバージョンは検査した環境のものです。
- radio-map: 利用者提供のMITプロジェクトを設計の参考にしました。NP1向け実装は独立していますが、元のCopyright 2026 sochoとMIT本文も保持します。
- Tailscale: 任意の準備ツールが公式の固定バージョンを別途取得します。このGitリポジトリはそのバイナリやソースを含みません。上流のBSDライセンスと配布元を一覧に示します。公式バイナリ自身の依存関係については [Tailscaleのライセンス案内](https://tailscale.com/licenses) も参照してください。

MPL/LGPL等の依存関係もOSSとして元の条件を維持します。ビルド用Lightning CSSのMPLや任意のホスト用ParamikoのLGPLを、プロジェクト全体のライセンスとして表示しません。依存ソフトウェアの変更版を再配布する場合は、その上流ライセンスに従ってソース・権利表示等を扱ってください。本リポジトリではこれらの依存ソースを改変・同梱していません。

## 純正ソフトウェアと公開対象

NP1の純正ファームウェア、純正ライブラリ、アプリ、SDKヘッダー、逆コンパイルしたソース、実機から取得したバイナリ・共有メモリー・設定・録画は配布しません。秘密値や非公開資料もGit対象外です。純正連携用のコードは本プロジェクトの実装で、対応確認に必要な識別子、ハッシュ、数値、観測した通信仕様と調査要約を含みます。純正ライブラリが必要な機能は、利用者の端末上にある既存ファイルを実行時に使用します。メーカーのソフトウェアをMITで再許諾するものではありません。

ライセンスが付いていない利用者提供のLEDサンプルは再配布していません。LED制御ではsysfsの属性・値という互換性情報を参考に、Go側の制御を実装しています。

## 地図・外部サービス

背景地図はOpenStreetMapの公開タイルを利用し、画面に© OpenStreetMap contributorsのリンクを表示します。地図データ・タイルはこのGitリポジトリに含みません。地図の利用条件は [OpenStreetMapの著作権・ライセンス案内](https://www.openstreetmap.org/copyright)、標準タイルの利用条件は [Tile Usage Policy](https://operations.osmfoundation.org/policies/tiles/) を参照してください。外部サービスの名称・商標の権利は各権利者に帰属します。

## 更新と検査

通常の確認はネット接続・追加依存不要です。

```powershell
python tools/third-party-licenses.py
```

依存更新時は`npm ci`とGo依存取得、任意のPythonホストツールのインストール後に原文を再収集します。ローカルの参考資料や上流から取得したLICENSEのパスはコマンド引数で指定し、公開一覧にはローカルの絶対パスを記録しません。

```powershell
python tools/third-party-licenses.py --generate --go GO_PATH --reference-license REFERENCE_LICENSE --tailscale-license TAILSCALE_LICENSE
```

不明なライセンス、本文の欠落、固定依存と一覧の不一致、原文の改変はエラーにします。パッケージの追加・更新時には上流の本文と追加の権利表示を再確認してください。
