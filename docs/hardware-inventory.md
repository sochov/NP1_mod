# 実機棚卸し（公開用）

2026-11-11。root ADBで読み取り確認。serial、MAC、SSID、IP、機器番号、位置、生ログは除外。

| 対象 | 観測 |
|---|---|
| CPU / kernel | AArch64 / 4.14.206-perf |
| OS | QTI Linux 202404231204 |
| init | systemd 239 |
| RAM | 約2.57 GiB |
| overlay | ext4、noexec、約1.7 GiB |
| SD | vfat、約29.7 GiB、使用率87% |
| 純正主要service | Communication / DriveRec / ImageRecog / Voice / QMMF / QCMAP active |
| failed unit | 初期状態0 |
| WAN | 純正wpa_supplicantのSTA経路あり。外部疎通はこの時点では未測定 |
| AP | bridge設定あり、link DOWN。SSID提供・DHCP・AP+STAは未検証 |
| Wi-Fi | wlan0/wlan1/p2p0、phy0。iw未導入 |
| モデム | /dev/qcqmi0、qmicli、QCMAP CLI存在。QCMAPとNEventWatcherComが所有 |
| camera | front/in両export存在、3,112,960 byte。NV12画像本体は先頭3,110,400 byte |
| USB | peripheral、既存所有者root ADB serviceあり |
| LED | ENE6K582 sysfsのmode/status/valueあり |
| Tailscale | 未導入、/dev/net/tun存在 |
| 録画 | DRIVERECDATA/{Video,Event,Parking,Photo}。MP4候補とNMEA sidecarあり |

## 純正制御の調査結果

Communicationの公開動的symbolに `CCOM_WifiEntity::SetWifiApMode`、`CCOM_WifiService::SetWifiApMode`、`NPMComSystem::ConnectMComAPN` 等が存在。
これは内部実装の存在を示すだけで、ABI・引数の意味・永続性・競合回避・任意SIM制御を保証しない。
`/data/data_qcmap` の読取りはI/O error。直接編集・復旧は行っていない。
純正設定を直接書き換えたり、QMI管理プロセスを二重起動したりしない。

既存所有者用postbootにはNCC以外のroot recovery処理も存在する。今回の変更対象に含めない。
