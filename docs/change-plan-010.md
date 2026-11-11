# 純正設定helperの前提確認

Bluetoothの純正Setting requestと同じvendor設定APIへつなぐため、独立helperを段階的に検証する。

- 初段は `native-preflight` だけ。固定vendorファイルのfingerprint、初期化済み共有map、既存semaphoreのGETVALを確認する。vendorライブラリはロードせず、設定を変更しない。
- 自作ELFだけを所有者アプリのexperimentalへ置き、noexecを考慮して固有 `/run` へコピーして実行する。stockファイルには書かない。
- trial scriptは前後の純正Settingを含む7サービスのPID/stateと、共有設定のread-backをprivate runtimeに記録する。
- vendor-read、実際の設定変更/復元は、helperのconstructor/lock/IPC/参照数の検証後に別の明示的段階として実施する。初段成功を設定変更成功として扱わない。
- 既知enumである後続車検知・感度だけを最初の対象とする。未確定enumや初期化/format/resetは受け付けない。

vendor-readは、DSO探索パスとfini順序の問題を解消した版で実機成功。次段は現在値1（標準）と確認した感度を0（低）に変更し、4秒保持・再取得後、expectedValueを照合して元の1へ復元する。純正7サービスのPID/state、2設定値、4semaphore、自己mapping/refcountの前後一致を検査する。raw blockやstockファイルを直接編集せず、純正APIと通知だけを用いる。

この往復試験は感度・検知ON/OFFとも合格し、純正Settingの保存fileも変更値→元値を確認した。Web側は別root workerの固定Unix socket経由に分離し、感度のPUT→永続保存read-back→復元も合格。共有IPCはapluser補助groupで許可し、Web本体や通常brokerへ共有mapの書込み権限を追加しない。workerは所有runtimeと既存victoria map以外へ書き込めず、パス/任意コマンドをHTTPから受け取らない。設定fileは純正Settingが保存する。
