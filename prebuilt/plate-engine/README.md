# ナンバー認識エンジン（ビルド済み、aarch64）

`plate-engine-aarch64` は、本体の認識プロセス（`np1-control-plate.service`）が子プロセスとして起動する推論エンジンです。ソースは [`tools/plate/engine/plate_engine.cc`](../../tools/plate/engine/plate_engine.cc) です。TensorFlow Lite 2.16.1、XNNPACK、GPU delegate、MNN 3.2.5（OpenCL・ARMv8.2 対応）を静的にリンクしています。TFLite の検出・読取りと PP-OCRv6 の Adreno 並列バッチ推論に本体の OpenCL を使い、PP-OCRv6 の CPU 経路およびフォールバックには同梱の ONNX Runtime 1.30.0 AArch64 を使います。

| 項目 | 値 |
| --- | --- |
| エンジンSHA-256 | `45aa1bdc5c3dba49c5eab34fe3d451b2b5ab28804ea463da7b93c01c3b56e326` |
| ONNX Runtime SHA-256 | `64e903a43a041240fd6bcffe0ac6d4fea47ef87bf24b9d097801bd00a9612a4b` |
| 対象 | AArch64 Linux、glibc 2.28以降（NP1 202404231204のrootfs） |
| ビルド | `bash tools/plate/build_engine.sh aarch64`（WSL / Linux x86_64、Apple Silicon macOS、`unzip` が必要）。zig 0.13.0、TensorFlow v2.16.1、MNN 3.2.5、nlohmann/json 3.11.3、ONNX Runtime 1.30.0 NuGetを固定して取得します |
| リンク | libc++・TensorFlow Lite・XNNPACK・MNNなどは静的、glibcと同梱ONNX Runtimeは動的 |

ソースを変更した場合は、ビルド後に `artifacts/plate/plate-engine-aarch64` と `artifacts/plate/libonnxruntime.so.1.30.0` をこのディレクトリへコピーし、上のSHA-256を更新してください。`tools/build.*` は `artifacts/plate/` に両方あればそれを、無ければこのビルド済み組を配布物に入れます。

aarch64版は常にGPU対応でビルドします。Apple Silicon macOSでは `build-aarch64-gpu` を使用します。初回はOpenCL・Vulkan・OpenGL・EGLのヘッダーの取得が必要です。OpenCLライブラリは実行時に読み込みます。

## 同梱しているOSS

エンジンに静的リンクしたOSSのライセンス原文を [`licenses/`](licenses) に保存しています。

| コンポーネント | ライセンス | 原文 |
| --- | --- | --- |
| TensorFlow Lite 2.16.1 | Apache-2.0 | `tensorflow-LICENSE` |
| MNN 3.2.5 | Apache-2.0 | `mnn-LICENSE` |
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
| Khronos OpenCL・Vulkan・OpenGL ES・EGLヘッダー | Apache-2.0 / MIT | `opencl-headers-LICENSE`、`vulkan-headers-LICENSE.txt`、`opengl-headers-LICENSE.txt`、`egl-headers-LICENSE.txt`、`khronos-headers-MIT.txt` |
| nlohmann/json 3.11.3 | MIT | `nlohmann-json-LICENSE.MIT` |
| LLVM libc++ / libc++abi / libunwind（zig 0.13.0同梱） | Apache-2.0 WITH LLVM-exception | `libcxx-LICENSE.txt` ほか |
| ONNX Runtime 1.30.0 | MIT | `../../models/plate/ONNX_RUNTIME_LICENSE.txt` |

Eigen（MPL-2.0）は改変していません。ソースは上流の該当バージョン（TensorFlow v2.16.1が固定する版）から入手できます。
