# 電波指標の集計ライブラリ

`backend/internal/radio`は実機収集から独立した派生集計。UIは実機観測がないため空表示であり、テスト点を本番へ流さない。

## セル定義

球面円筒等積投影を使用し、R=6,371,008.8m、標準緯線30°とする。

```text
x = R * cos(30°) * lon_radians
y = R * sin(lat_radians) / cos(30°)
cell_x = floor(x / cell_size)
cell_y = floor(y / cell_size)
```

cell_sizeは20/50/100m。ズームから独立し、同じ座標は同じセルに入る。
これは楕円体EPSG:6933ではなく球面近似。緯度ごとの距離歪みを含み、測量用途ではない。

## 収集APIに接続する前の条件

- sourceが確定したdBm/dB観測だけを渡す。RSRP/SINR/RSRQは別呼出し・別凡例。
- 呼出し側で期間・事業者/SIM・RAT・bandを分離し、混在させない。現状は収集・絞込API未実装。
- GPS時刻との5秒超の差、測位精度未取得/0または50m超、無効値を除外する。
- 無登録と値未取得を分離する。無登録に0dBmを割り当てない。
- trip代表値モードでは各tripの中央値から統計を計算し、長い停車の観測過密を抑制する。
- Countは元の有効観測数、Tripsは異なるtrip数。統計値は選択した集計モードの母数に対する値。
- p10/p90はソート後の線形補間。3観測未満はLowConfidence。

無線値の仮の有効範囲ガードは実機モデムのscale/invalid値仕様が確定後に合わせる必要がある。
描画・coverage秒・bbox/limit/cancel・派生index・原始SDログの保持は今後の収集実装に含める。
