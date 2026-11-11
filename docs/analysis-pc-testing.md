# 解析PCを使う無線試験

WindowsのWi-Fi接続・プロファイル・設定は変更しない。利用者指定の解析PCへSSH接続して試験する。内蔵Wi-Fiは外部接続を担うため変更禁止。USB Wi-Fiも別作業に使われる可能性があり、未接続表示だけで空きと判断しない。現在のAP、他のinterface、所属PHY、capture/supplicant process、NetworkManager、route/DNSの占有を確認してから使用する。Bluetoothは使用可能。

`python tools/analysis-pc.py --out tmp/<fresh-directory>` は読み取り専用inventoryをSSH stdinで実行する。接続情報はignored `tmp/analysis-pc-access.json` のhost/username/password（任意port）に置く。初回host keyをignored known-hostsへ固定し、以後の変化を拒否する。passwordをargvや公開fileへ入れない。

内蔵storageの余裕が少ないため、巨大材料やbuild一式を送らない。一時的な無線試験の小さいconfigはtmpfsを使い、終了時に所有物だけを撤去する。空きUSBを一時netnsへ隔離する場合も、元namespace・管理状態・link状態を記録し、外部uplinkと使用中AP、host route/DNSが変わっていないことを前後で検査する。未使用USBの同定と復旧を確認するまで実行しない。

SSHの成功、USBの接続、NP1-origin疎通、実APクライアントのNAT疎通は別の検証結果である。
