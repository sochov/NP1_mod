# 固定APNプロファイルの単発読取り計画

## 変更と前提

独立WDS CIDからGet Profile Settingsの3GPP index 1/2だけを読み、APN変更前の原状をroot-only領域へ保管する。純正AT所有・rootfs・modem profile設定・既存接続・純正サービスには書かない。NASの常時observerを自作unitだけ一時停止し、trapで元のactive状態へ復帰する。

実行は `python tools/deploy.py --serial SERIAL --action gobinet-profiles-readonly`。serialはCLI引数だけで渡す。専用 `/tmp/ncc-profiles-stage` にmanifest検証済み自作bundleを置き、既存のnative trial用stageと競合させない。

## 保管と復旧

- 実行前後のCommunication/DriveRec/ImageRecog/Voice/QMMF/NEventWatcherCom/SettingのPIDとstateをprivate `/run/np1-apn-profiles-probe` へ記録する。
- 生QMI応答は固定 `private/apn/recovery` のroot:0700以下、root:0600のimmutableファイルへ保管。既存captureを上書きしない。
- helperを15秒で停止し、timeout時は自動再試行しない。成功・失敗・通常signal時とも、元がactiveだった自作modem observerを再開する。
- 復帰後はactiveだけでなく、observerが実測のsignalを取得するまで最大30秒待つ。
- stdoutはqueryのstatus/count、field完全性、read-back結果だけ。APN/credential/raw TLV/backup名は出さない。

## 合格条件

2つの固定GETの成功、private backupのroot所有・0600と厳密Load read-back、全7純正PID/stateの前後一致、自作observerの実測signal復帰を確認する。PDP/credential等が省略されていれば、取得成功でもnative復旧準備完了とはしない。APNの適用・復旧操作はこのactionに含まない。

検証結果は `docs/apn-profile-recovery.md` へ追記する。生データ・固有値はGitへ保存しない。
