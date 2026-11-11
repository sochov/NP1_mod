# LED制御の追加

- 所有者指定のnp1-led-gaming-modeを参考に、Goで同じ5ゾーンHSV位相・gamma1.8・既定15fps/240°毎秒/輝度180を実装。
- 専用root LED unitだけがled_mode/status/valueへwriteする。Web/brokerは任意sysfsパスを受け付けない。
- モードはoff/solid/rainbow、RGB0..255、輝度1..255、fps1..18、速度非ゼロ±360内に制限。別の所有者LED serviceがactiveなら拒否。
- configはroot専用private/ledへatomic保存。revision検査、worker命令確認、失敗時に直前設定へ戻す。
- 再起動後点灯は明示チェック時だけ。postbootは専用marker、既定OFF。uninstallは自分のworkerだけ停止。
- 停止は参考実装と同じく5ゾーン消灯。読み戻しがEIOのため、元の純正modeを推測して復元しない。
- 成功表示はworkerがsysfs書込みを受理した命令状態。物理発光のread-backは非対応と明示する。
- 小規模実機試験は3秒のrainbow後off、純正主要サービスの前後比較、service/命令状態を確認する。
