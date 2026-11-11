# 走行距離・時間の長期保持

SDのFIFOで純正NMEAが消えても、NCCが既に観測した距離・走行時間と区間詳細はアプリの永続領域へ残す。位置記録の個別許可は要求しない。原本の書込み・削除や純正ロガーの設定変更は行わない。

## データと重複の扱い

`trips.MeasuredBatch` はチェックサム・測位品質・時刻・速度・5秒以内の間隔を通過した隣接測位点だけから、開始/終了時刻・距離・走行秒を作る。録画の長さを走行時間へ変換せず、無効測位や欠落をまたいで補間しない。raw座標、映像、ファイル名、SD/機器の識別子を長期集計へ複製しない。

`trips.Archive` はJST日内の連続した実測coverageへ区間を合併する。通常は1Hzの点が増えても保存件数は走行と欠落区間に対応し、全ての測位点が永続化され続ける構成にはしない。

- 既存coverage内の区間は再計上しない。再起動、同じSDの再走査、過去のカードへの交換で総量を増やさない。
- watermarkより古くても、まだ覆われていない区間は追加できる。後から欠落の間を埋めた実測区間は隣接coverageと結合する。
- 既存coverageと一部分だけ重なる別サンプリングの区間は、残りの距離を推測せず拒否し、partialとして残す。
- 同じ時間範囲に後から別の測定値が現れても、最初に保存した測定量を置換しない。過去値の補正・再計算機能は別仕様とする。
- 日跨ぎは個々の実測interval内の時間比で分割する。日付指定の距離を走行全体の平均から逆算しない。実際には測位していない午前0時の境界を測位点数へ加えない。
- 未来のintervalや5秒を超えるintervalは受理しない。システム時計が後退した場合、未来に終わる保持区間を表示から外し、その応答をpartialにする。保持データ自体は消さない。
- 単独測位や未測位だけの日を「走行距離0」「走行時間0」に変換しない。連続測位中の停止は実測0として区別する。

## 保存・上限

非root Web/APIプロセスの `state/journeys/archive.json` へ保存する。GETはディレクトリ/ファイルを作成せず、背景collectorの `Ingest` だけが作成・更新する。

1. 全batchの型・時刻・値・並び順を検査する。
2. 現在値のcopyへ区間を取り込む。
3. SHA-256つきversioned envelopeを同じディレクトリの0600一時ファイルへ書き、fsyncする。
4. renameで置換し、Linuxでは親ディレクトリもfsyncする。
5. 保存成功後にactiveなメモリ値を更新する。

読み込みはサイズ、schema、checksum、日付/coverageの並び、値の範囲、Linuxの所有者・0600を検査する。壊れたファイルを空の履歴として上書きしない。symlinkのファイル/親ディレクトリを拒否する。Linux以外のhost試験ではPOSIX ownershipとdirectory-fsyncを保証した扱いにしない。

上限は日付36600件、連続coverage 50000件、ファイル16MiB、入力batch5000interval。上限で古い総量をFIFO削除せず、受け入れられない新規区間をpartialとして扱う。既存coverageの延長・結合で上限内に収まる場合は継続できる。上限到達や部分overlapの発生は `ArchiveState` に残る。これは無期限・無制限の保存を保証するものではない。

watermarkは検査済みintervalの走査位置であり、全ての区間を保存できたという証明ではない。制限による拒否があっても収集が同じページで停止し続けないよう進める。partial/limit状態とcoverageを併せて判断する。`overlapRejected` は拒否した取込試行回数で、再照合を含むためユニーク区間数ではない。

重複だけのpollは測定量ファイルを書き直さない。再読取り時刻だけの変化もディスク書込みの理由にしない。新しい測定量、診断件数、backfill/制限状態が変化した場合に保存する。

## 収集とAPI

内部brokerの `GET /journey-intervals?after=<RFC3339Nano>` は最大5000件・1MiB以下の測定intervalと、元NMEAの未測位/読取エラー/切詰め診断を返す。pathや読み出し上限をcallerが指定する経路は設けない。

背景collectorは30秒ごと、1回最大4pageを取り込む。起動時と10分ごとにcursorをゼロへ戻してSDを再照合し、遅れて現れたsidecarやカード交換を検出する。通常のforward cursorは保存済みwatermarkを使うが、それだけを重複判定の根拠にはしない。再照合中でも既に保持した値を削除しない。元SDが未挿入/読取不能でも保持済み履歴を表示できる。

公開 `GET /trips` は既存 `HistoryReport` の形を維持し、7/30/90/365/all/custom、JSTの開始/終了日（両端を含む）を扱う。元NMEAが現在未測位でも診断件数を保持する。区間詳細は新しい1000件に制限するが、日別/期間総量を1000件だけの合計にしない。

`GET /trips/archive` はcollector稼働、直近poll、収集エラー、保存watermark、coverage件数、partial/backfill/容量制限を返す。画面から保存を開始する許可操作は不要。

`HistoryReport.sourceTruncated` は元sourceの切詰め、backfill途中、部分overlapの拒否、保存上限などの保守的な制限表示に使う。「新しいSDファイルだけを表示している」とは限らない。source metadataのファイル件数/未測位件数は最後に成功したSD観測の診断で、永久累計ではない。

## 検証

host単体/API試験で、再起動・FIFO削除・別cardの過去データ・遅延gap埋め・重複・異なるsamplingの部分overlap・日跨ぎ・午前0時endpoint・欠測と停止0・時刻逆行・件数上限・checksum不正・symlink・atomic置換失敗・ページ収集上限・source停止後の表示・GETの非書込みを確認した。

```powershell
go -C backend test ./internal/trips ./internal/adapters ./internal/api
go -C backend vet ./internal/trips ./internal/adapters ./internal/api
go -C backend test ./internal/trips -run '^$' -bench BenchmarkArchiveDenseIntervalBatch -benchtime=1x -benchmem
```

実機の有効GNSSデータ、長期間の電源断/SD交換は別の実機受け入れ確認とする。hostの合成データを実機走行の成功記録として扱わない。

## 画面を開いたままの更新

距離・時間グラフはダッシュボードも含めて保持APIを読み、30秒ごとに更新する。SDに残るデータだけのsummary値で365日集計を代用しない。非表示タブでは取得を中断し、再表示時に読み直す。同じ期間の取得を重ねず、期間変更・画面終了で古い要求をcancelし、遅れた応答を新しい期間へ反映しない。

背景更新ではグラフ・カスタムの日付draft・日/週/月・選択した日・展開した日付一覧を保持する。更新失敗時は確認済みの前回値を残して再試行を案内し、未確認値へ置換しない。5件のbrowser fixtureで更新、選択保持、hidden/visible、request abort、遅い応答、失敗からの回復を確認した。
