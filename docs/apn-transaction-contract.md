# APN1適用jobと私有設定の契約

HTTPは設定保存と実機適用を分ける。保存はprivate editorのrevisionを更新するだけで、`AppliedRevision` を動かさない。適用はprofile1有効/profile2無効だけを最初の対応範囲とし、任意profile・IPv6・Create/Delete Profile・AT command等を受け取らない。

## job

- POSTの入力は `expectedRevision` だけ。APN/user/passwordをworker socket、argv、環境へ重複して渡さない。root workerが固定private configを読む。
- POSTは202でopaque job ID、revision、queued状態、開始時刻を返す。実機処理を20秒HTTP timeoutへ載せない。
- GETはjob状態とredacted resultだけを返す。2秒pollまたはSSEで進捗を表示する。保存されたcredential値やprivate capture、経路・設定ファイル名を返さない。
- 同時実行は1件。queued/running中の別applyは409、再送は同じjobを返す等で重複writeを防ぐ。
- 完了は `NativeAPNApplyResult.Status` とnullable readbackに基づく。`applied` は保存profileの反映確認であり、Internet接続の成功とは異なる。PDP状態、CM process、Linux IPv4/defaultの有無は別項目にする。
- `restored`、`readback-incomplete`、`recovery-required` は成功へ丸めない。worker再起動中のjobはinterrupted/unknownとし、再適用を自動で行わない。

## 2つのlockとrevision

APNStoreは同一プロセス内の `s.mu` try-lockと、broker/root workerを跨ぐ固定 `config.lock`（production root0600、NOFOLLOW）のflockを実装した。Windows開発hostはLockFileExを使い、実OSの子プロセス試験で排他を確認する。Linux開発hostではcurrent UIDのprivate test directoryも扱い、production workerはrootで固定directoryを使う。順序は必ずeditor config lock → modem native apply lockとする。

workerはconfig lockを保持したままrevision検査、private snapshot、実機transaction、同じrevisionへのAppliedRevision更新を一括で行う。Save/seed/MarkAppliedも同じlockを取る。保存側のtry-lock失敗は `APPLY_IN_PROGRESS` を返す。ForApply単独のprivate snapshot取得もbusyを明示して拒否する。GETはatomic configのpublic viewを読み、長時間のapply待ちを要求せず、`applyInProgress/busyReason` を返す。GETでlockfileやdirectoryを新規作成しない。

`ForApply` でsnapshotを取ってlockを解放し、後から `MarkApplied` するだけでは、別プロセスで保存された新revisionと実機値がずれる。lock保持中にpublic `MarkApplied` を再呼出す設計も、mutex/flockのself-deadlockを起こす。専用 `WithAPNApplyRevision(expectedRevision, func(modem.NativeAPNProfile1Input) error)` はprivate snapshotからstore内部でopaque profile1入力を作り、callback nil error後にrevisionとprofile内容を再照合して内部writeする。callbackエラー、stale revision、外部変更ではAppliedRevisionを更新しない。

`APNSecretProfile` のJSON credentialは除外済みだが、fmtは平文fieldを展開するためlogへ渡さない。新callbackへはその型を渡さず、store内で `modem.NewNativeAPNProfile1Input` のopaque型へ変換する。この型はfmt/JSONの両方をredactし、適用APIはtyped profile1しか受け取らない。

## root workerと外側の期限

専用workerのUnix socketは固定path・root/brokerのpeer credentialを検証し、fixed operationとrevision以外を拒否する。own modem observerを停止・復帰する期間と、他のnetwork/native操作を調整する。WDS kernel syscallはGo contextで必ず中断できるとは限らないため、固定helper子プロセスに外側timeoutを設け、kill後waitが完了するまで次を開始しない。

workerは自身をcopyした固定 `/run/np1-control-apn/ncc` の `apn-apply-revision` を子として使い、argvにはrevisionの整数だけを渡す。子がeditor lockを取得し、private入力の構築、適用、observer復帰、同じrevisionのApplied更新まで保持する。出力はversion/revision/AppliedRevision/redacted result/固定error codeだけで、APNを含むSettingsViewのProfilesは出さない。

親watchdogは90秒で、timeout・shutdown時も子をkillしWaitが終わるまで次を開始しない。子の適用には35秒、transaction内部のroute cleanupには独立10秒、observer stopには20秒、startと実測待ちには合わせて30秒の上限を持たせる。最悪条件で外側期限へ達した場合は成功とせず、親がreap後に独立12秒の限定route recoveryとobserver再開を試す。shutdown時の復旧待ちを含めunitのTimeoutStopSecは90秒。kernelがkill後も終了しない場合はreap完了まで次jobへ進まない。

observerの再開合格には、startの直前以降に採取され、10秒以内でsignal query available、RSSIまたはRSRPが実在する新規sampleが必要。service activeや再開前のstate fileでは合格しない。私有pause markerは実測確認後にだけ削除し、失敗時は保持してworker readinessをfalseにする。worker再起動でもそのmarkerを処理し、復帰未確認のままapplyを開放しない。

queued状態のrestartはnative writeが始まっていないためroute復旧を行わない。running状態ではjobのStartedAt以降のprivate backupだけを使い、古いjobのsnapshotを流用しない。job/markerはbounded strict JSON、通常file・owner・permission・NOFOLLOWとopen前後の同一fileを検証する。nil errorだけではjobを成功にせず、Status=applied、profile1照合、profile2不存在、management settled、recovery不要、AppliedRevision一致をすべて確認する。

必要な書込みはAPN private directoryとown runtimeに限定する。route変更にはNET_ADMIN、全proc FD所有者確認にはSYS_PTRACE等をworkerだけへ与える。STATUS reply socketはNEventWatcherCom group0660、vendorが探索可能な親directoryが必要。private evidenceは0700の子directoryへ分ける。AF_UNIXとIP toolが使うAF_NETLINK/AF_INETを許可する。

## 実機確認の段階

純正ownerのread-only0a、private profile backup/list、editor seed、Go management baseline/dataLinkのfield試験は合格済み。同値0bではprofile全39TLV/p2不存在/PDP3/2を維持したが、元WiFi default削除の副作用があり、条件付きroute1本の復旧でHTTP/Tailscaleも回復した。

改訂transactionの同値試験では一時的にCM/usb0.1 IPv4/4G defaultを実測したが、その後純正CommunicationのStopMComでCMと4G defaultが消えた。競合する正当な4G defaultを勝手に旧WiFi defaultへ置換せず、消失後に同SSID/IP/純正PID・default不存在を再照合し元WiFi default1本を復旧した。HTTP/Tailscaleと新規modem signalの回復を確認した。profile適用の成功・継続4G出口の成功とは報告していない。

2026-11-11のworker修正はhostのunit tests/vetとCGO0 ARM64 compileまで合格。実子プロセスのtimeout/取消後reap、停止失敗時もobserver復帰、旧sample拒否、復帰失敗時marker保持、job/childの曖昧な成功拒否、private receiptの型・hash・時刻を検証する。Web apply job、異なる設定値と原状戻しは未合格。`field-ready.json` は作成しておらず、unit Conditionとworker内部のreceipt照合で未検証writeを開放しない。実機writeは他の設定試験と競合しない専用windowで行う。
