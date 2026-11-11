# 独立NAS clientでのGobiNet信号照会

- 搭載GobiNetのversion/srcversion/module SHA256とFDごとのclient分離を静的確認した根拠は `gobinet-observer.md`。
- 対象は新規にopenする `/dev/qcqmi0` のFDだけ。既存ATポート・他プロセスFD・WDS/WDA/APN設定に触れない。他の所有FDがあれば拒否。
- NAS service=3のclient割当、固定Get Signal Info、通常closeによる自分のclient解放だけ。任意QMI messageを受け付けない。
- 外側helperに15秒上限。失敗/timeout時に純正をresetせず、再試行を自動化しない。
- ファイルは揮発 `/run/np1-gobinet-probe/` のみ。永続設定の変更はなく、復旧は自分のhelper終了/FD close。生データはGitへ保存しない。
- 純正Communication/DriveRec/ImageRecog/Voice/QMMF/NEventWatcherComのPIDとstateを前後比較し、Web/SD/カメラ/Tailscaleを再確認する。
- 成功値はRSSI/RSRQ/RSRP/SNRとして扱い、SNRをSINRへ変換しない。回線登録/PDP状態はこの照会から推測しない。
