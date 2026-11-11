# 純正CMの所有者検証

2026-11-12の後続調査: STAと4Gの同時維持は利用者の指示で今後のTODOへ移した。[独立WDS data-call試作](owned-cellular-data-call.md) に純正CommunicationのSTA優先分岐、実証したQMAP/mux対応、保存profileを変更しないStart候補、own FDとNAS observerの競合、unknown handle時の停止拒否、未実装のlive private proofをまとめた。QMAP metadata GETだけがFIELD PASSで、独立Start/Current/Stopは未実施、通常サービスへ未接続である。純正CM生存中のNAS限定GETのFIELD PASSと、独立data callの未検証を分けて扱う。

正式STA OFFから純正CMと4G Linux defaultが立つことを確認した。一方、現在のGobiNet getterは他processがqcqmi0 FDを保持すると一律に拒否する。PDP/転送が動いているときにNAS信号やWDS設定を取得できない理由であり、欠測を正常値へ丸めない。

`stock_cm_owner*.go` と `stock_cm_inspect*.go` はprovenance検証を実装する。metadata-only modeと限定GETの実測後、NAS/getterのsourceに厳格なCM経路の選択を追加した。元のexclusive `inspectQMIOwnership` は維持し、任意のforeign FDを許容する変更はない。CM検出不明・CM経路の検証失敗ではexclusiveへfallbackしない。

`InspectStockCMOwnership(ctx)` はproc/sysfsと固定executableのhashを読む。AT/QMI送信、modem device open、他processのFD open/dup、CM起動停止は行わない。返すのは時刻/source/status/owner検証bool/CM数/QMI FD数/verifiedだけで、PID、UID、cmdline、cgroup、IP、credentialを公開しない。私有identity型のfmt/JSONもredactする。

要求する証拠:

- 固定GobiNet module版/hash、EC25 USB祖先は既存getterと同じ検証。
- `quectel-CM` は `/usr/bin/quectel-CM`、SHA256 `def4cbfafdb1aa12d0ed1ff6994e7b9a11f30d33cd2e0a81e4d86189f60e9d89`。
- 既存NEventWatcherComのexe/hashが一致して一意、固定service accountのreal/effective/saved/fs UID/GIDがすべて一致すること。実機proc/passwd/groupを読取りで照合し、単なる任意の非root UIDを許可しない。
- CMのUID/GID、全cgroup hierarchy、network namespaceをそのlive ownerと照合。systemdのNEventWatcherCom.service所属も必要。複数hierarchyすべてがservice pathを持つわけではないため、各hierarchyのlive owner tupleを使う。
- liblteapi .rodata98a8にある固定起動 `quectel-CM -n 1&` と一致する3 argvだけ。profile2、`-s`、kill/reset/任意optionは拒否する。保存profileの値をargvから読んだことにはしない。
- CMが一意で、観測されたqcqmi0 FDの所有者がそのCMだけであること。FDはsymlink metadataだけを読む。
- procstatのPID/starttime、exe/hash/argv、UID/GID、cgroup、netnsが前後一致すること。PID再利用や別namespaceを許可しない。

headerやargv parseはboundedで、異常長・重複UID/cgroup・不正整数・欠損・プロファイル違いを拒否する。host tests/vetとCGO0 ARM64 compileを合格した。現在のstock ownerのUID/cgroup等はread-onlyで私有保存したが、CM存命中の全tupleと独立GETのfield試験はまだ未実施。

STA試験launcherは、CMを初めて見たときだけproc status/stat/cgroup/cmdline/netns/exe hash、QMI FD個数をprivateに記録し、metadata-only Go modeも1回実行する。この採取もQMIをopenせず、GET許可を自動作成しない。rootのservice制御試験とはnetwork write windowを分ける。

## 独立metadata-only診断

`python tools/build_stock_cm_probe.py` は通常bundleと別の `artifacts/ncc-stock-cm-provenance` を作る。固定shell `tools/device/stock-cm-provenance-readonly.sh` は自分の一時実行ファイルだけをprivate runtimeへコピーし、BusyBox形式の15秒timeoutで明示modeを実行する。純正/ownサービスの停止・再開、AT/QMI open、設定変更はない。出力はtyped proofと固定エラーコードだけ。

2026-11-12、STA ONのまま独立stageでこのmodeを合格した。結果は `status=no-cm / ownerVerified=true / cmProcesses=0 / verified=false`。CM不存在をCM所有者検証の合格へ読み替えない。生存CMの全tupleと独立GETの実機確認は次の排他windowで行う。初回はBusyBox非対応のtimeout optionで本体CLIが起動せず、固定 `-t 15 -s TERM` へ修正後に確認した。私有証拠は `tmp/stock-cm-provenance-sta-on-2/`。

## 45秒OFFの実測とparser修正

正式45秒STA OFF→元ONへの復帰を合格し、その区間の4G AP client標準TLS/UDP・TCP DNSとcellular向けNAT増分は別担当が確認した。ただしmetadata proofは `STOCK_CM_ARGUMENTS_UNVERIFIED` で失敗した。実物のcmdlineは正確な固定3argv `quectel-CM / -n / 1` だが末尾NULが無く、parserが拒否していたためである。これを全provenance合格へ読み替えない。

保存したprivate proc証拠のoffline照合では固定UID/GID、全8cgroup tuple、ownerと同netns、固定exe/hash、前後starttime、QMI FD4本が一致した。parserは固定3argvだけを、末尾NUL有/無の有限2表現で受理するよう修正した。空引数・追加NUL・空白・別profile・任意optionは引き続き拒否する。修正版のlive全体証明は次回限定probeの先行検査で確認する。証拠は `tmp/cellular-hotspot-20261112-1/sta`、個体情報の無い照合要約は同親directoryの `cm-offline-tuple-summary.json`。

## 限定GET

`stock-cm-nas-readonly` と `stock-cm-profiles-readonly` を新CLI modeに追加した。前者は独立NAS CIDの固定signal/serving-system/RF-band GET3件、後者は独立WDS CIDの保存3GPP profile1/2 GETとprofile inventoryだけ。message/transaction/profile/indexを外から受け取らず、data call開始停止・bind・APN変更・WDA・CTL操作を含めない。

新しい明示getterはlive metadataの `Verified=true` を必須とする。その後もCM/ownerのPID/start/exe/hash/argv/UID/GID/cgroup/netnsとCMのQMI FD番号集合を保持し、open後・CID後・各GET前後・close後に再検査する。例外は自分PIDの実際に開いたFD1本だけで、同processの別FDや別processの同番号は許容しない。途中の変化では取得済みraw/parsed値も破棄し、後続GETを止める。読取り不能な生存proc、未知所有者、FD増減、時間切れも拒否する。

各modeは8秒context、各QMI frameは既存の2秒poll/最大長/TLV boundsを使う。kernel open/ioctl/closeがGo contextだけでは中断できない制約は従来どおりであり、一時helperの外側timeoutとreapを併用する。自己FDのcloseだけでCIDを解放し、他所有者のFDはopen/dupしない。WDS captureは既存のroot-only private recovery保存と完全readbackを使い、stdoutは値を持たないsummary/inventoryだけ。保存profileは実際のPDP接続APNとは区別する。

独立builderに固定shell `stock-cm-fixed-gets-readonly.sh nas|profiles` を含める。STA launcherからの利用は `--cm-get-mode nas|profiles` の明示指定だけで、既定none。CM metadata成功後、OFF残り15秒以上のときだけ1回実行し、45秒の保持を延長しない。Windows Wi-Fiの接続・切断・profile変更は利用しない。次の実機network windowはcoordinatorが別途指定する。

host testsはexact FD例外・他FD/他PID拒否・close後FD残留・未知GET/WRITE拒否・送信前guard失敗時no-send・応答後guard変化時raw破棄と後続停止・private identity非出力を確認した。Go tests/vet、Linux ARM64 compile、専用bundle buildも合格した。

## NAS実機合格とworker選択

NP1だけの45秒STA OFF窓で、metadataはavailable/ownerVerified=true/CM1/QMI FD4/verified=true、NASのsignal/servingSystem/rfBand全3GETがavailableで合格した。実測signal、registered=true、LTE bandが得られ、元STAとstock PIDの復帰も確認された。証拠は `tmp/cm-nas-20261112-1/device`。Windows Wi-Fi操作は伴わない。

この合格後、`ReadGobiNetStatus` はCMありならfield合格の `ReadStockCMNAS`、CMなしなら元exclusiveへ選択するsourceを追加した。SignalOnlyは同じ所有者guardでsignal GETだけを送り、status GETを混入させない。workerは新exact source文字列を許可するが、時刻・数値・query結果・modeの検証を維持する。検出エラー、検証失敗、CM生成/消失のraceではunknown/staleとなり、exclusiveへの再試行で迂回しない。背景worker経由の継続動作は新bundleの反映・検証で別途確認する。

## profile2 extended 5とschema2の証明

次のNP1-only profiles窓ではmetadataとprivateSave/readback、profile1の39TLV/nativeFieldsCompleteを確認した。profile2はQMI result81/extended5、同じCIDの3GPP inventoryはindices[1]だった。従来schemaではextended5単独をabsenceにしないため、当初のCLI結果は `STOCK_CM_PROFILE_READBACK_PARTIAL`。完全合格と記録しない。証拠は `tmp/cm-profiles-20261112-1/device`。

extended5は `INVALID_PROFILE_NUMBER`、1003/1004等とは別の値である。[公式libqmi列挙](https://github.com/linux-mobile-broadband/libqmi/blob/main/src/libqmi-glib/qmi-enums-wds.h) を確認した。単独errorの意味を緩和せず、次のすべてがそろう場合だけprofile2の不存在を認定する。

- 固定type0/index2 GETがresult81/extended5。
- 同じ独立CIDの固定GETでprofile1取得成功。
- 同じCIDで取得・TLV検証したinventoryが3GPPのindices[1]。
- 全GETを囲むCM/owner/FDの検証が成立。

新しいprivate schema2は2profileのraw responseと `sameCIDInventoryFrame` をroot-only0600 bundleへ併録する。再読時もraw responseのtransaction/message/TLV bounds/family/schemaを検証する。外部から渡したpublic inventory summaryを証拠にしない。schema1の既存backupも引き続き読み、schema1へのinventory混入・schema2の欠落/不正frameは拒否する。

public summaryへ `inventoryVerified` と `absenceSource` を追加した。ext5単独はfalseのまま、組合せ成功時は `profile-get-and-same-cid-inventory`。index2がinventoryに存在する矛盾やaccess error等はabsenceにしない。不存在はnative profile2の値を復元可能とする証拠ではなく、2profileのnative入力がcompleteになることもない。profile1-only適用はprofile2 flag0を維持する。

CM有時の `ReadGobiNetProfiles/Inventory` はこのstrict readerへ接続し、CMなしは従来exclusiveを維持した。schema2のprivate Save→Load、旧schema互換、矛盾/誤error/不正family/tx拒否、秘密情報・profile名非出力、profile2の捏造禁止をhostで合格。Go全tests/vetとARM64 compileも合格した。schema2を実機で新規Save/readbackする試験はまだ未実施であり、ユーザーが停止したstockサービスを勝手に起動して試験しない。
