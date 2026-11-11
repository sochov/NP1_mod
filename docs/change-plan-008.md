# モデムの読み取り専用照会

HR01参照実装は別SoC/別ATポートのため、そのまま使わない。
NP1内蔵USBモデム2c7c:0125を確認し、既存プロセスが開いていない副ATポートttyUSB3だけを一度照会する。

- `/proc/*/fd`による所有確認、モデムVID/PID確認、flock。占有があれば開かず中断。
- 固定queryのみ: AT / CPIN? / CEREG? / QNWINFO / QCSQ / QENG servingcell。
- PIN・APN・回線・band・SIM契約・無線出力の変更、reset、別モデム管理daemonの起動はしない。
- termiosは取得して保存し、終了時に復元する。全体18秒、各query2秒上限。
- 生応答は/runとホストtmpのみ。SIM固有番号は要求しない。
- 成功判定は有効応答、純正service/PID/既存STA route維持。値の単位・invalid値はQuectel一次資料と照合する。
