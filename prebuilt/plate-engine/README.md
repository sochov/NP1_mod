# ナンバー認識エンジン（ビルド済み、aarch64）

`plate-engine-aarch64` は、本体の認識プロセス（`np1-control-plate.service`）が子プロセスとして起動する推論エンジンです。ソースは [`tools/plate/engine/plate_engine.cc`](../../tools/plate/engine/plate_engine.cc) です。本体内蔵のTensorFlow Liteは古く、現在のモデルを扱えません。このため、TensorFlow Lite 2.16.1とXNNPACKをソースからビルドして静的にリンクしています。

| 項目 | 値 |
| --- | --- |
| SHA-256 | `883ca2f540c0e3c204c88b5c6f354732923fdd82a56af629ddf2ac420b8a4c8b` |
| 対象 | AArch64 Linux、glibc 2.28以降（NP1 202404231204のrootfs） |
| ビルド | `bash tools/plate/build_engine.sh aarch64`（WSL / Linux x86_64）。zig 0.13.0・TensorFlow v2.16.1・nlohmann/json 3.11.3をバージョンとSHA-256で固定して取得します |
| リンク | libc++・TensorFlow Lite・XNNPACKなどは静的、glibcは動的 |

同じソースと手順から、同じSHA-256のバイナリが得られることを確認しています。ソースを変更した場合は、ビルド後に `artifacts/plate/plate-engine-aarch64` をこのディレクトリへコピーし、上のSHA-256を更新してください。`tools/build.*` は `artifacts/plate/plate-engine-aarch64` があればそれを、無ければこのビルド済みバイナリを配布物に入れます。

## 同梱しているOSS

エンジンに静的リンクしたOSSのライセンス原文を [`licenses/`](licenses) に保存しています。

| コンポーネント | ライセンス | 原文 |
| --- | --- | --- |
| TensorFlow Lite 2.16.1 | Apache-2.0 | `tensorflow-LICENSE` |
| XNNPACK | BSD-3-Clause | `xnnpack-LICENSE` |
| ruy | Apache-2.0 | `ruy-LICENSE` |
| gemmlowp | Apache-2.0 | `gemmlowp-LICENSE` |
| FlatBuffers | Apache-2.0 | `flatbuffers-LICENSE` |
| Abseil | Apache-2.0 | `abseil-cpp-LICENSE` |
| Eigen | MPL-2.0（一部BSD・Apache・MINPACK） | `eigen-COPYING.*` |
| ml_dtypes | Apache-2.0 | `ml_dtypes-LICENSE` |
| farmhash | MIT | `farmhash-COPYING` |
| fft2d | 独自の許諾（README記載） | `fft2d-readme.txt` |
| cpuinfo / pthreadpool / FP16 / FXdiv / psimd | BSD-2-Clause / BSD-2-Clause / MIT / MIT / MIT | 各`*-LICENSE` |
| nlohmann/json 3.11.3 | MIT | `nlohmann-json-LICENSE.MIT` |
| LLVM libc++ / libc++abi / libunwind（zig 0.13.0同梱） | Apache-2.0 WITH LLVM-exception | `libcxx-LICENSE.txt` ほか |

Eigen（MPL-2.0）は改変していません。ソースは上流の該当バージョン（TensorFlow v2.16.1が固定する版）から入手できます。
