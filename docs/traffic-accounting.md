# 4G・Wi-Fiの通信量と速度

通信量ストアはraw IF累計の既存契約を保ったまま、`usage.data.networks.cellular/wifi` を追加する。collectorは読取りで検証したexact IFだけを `FeedNetworks` へ渡す。確認済みNP1ではcellular=親usb0、Wi-Fi STA=wlan0。usb0.1/usb0.2、wlan1/AP、bridge0、p2p、Tailscaleを合算しない。同じIFを両分類へ指定した場合も計上しない。Wi-Fi値はSTAインターフェースの通信量で、管理画面等のLAN通信を含み、インターネット請求量だけを抽出した値ではない。

`Feed(at,boot,items)` と `Summary(iface,now)` の既存利用は継続可能。ネットワーク分類なしの旧データを、現在のIF名や推定で4G/Wi-Fi別の過去統計へ振り分けない。新分類の初回はbaselineとして記録し、以後の差分を保存する。

各networkの今日/月/保持期間累計RX・TXは、精度を失わない10進文字列。計測区間のない期間はnull、実測静止区間の0は文字列`"0"`。速度は受信/送信counterの差分を実際の採取時刻差で割ったbytes/sであり、直近2点の平均速度。初回・IF変更・再起動・IF reset・不正counter・boot ID不明ではnullにし、reset後の初counterを推定で新分類累計へ加算しない。真のuint64 wrapは差分補正する。

有効2点の間隔が10秒を超えた場合、同bootの単調counter差分は実測累計へ含めるが、現在速度には使わない。最終有効採取から6秒超は現在速度をnullへ戻す。collectorの2秒周期を前提としており、UIもその更新に追従させる。欠測/非選択IFの復帰はbaselineから再開する。逆順・同時刻sampleは状態を変更しない。

日付はJST。採取区間の差分は終点の採取日に計上する。日/月境界をまたぐ短い区間の転送時刻を推定分割しないため、境界直前の最大1採取区間が翌日/月に含まれうる。正常時は2秒程度、collector停止等があればこの誤差は広がる。請求期間の厳密な上限管理ではなく、収集済み量からの残量目安とする。

各networkには `observedSince`、`sampledAt`、`todayObservedSeconds`、`monthObservedSeconds`、`status`、`speedStatus`、`stale`、`partial:true`、`scope` を付ける。初回導入より前・collector停止・reset時に未取得の量は復元しない。保持は90日で、月初から完全に収集できたことや通信会社との一致を主張しない。4G月上限は設定側の `cellularLimitBytes` に保存し、この観測済み月累計と比較する。

検証: 既存traffic persistence/reset/wrapテスト、新規4G/Wi-Fi分離、子IF/AP/bridge/Tailscale増加の除外、0とnull、2秒/4秒の実時間差、6秒stale、10秒超gap、再起動・reset、boot不明、IF非選択・重複役割、旧保存形式読込、JST月境界、永続化後の継続をGo test/vetで合格。device/collector/UIは担当外で変更していない。
