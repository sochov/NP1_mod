# 純正CMの所有者検証（source段階）

正式STA OFFから純正CMと4G Linux defaultが立つことを確認した。一方、現在のGobiNet getterは他processがqcqmi0 FDを保持すると一律に拒否する。PDP/転送が動いているときにNAS信号やWDS設定を取得できない理由であり、欠測を正常値へ丸めない。

`stock_cm_owner*.go` と `stock_cm_inspect*.go` は、将来の固定GET単発試験へ向けたprovenance検証だけを実装する。現 `inspectQMIOwnership`、NAS/WDS getter、背景worker、main/APIへの接続はしておらず、concurrent GETを暗黙に許可していない。

`InspectStockCMOwnership(ctx)` はproc/sysfsと固定executableのhashを読む。AT/QMI送信、modem device open、他processのFD open/dup、CM起動停止は行わない。返すのは時刻/source/status/owner検証bool/CM数/QMI FD数/verifiedだけで、PID、UID、cmdline、cgroup、IP、credentialを公開しない。私有identity型のfmt/JSONもredactする。

要求する証拠:

- 固定GobiNet module版/hash、EC25 USB祖先は既存getterと同じ検証。
- `quectel-CM` は `/usr/bin/quectel-CM`、SHA256 `def4cbfafdb1aa12d0ed1ff6994e7b9a11f30d33cd2e0a81e4d86189f60e9d89`。
- 既存NEventWatcherComのexe/hashが一致して一意、非rootのreal/effective/saved/fs UID/GIDがすべて一致すること。
- CMのUID/GID、全cgroup hierarchy、network namespaceをそのlive ownerと照合。systemdのNEventWatcherCom.service所属も必要。実機では16hierarchyがあり、すべてがservice pathを持つわけではないため、各hierarchyのlive owner tupleを使う。
- liblteapi .rodata98a8にある固定起動 `quectel-CM -n 1&` と一致する3 argvだけ。profile2、`-s`、kill/reset/任意optionは拒否する。保存profileの値をargvから読んだことにはしない。
- CMが一意で、観測されたqcqmi0 FDの所有者がそのCMだけであること。FDはsymlink metadataだけを読む。
- procstatのPID/starttime、exe/hash/argv、UID/GID、cgroup、netnsが前後一致すること。PID再利用や別namespaceを許可しない。

headerやargv parseはboundedで、異常長・重複UID/cgroup・不正整数・欠損・プロファイル違いを拒否する。host tests/vetとCGO0 ARM64 compileを合格した。現在のstock ownerのUID/cgroup等はread-onlyで私有保存したが、CM存命中の全tupleと独立GETのfield試験はまだ未実施。

次のSTA試験launcherは、CMを初めて見たときだけproc status/stat/cgroup/cmdline/netns/exe hash、QMI FD個数をprivateに記録する。この採取もQMIをopenせず、GET許可を自動作成しない。rootのservice制御試験とはnetwork write windowを分ける。
