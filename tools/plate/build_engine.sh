#!/usr/bin/env bash
# Build the NP1 plate-recognition engine (run inside WSL/Linux x86_64).
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
JOBS=${JOBS:-$(nproc)}

ZIG_VERSION=0.13.0
ZIG_SHA256=d45312e61ebcc48032b77bc4cf7fd6915c11fa16e4aad116b66c9468211230ea
TF_TAG=v2.16.1
JSON_VERSION=3.11.3
JSON_SHA256=9bea4c8066ef4a1c206b2be5a36302f8926f7fdc6087af5d20b417d0cf103ea6

mkdir -p "$WORK" "$ROOT/artifacts/plate"
cd "$WORK"

fetch() { # url sha256 output
  if [ ! -f "$3" ]; then curl -fsSL --retry 3 -o "$3.part" "$1" && mv "$3.part" "$3"; fi
  echo "$2  $3" | sha256sum -c --quiet - || { echo "checksum mismatch: $3" >&2; rm -f "$3"; exit 1; }
}

fetch "https://github.com/nlohmann/json/releases/download/v$JSON_VERSION/json.hpp" "$JSON_SHA256" json.hpp
mkdir -p include/nlohmann && cp json.hpp include/nlohmann/json.hpp

if [ ! -d tensorflow/.git ]; then
  git clone --quiet --depth 1 --branch "$TF_TAG" https://github.com/tensorflow/tensorflow.git tensorflow
fi
[ "$(git -C tensorflow describe --tags --exact-match)" = "$TF_TAG" ] || { echo "unexpected TensorFlow checkout" >&2; exit 1; }

# Host flatc is required when cross-compiling TFLite.
if [ ! -x host-tools/flatbuffers-flatc/bin/flatc ]; then
  cmake -S tensorflow/tensorflow/lite/tools/cmake/native_tools/flatbuffers -B build-flatc -DCMAKE_BUILD_TYPE=Release >/dev/null
  cmake --build build-flatc -j "$JOBS" >/dev/null
  cmake --install build-flatc --prefix host-tools >/dev/null
fi

common_flags=(
  -DCMAKE_BUILD_TYPE=Release
  -DTFLITE_SOURCE_DIR="$WORK/tensorflow"
  -DJSON_INCLUDE_DIR="$WORK/include"
  -DTFLITE_VERSION_LABEL="${TF_TAG#v}"
  -DTFLITE_ENABLE_XNNPACK=ON
  -DTFLITE_ENABLE_GPU=OFF
  -DTFLITE_ENABLE_NNAPI=OFF
  -DTFLITE_ENABLE_RUY=ON
  # zig's integrated assembler rejects some fp16 .S microkernels; the C
  # intrinsic kernels are used instead (runtime CPU dispatch is unchanged).
  -DXNNPACK_ENABLE_ASSEMBLY=OFF
  -DTFLITE_HOST_TOOLS_DIR="$WORK/host-tools/bin"
)

build_host() {
  cmake -S "$ROOT/tools/plate/engine" -B build-host "${common_flags[@]}" >/dev/null
  cmake --build build-host --target plate-engine -j "$JOBS"
  install -m 0755 build-host/plate-engine "$ROOT/artifacts/plate/plate-engine-host"
}

build_aarch64() {
  if [ ! -x "zig-linux-x86_64-$ZIG_VERSION/zig" ]; then
    fetch "https://ziglang.org/download/$ZIG_VERSION/zig-linux-x86_64-$ZIG_VERSION.tar.xz" "$ZIG_SHA256" zig.tar.xz
    tar -xJf zig.tar.xz
  fi
  ZIG="$WORK/zig-linux-x86_64-$ZIG_VERSION/zig"
  mkdir -p bin
  # zig cc treats -march=armv8.2-a+fp16 (used by XNNPACK microkernels) as an
  # unknown CPU; translate it to zig's -mcpu=generic+v8_2a+fullfp16 form.
  for tool in cc c++; do
    cat > "bin/zig-$tool" <<EOF
#!/usr/bin/env bash
args=()
for a in "\$@"; do
  if [[ \$a == -march=armv8* ]]; then
    spec=\${a#-march=armv}; base=\${spec%%+*}; ext=""
    [[ \$spec == *+* ]] && ext=+\${spec#*+}
    ver=\${base%-a}; ver=v\${ver/./_}a
    ext=\${ext//+fp16fml/+FP16FML}; ext=\${ext//+fp16/+fullfp16}; ext=\${ext//+FP16FML/+fp16fml}
    args+=("-mcpu=generic+\$ver\$ext")
  else
    args+=("\$a")
  fi
done
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
set(CMAKE_AR "$WORK/bin/zig-ar")
set(CMAKE_RANLIB "$WORK/bin/zig-ranlib")
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
EOF
  export ZIG_GLOBAL_CACHE_DIR="$WORK/zig-cache" ZIG_LOCAL_CACHE_DIR="$WORK/zig-cache"
  cmake -S "$ROOT/tools/plate/engine" -B build-aarch64 -DCMAKE_TOOLCHAIN_FILE="$WORK/aarch64.cmake" "${common_flags[@]}" >/dev/null
  cmake --build build-aarch64 --target plate-engine -j "$JOBS"
  "$ZIG" objcopy --strip-all build-aarch64/plate-engine "$ROOT/artifacts/plate/plate-engine-aarch64" 2>/dev/null \
    || cp build-aarch64/plate-engine "$ROOT/artifacts/plate/plate-engine-aarch64"
  chmod 0755 "$ROOT/artifacts/plate/plate-engine-aarch64"
}

case "$TARGETS" in
  host) build_host ;;
  aarch64) build_aarch64 ;;
  all) build_host; build_aarch64 ;;
  *) echo "usage: $0 [aarch64|host|all]" >&2; exit 2 ;;
esac
sha256sum "$ROOT"/artifacts/plate/plate-engine-*
