# Windows Wi-Fiを変更する試験の既定拒否

ユーザー指示により、Windows Wi-Fiの接続・切断・設定・profile追加削除は禁止中。以前の実機試験許可はWindows操作の許可として再利用しない。ユーザーが許可するまで実行を再開せず、繰り返し許可を求めない。

次の入口は、既定で入力読取り・ADB・出力directory作成・ネットワーク操作より前に終了する。

| 入口 | 将来の明示許可がある場合だけ使うflag |
| --- | --- |
| tools/verify-cellular-hotspot.py | --allow-windows-wifi-changes |
| tools/verify-hotspot-internet.py | --allow-windows-wifi-changes |
| tools/ap-sharing-trial.py | --allow-windows-wifi-changes |
| tools/ap-standalone-trial.py | --allow-windows-wifi-changes |
| tools/diagnose-ap-continuity.py | --allow-windows-wifi-changes |
| tools/verify-ap-client.ps1 | -AllowWindowsWiFiChanges |
| tools/verify-ap-continuity.ps1 | -AllowWindowsWiFiChanges |
| tools/verify-ap-internet.ps1 | -AllowWindowsWiFiChanges |

Python wrapperは自分のopt-in確認後だけ子PowerShellへflagを渡す。flagは認可の証明ではないため、現在の禁止指示下では使用しない。既存の手順書に過去の実行コマンドが残っていても、この制約が優先する。

`tests/test_windows_wifi_guards.py` はflagなしの拒否だけを実行し、ネットワーク操作に到達しないことを確認する。既存の4G連動試験のstate machineはfake IOで検証する。今後はNP1内の固定宛先・IF/source bind DNS/TLS検証を優先し、NP1自身の外向き疎通とAP端末からの転送確認を混同しない。
