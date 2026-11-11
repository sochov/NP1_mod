# 前方プレビュー503の切り分け

2026-11-11の実機読取り。個体情報・映像・raw/hash・生ログはignored `tmp/camera-export-evidence/` / `tmp/camera-export-analysis/` のみに置く。純正サービスの停止・再起動、カメラの再生成、stock設定の変更はこの調査では行っていない。

## 確認した範囲

- 前方JPEGだけ503。車内JPEGは200で取得できる。
- 前方exportの最終mtimeは11:43:05 UTC。12:54および12:57 UTCに各3回、1秒以上間隔で再openした。inode・mtime・NV12本体・Y・UV・先頭128byte・末尾producer timespecが変化しなかった。
- 車内exportは同じ観測でinode・mtime・Y・UV・producer timespecが変化した。
- ImageRecog journalには11:43:06 UTCから毎秒 `CIMGN_SRV_IMGR_ImageDataProxyFront get no image` がある。前方購読の待機loopは動いている一方、bufferを得られていない可能性が高い。
- 純正ImageRecog / DriveRec / qmmf-serverはactiveで、ドラレコcatalogは前方・車内とも12:57:06 UTCまで録画を示す。前方センサーや録画全体の停止とは断定できず、ImageRecogのfront購読/export経路に絞って調べる。

ファイル時刻だけから停止と判断しない。静止画でもproducer timespecやinodeが進めばproducerは動いているし、in-place mmapでは画素だけ変わる可能性がある。今回のfrontはこれらすべてが不変だった。

## Writerの静的根拠

`ImageRecog` SHA-256 `02a37de0a0a246bd90612e8f4fea77d20aa9556b18259ffc1f9babd9805b4f47` を対象とする。

- 前方loop `0xa5800` はNPVIN bufferを1秒待機し、front有効flagを確認して `0xaa598` を呼ぶ。後者は `NPVINCaptureBuffer::GetVirtualAddress` → `CIMGN_DataServIF::write("imagedata", data, size, true, NULL)` を呼ぶ。
- `libPIMG_SharedLib.so` の `CIMGN_DataServIF::write` (`0x63b8`) はmkstemp (`0x64b4`)、page単位への丸め、mmap、frame memcpy (`0x6594`)、末尾16byteのtimespec、munmap、rename (`0x65dc`) で既存exportを置き換える。データはmmapするが、公開済み同一inodeを更新し続ける経路ではない。
- frameは1920×1080 NV12で3,110,400 byte。直後16byteはproducerのtimespecで、ファイル全体3,112,960 byteはpage4096への丸めに一致する。先頭128byteは画像であり制御headerではない。
- ImageRecog `0xa3a40` は純正CDVRイベント `0x80030f04/05/06` を受けてfront有効flagを切り替える (`0xa6058` / `0xa6110`)。この入口やNPVIN Start/Stopを独立helperから未検証呼出ししない。

既存Webの15秒stale検査は今回の古いfront出力を正しく拒否している。古いフレームをliveと見せるためにguardを削除しない。

## 時刻関係と限界

以前のhardware smokeは11:39:34 UTCに前方・車内JPEG合格を記録している。旧native50/51試験の取得済み記録は11:25:33 / 11:38:05 UTC、前方最終exportは11:43:05 UTC。追加9設定のtrial開始は12:33 UTC以降だった。

取得ファイルのhost mtimeは試験実行時刻そのものを保証しない。停止に近い時刻の操作があったことだけで原因を断定しない。追加9設定の前から止まっていたことと、50/51等が原因ではないことは別の主張であり、後者は未確定。

## 再現手順

```sh
python tools/diagnose-camera-export.py --serial <ADB_SERIAL>
python -m unittest discover -s tests -p test_camera_export_diagnostic.py -v
```

診断は固定2exportのstat/pullを3回行うだけで、デバイス側への書込みはしない。stdoutは比較booleanと取得時刻のみ。raw/hash/metadataはhostのignored tmpに保存する。mtime固定で画素が変わるケース、静止画でもproducer timestampが変わるケース、不完全pull、単一sampleの判定不能をunit testで検証済み。

復旧は純正所有者の状態を保持する方法を優先し、独立QMMFsessionやencoderを作成しない。再起動による復旧を行う場合は、担当者間でwrite windowを設定し、診断・前方/車内JPEG・直近MP4を前後で再取得する。
