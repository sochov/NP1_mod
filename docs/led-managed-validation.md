# LED連続変更の回帰検証

root担当の実機ログで、変更ごとのsystemd restartが`StartLimitBurst=3 / 60s`へ達することを確認した。修正はworkerを常駐させ、configを100ms間隔で再読取する方式。消灯中もworkerは待機し、再設定でrestartしない。StartLimit自体は実際のクラッシュ保護として残す。

独立review担当は実機へ変更せず、新しい3つのtest fileだけを追加した。

| Test file | 確認内容 |
|---|---|
| backend/internal/led/managed_test.go | 8連続のsolid/rainbow/off変更で同一goroutine継続、off待機で追加sysfs書込なし、不正設定/JSON読取失敗で前の有効設定を保持、正常値/同revisionへの復帰、rollbackでrevisionを戻せること、取消時5zoneすべて消灯 |
| backend/internal/adapters/led_apply_test.go | active workerへの8変更でsystemctl操作0回、busy/revision/入力不正の副作用なし、failed時reset-failed→start、起動失敗503と設定/marker復元、反映未確認503時もactive workerを再起動しない |
| backend/internal/api/led_control_test.go | brokerの入力400・busy/conflict409・起動/反映失敗503と個別codeをpublic APIで保持し、汎用LED_REJECTED/exit statusへ潰さない |

新規testは9件（API分類の5subcaseを含む）、対象go vetとともに合格。managed testはfake attribute writerで各zone命令を記録し、OSのsystemctlや実機sysfsへ触れない。API testはfake broker transportを使うため、実機で同一PIDが維持された証明や、物理的な光の読取りを意味しない。実機の連続Web変更・worker PID・StartLimit非到達はroot担当のfield試験で別途確認する。

レビューでは、brokerの旧ledMuとApplyLED mutexの二重化、rollback時save/marker失敗を無視した際の復旧表示についてroot担当へ指摘した。変更の最終状態は実装とfield記録を参照する。

修正版を実機へ配備後、`tools/verify-led-updates.py --serial SERIAL`でsolid/rainbow/offを含む9回の連続変更を確認した。全変更のworker PIDは同一、ActiveState=active/Result=success、最後に元の設定と起動marker相当を復元した。証跡はignored `tmp/led-updates.json`。物理的なLEDの光学読取りは行っていない。
