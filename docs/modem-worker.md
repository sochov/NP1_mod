# モデム観測プロセスの分離

brokerはモデムhelperを起動せず、proc FD一覧・QMIデバイスへアクセスしない。`adapters.ModemObserver` は `/run/np1-control-modem/state.json` の限定構造体だけを読み取る。元のbroker側取得失敗はproc所有者検査の権限不足が候補であり、分離前に原因を確定したと主張しない。

`np1-control-modem.service` は専用root workerとして動作する。brokerの実行ファイルや起動順への依存を避け、unit自身がoverlayの成果物を専用 `/run/np1-control-modem/ncc` へコピーする。workerは自身の `os.Executable()` を検証し、この固定パスからだけ単発helperを起動する。

- runtime directory: root:ncc、0750
- JSON状態: root:ncc、0640、同一directoryの一時ファイルへ書込み・fsync・rename
- 能力: CAP_DAC_READ_SEARCH、CAP_SYS_PTRACEを専用workerだけに限定。brokerへptrace権限を付けない
- device policy: qcqmi0のread/writeのみ。任意のraw blockやTTYは許可しない
- helper: shellを介さず固定modeのみ。最大12秒、標準出力16KiB制限、stderrは状態JSONへ含めない
- 周期: 15秒。前helperのWait完了まで次を起動しない

`modem.RunWorker(ctx, modem.SignalOnly)` は信号Getのみ。`modem.WithStatus` は検証済み固定NAS登録/バンドGetも含める明示的な選択。mainのworker mode、配備・postboot・停止処理はroot側で統合する。追加Getを既存signal-only入口から暗黙に開始しない。

状態にはschema version、選択mode、状態、採取開始/公開日時、固定error code、型付き信号/登録情報だけを含む。raw QMI、PID/FD一覧、SIM情報、資格情報を公開しない。読み側はroot所有・書込権限・通常ファイル・サイズ・JSON schema・取得元/版・数値範囲・照会成否と値の整合を検証する。

worker更新日時だけで古い信号を新鮮に見せない。採取時刻とworker更新時刻の両方を確認し、35秒超・取得失敗・停止はstale、初回未取得や起動失敗はunknown。状態ファイルが消えた場合、brokerの取得済み値はstaleとして残し、数値を0で置き換えない。PS attachからPDP接続やインターネット疎通を推測せず、`pdpConnected` は独立取得までnilを維持する。SNRとSINRも別に扱う。

ctx取消ではhelperを終了させ、Wait後にstopped状態を公開する。systemdはcontrol-group全体を停止し、20秒の停止時間制限を持つ。ただしkernel内の非割込み待機はGo/シグナルでも即時中断できない。こうした場合に並行helperを増やしたり純正FDを閉じることはしない。デバイス側FDオーナー検査も継続する。

検証: helper明示mode、16KiB超/未知schema拒否、実プロセスの時間制限、直列実行、ctx取消後のWaitと停止遷移、起動時公開失敗、状態atomic保存、provenance/mode整合、freshness、PDP推測禁止、欠測/取得済みstaleをGo test/vetで合格。Linux/ARM64 CGO0 compileも合格。実機unitの起動と権限確認・配備試験はroot担当であり、この実装担当から実行していない。
