#!/usr/bin/env bash
# Build the NP1 plate-recognition engine (run inside WSL/Linux x86_64, or on an Apple-silicon Mac for the
# aarch64 target).
#
#   bash tools/plate/build_engine.sh [aarch64|host|all]
#
# aarch64: device binary, zig cc targeting glibc 2.28 (the NP1 rootfs), libc++
#          linked statically, glibc dynamic. TFLite + XNNPACK built from source.
# host   : x86_64 binary built with the system compiler, for PC-side tests.
#
# Downloads are pinned by version and SHA-256. Work files live outside the
# repository (PLATE_BUILD_DIR, default ~/np1-plate-build); outputs go to
# artifacts/plate/ (ignored by Git).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK=${PLATE_BUILD_DIR:-$HOME/np1-plate-build}
TARGETS=${1:-all}
HOST_OS=$(uname -s)
if [ "$HOST_OS" = Darwin ]; then
  JOBS=${JOBS:-$(sysctl -n hw.ncpu)}
  sha256sum() { shasum -a 256 "$@"; }
else
  JOBS=${JOBS:-$(nproc)}
fi

ZIG_VERSION=0.13.0
ZIG_SHA256=d45312e61ebcc48032b77bc4cf7fd6915c11fa16e4aad116b66c9468211230ea
TF_TAG=v2.16.1
MNN_TAG=3.2.5
JSON_VERSION=3.11.3
JSON_SHA256=9bea4c8066ef4a1c206b2be5a36302f8926f7fdc6087af5d20b417d0cf103ea6
ORT_VERSION=1.30.0
ORT_PACKAGE_SHA256=30df119bd9164eaa187666d93147448fdd4a9a1a3caf83f1505daf7af2d1c5d2
CMAKE_VERSION=3.31.10
CMAKE_SHA256=3cb3dd247b6a1de2d0f4b20c6fd4326c9024e894cebc9dc8699758887e566ca7
ZIG_MACOS_SHA256=46fae219656545dfaf4dce12fb4e8685cec5b51d721beee9389ab4194d43394c
CMAKE_MACOS_SHA256=be9f3faeeaf7921cc2d77cea711dd5e6f72c63af2810cacd9205b3ce8d1593c9

mkdir -p "$WORK" "$ROOT/artifacts/plate"
cd "$WORK"

fetch() { # url sha256 output
  if [ ! -f "$3" ]; then curl -fsSL --retry 3 -o "$3.part" "$1" && mv "$3.part" "$3"; fi
  echo "$2  $3" | sha256sum -c --quiet - || { echo "checksum mismatch: $3" >&2; rm -f "$3"; exit 1; }
}

fetch "https://github.com/nlohmann/json/releases/download/v$JSON_VERSION/json.hpp" "$JSON_SHA256" json.hpp
mkdir -p include/nlohmann && cp json.hpp include/nlohmann/json.hpp

if [ "$HOST_OS" = Darwin ]; then
  fetch "https://github.com/Kitware/CMake/releases/download/v$CMAKE_VERSION/cmake-$CMAKE_VERSION-macos-universal.tar.gz" \
    "$CMAKE_MACOS_SHA256" "cmake-$CMAKE_VERSION-macos-universal.tar.gz"
  if [ ! -d "cmake-$CMAKE_VERSION-macos-universal" ]; then tar -xzf "cmake-$CMAKE_VERSION-macos-universal.tar.gz"; fi
  export PATH="$WORK/cmake-$CMAKE_VERSION-macos-universal/CMake.app/Contents/bin:$PATH"
else
  fetch "https://github.com/Kitware/CMake/releases/download/v$CMAKE_VERSION/cmake-$CMAKE_VERSION-linux-x86_64.tar.gz" \
    "$CMAKE_SHA256" "cmake-$CMAKE_VERSION-linux-x86_64.tar.gz"
  if [ ! -x "cmake-$CMAKE_VERSION-linux-x86_64/bin/cmake" ]; then
    tar -xzf "cmake-$CMAKE_VERSION-linux-x86_64.tar.gz"
  fi
  export PATH="$WORK/cmake-$CMAKE_VERSION-linux-x86_64/bin:$PATH"
fi

prepare_ort() {
  local arch=$1 archive="onnxruntime-$ORT_VERSION.nupkg"
  local directory="onnxruntime-linux-$1-$ORT_VERSION"
  local id=microsoft.ml.onnxruntime runtime_arch=$1
  if [ "$arch" = aarch64 ]; then runtime_arch=arm64; fi
  fetch "https://api.nuget.org/v3-flatcontainer/$id/$ORT_VERSION/$id.$ORT_VERSION.nupkg" "$ORT_PACKAGE_SHA256" "$archive" || return
  mkdir -p "$directory/include" "$directory/lib"
  unzip -jo "$archive" 'build/native/include/*' -d "$directory/include" >&2 || return
  local library="$directory/lib/libonnxruntime.so.$ORT_VERSION"
  unzip -p "$archive" "runtimes/linux-$runtime_arch/native/libonnxruntime.so" > "$library" || return
  ln -sfn "libonnxruntime.so.$ORT_VERSION" "$directory/lib/libonnxruntime.so"
  ln -sfn "libonnxruntime.so.$ORT_VERSION" "$directory/lib/libonnxruntime.so.1"
  printf '%s' "$WORK/$directory"
}

if [ ! -d tensorflow/.git ]; then
  git clone --quiet --depth 1 --branch "$TF_TAG" https://github.com/tensorflow/tensorflow.git tensorflow
fi
[ "$(git -C tensorflow describe --tags --exact-match)" = "$TF_TAG" ] || { echo "unexpected TensorFlow checkout" >&2; exit 1; }

# Host flatc is required when cross-compiling TFLite.
if [ ! -x host-tools/bin/flatc ]; then
  cmake -S tensorflow/tensorflow/lite/tools/cmake/native_tools/flatbuffers -B build-flatc -DCMAKE_BUILD_TYPE=Release >/dev/null
  cmake --build build-flatc -j "$JOBS" >/dev/null
  cmake --install build-flatc --prefix host-tools >/dev/null
fi

common_flags=(
  -DCMAKE_BUILD_TYPE=Release
  -DCMAKE_POLICY_VERSION_MINIMUM=3.5
  -DTFLITE_SOURCE_DIR="$WORK/tensorflow"
  -DJSON_INCLUDE_DIR="$WORK/include"
  -DTFLITE_VERSION_LABEL="${TF_TAG#v}"
  -DTFLITE_ENABLE_XNNPACK=ON
  -DTFLITE_ENABLE_NNAPI=OFF
  -DTFLITE_ENABLE_RUY=ON
  # zig's integrated assembler rejects some fp16 .S microkernels; the C
  # intrinsic kernels are used instead (runtime CPU dispatch is unchanged).
  -DXNNPACK_ENABLE_ASSEMBLY=OFF
  -DTFLITE_HOST_TOOLS_DIR="$WORK/host-tools/bin"
)

build_host() {
  local ort_root
  ort_root=$(prepare_ort x64)
  local build_dir=${PLATE_HOST_BUILD_DIR:-build-host}
  cmake -S "$ROOT/tools/plate/engine" -B "$build_dir" "${common_flags[@]}" \
    -DTFLITE_ENABLE_GPU=OFF -DORT_ROOT="$ort_root" >/dev/null
  cmake --build "$build_dir" --target plate-engine -j "$JOBS"
  install -m 0755 "$build_dir/plate-engine" "$ROOT/artifacts/plate/plate-engine-host"
  install -m 0755 "$ort_root/lib/libonnxruntime.so.$ORT_VERSION" "$ROOT/artifacts/plate/libonnxruntime-host.so.$ORT_VERSION"
}

build_aarch64() {
  local ort_root
  ort_root=$(prepare_ort aarch64)
  local zig_name=zig-linux-x86_64-$ZIG_VERSION zig_sha=$ZIG_SHA256
  if [ "$HOST_OS" = Darwin ]; then zig_name=zig-macos-aarch64-$ZIG_VERSION; zig_sha=$ZIG_MACOS_SHA256; fi
  if [ ! -x "$zig_name/zig" ]; then
    fetch "https://ziglang.org/download/$ZIG_VERSION/$zig_name.tar.xz" "$zig_sha" "$zig_name.tar.xz"
    tar -xJf "$zig_name.tar.xz"
  fi
  ZIG="$WORK/$zig_name/zig"
  mkdir -p bin
  # zig cc treats -march=armv8.2-a+fp16 (used by XNNPACK microkernels) as an
  # unknown CPU; translate it to zig's -mcpu=generic+v8_2a+fullfp16 form.
  for tool in cc c++; do
    cat > "bin/zig-$tool" <<EOF
#!/usr/bin/env bash
args=()
has_s=0
has_fp16=0
for a in "\$@"; do
  if [[ \$a == *.S ]] || [[ \$a == *.s ]]; then has_s=1; fi
  if [[ \$a == -march=armv8* ]]; then
    spec=\${a#-march=armv}; base=\${spec%%+*}; ext=""
    [[ \$spec == *+* ]] && ext=+\${spec#*+}
    ver=\${base%-a}; ver=v\${ver/./_}a
    [[ \$ext == *fp16* ]] && has_fp16=1
    ext=\${ext//+fp16/+fullfp16}; ext=\${ext//+fullfp16fml/+fp16fml}
    args+=("-mcpu=generic+\$ver\$ext")
  else
    args+=("\$a")
  fi
done
if [ "\$has_s" = "1" ] && [ "\$has_fp16" = "1" ]; then
  args+=("-Wa,-march=armv8.2-a+fp16+dotprod")
fi
exec "$ZIG" $tool -target aarch64-linux-gnu.2.28 "\${args[@]}"
EOF
  done
  for tool in ar ranlib; do
    printf '#!/bin/sh\nexec "%s" %s "$@"\n' "$ZIG" "$tool" > "bin/zig-$tool"
  done
  chmod +x bin/zig-*
  cat > aarch64.cmake <<EOF
set(CMAKE_SYSTEM_NAME Linux)
set(CMAKE_SYSTEM_PROCESSOR aarch64)
set(CMAKE_C_COMPILER "$WORK/bin/zig-cc")
set(CMAKE_CXX_COMPILER "$WORK/bin/zig-c++")
set(CMAKE_ASM_COMPILER "$WORK/bin/zig-cc")
set(CMAKE_AR "$WORK/bin/zig-ar")
set(CMAKE_RANLIB "$WORK/bin/zig-ranlib")
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
EOF
  export ZIG_GLOBAL_CACHE_DIR="$WORK/zig-cache" ZIG_LOCAL_CACHE_DIR="$WORK/zig-cache"
  if [ ! -d MNN/.git ]; then
    git clone --quiet --depth 1 --branch "$MNN_TAG" https://github.com/alibaba/MNN.git MNN
  fi
  [ "$(git -C MNN describe --tags --exact-match)" = "$MNN_TAG" ] || { echo "unexpected MNN checkout" >&2; exit 1; }
  if [ ! -f build-mnn-aarch64/libMNN.a ]; then
    cmake -S MNN -B build-mnn-aarch64 -DCMAKE_TOOLCHAIN_FILE="$WORK/aarch64.cmake" \
      -DCMAKE_BUILD_TYPE=Release \
      -DMNN_BUILD_SHARED_LIBS=OFF \
      -DMNN_OPENCL=ON \
      -DMNN_ARM82=ON \
      -DMNN_LOW_MEMORY=ON \
      -DMNN_KLEIDIAI=OFF \
      -DMNN_SUPPORT_TRANSFORMER_FUSE=ON \
      -DMNN_USE_THREAD_POOL=ON \
      -DMNN_BUILD_TOOLS=OFF \
      -DMNN_BUILD_CONVERTER=OFF \
      -DMNN_BUILD_BENCHMARK=OFF \
      -DMNN_SEP_BUILD=OFF >/dev/null
    cmake --build build-mnn-aarch64 --target MNN -j "$JOBS" >/dev/null
  fi
  local build_dir=${PLATE_AARCH64_BUILD_DIR:-build-aarch64}
  if [ "$HOST_OS" = Darwin ]; then build_dir=${PLATE_AARCH64_BUILD_DIR:-build-aarch64-gpu}; fi
  cmake -S "$ROOT/tools/plate/engine" -B "$build_dir" -DCMAKE_TOOLCHAIN_FILE="$WORK/aarch64.cmake" \
    "${common_flags[@]}" -DTFLITE_ENABLE_GPU=ON -DORT_ROOT="$ort_root" \
    -DMNN_INCLUDE_DIR="$WORK/MNN/include" -DMNN_LIBRARY="$WORK/build-mnn-aarch64/libMNN.a" >/dev/null
  cmake --build "$build_dir" --target plate-engine -j "$JOBS"
  install -m 0755 "$build_dir"/plate-engine "$ROOT/artifacts/plate/plate-engine-aarch64"
  install -m 0755 "$ort_root/lib/libonnxruntime.so.$ORT_VERSION" "$ROOT/artifacts/plate/libonnxruntime.so.$ORT_VERSION"
}

if [ "$HOST_OS" = Darwin ] && [ "$TARGETS" != aarch64 ]; then
  echo "on macOS only the aarch64 target is supported" >&2; exit 2
fi
case "$TARGETS" in
  host) build_host ;;
  aarch64) build_aarch64 ;;
  all) build_host; build_aarch64 ;;
  *) echo "usage: $0 [aarch64|host|all]" >&2; exit 2 ;;
esac
sha256sum "$ROOT"/artifacts/plate/plate-engine-* "$ROOT"/artifacts/plate/libonnxruntime*.so.*
