#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT/frontend"
npm ci
npm run build
python3 "$ROOT/tools/prepare-full-native.py"
RECORDER_HOOK_SHA=$(python3 "$ROOT/tools/verify-recorder-artifacts.py" --field hookSha256)
RECORDER_INJECTOR_SHA=$(python3 "$ROOT/tools/verify-recorder-artifacts.py" --field injectorSha256)
RECORDER_STATUS_OFFSET=$(python3 "$ROOT/tools/verify-recorder-artifacts.py" --field statusOffset)
RECORDER_LDFLAGS="-s -w -X github.com/np1-control-center/ncc/internal/adapters.recorderHookLibrarySHA=$RECORDER_HOOK_SHA -X github.com/np1-control-center/ncc/internal/adapters.recorderHookInjectorSHA=$RECORDER_INJECTOR_SHA -X github.com/np1-control-center/ncc/internal/adapters.recorderHookStatusOffText=$RECORDER_STATUS_OFFSET"
cd "$ROOT/backend"
go test ./...
go vet ./...
mkdir -p "$ROOT/artifacts/ncc/bin" "$ROOT/artifacts/ncc/tools"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$RECORDER_LDFLAGS" -o "$ROOT/artifacts/ncc/bin/ncc" ./cmd/ncc
go version -m "$ROOT/artifacts/ncc/bin/ncc" > "$ROOT/artifacts/ncc/build-info.txt"
python3 "$ROOT/tools/verify-recorder-artifacts.py" --binary "$ROOT/artifacts/ncc/bin/ncc"
cp -R "$ROOT/systemd" "$ROOT/artifacts/ncc/"
cp "$ROOT/tools/device/"* "$ROOT/artifacts/ncc/tools/"
# A fresh NP1CC installation includes the pinned official Tailscale build.
# prepare-tailscale.py verifies the publisher-provided SHA-256 before staging it.
python3 "$ROOT/tools/prepare-tailscale.py"
mkdir -p "$ROOT/artifacts/ncc/vpn/bin"
cp "$ROOT/artifacts/ncc-vpn/bin/tailscale" "$ROOT/artifacts/ncc-vpn/bin/tailscaled" "$ROOT/artifacts/ncc/vpn/bin/"
cp "$ROOT/artifacts/ncc-vpn/source.json" "$ROOT/artifacts/ncc/vpn/source.json"
if test -f "$ROOT/artifacts/native-helper/ncc-native-setting-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/native-helper/ncc-native-setting-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/native-alexa-helper/ncc-native-alexa-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/native-alexa-helper/ncc-native-alexa-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/native-extended-helper/ncc-native-setting-extended-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/native-extended-helper/ncc-native-setting-extended-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/native-choice-helper/ncc-native-setting-choice-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/native-choice-helper/ncc-native-setting-choice-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/native-notification-helper/ncc-native-setting-notification-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/native-notification-helper/ncc-native-setting-notification-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/native-audio-helper/ncc-native-audio-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/native-audio-helper/ncc-native-audio-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/native-operations-helper/ncc-native-operations-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/native-operations-helper/ncc-native-operations-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/ncc-native-wifi-sta/bin/ncc-native-wifi-sta-helper"; then mkdir -p "$ROOT/artifacts/ncc/experimental"; cp "$ROOT/artifacts/ncc-native-wifi-sta/bin/ncc-native-wifi-sta-helper" "$ROOT/artifacts/ncc/experimental/"; fi
if test -f "$ROOT/artifacts/stock-start-helper/ncc-stock-start-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/stock-start-helper/ncc-stock-start-helper" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/recorder-hook/libncc-driverec-hook.so"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/recorder-hook/libncc-driverec-hook.so" "$ROOT/artifacts/ncc/experimental/"
fi
if test -f "$ROOT/artifacts/recorder-injector/ncc-recorder-injector"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/recorder-injector/ncc-recorder-injector" "$ROOT/artifacts/ncc/experimental/"
fi
# Plate recognition: models and the inference engine (freshly built if
# present, otherwise the checked-in prebuilt binary) with their notices.
mkdir -p "$ROOT/artifacts/ncc/plate/models" "$ROOT/artifacts/ncc/plate/licenses"
cp "$ROOT/models/plate/detector.tflite" \
    "$ROOT/models/plate/ocr.tflite" \
    "$ROOT/models/plate/ocr-kana.tflite" \
    "$ROOT/models/plate/ocr-classification.tflite" \
    "$ROOT/models/plate/ppocrv6_rec.onnx" \
    "$ROOT/models/plate/ppocrv6_rec_np1_int8.onnx" \
    "$ROOT/models/plate/ppocrv6_rec_np1_w8.mnn" \
    "$ROOT/models/plate/ppocrv5_r2_rec.onnx" \
    "$ROOT/models/plate/ppocrv5_r2-dictionary.json" \
    "$ROOT/models/plate/ppocrv5_r2_w128.tflite" \
    "$ROOT/models/plate/ppocrv5_r2_w256.tflite" \
    "$ROOT/models/plate/plate-model.json" \
    "$ROOT/models/plate/ppocr-dictionary.json" \
    "$ROOT/models/plate/model-manifest.json" \
    "$ROOT/models/plate/README.md" \
    "$ROOT/models/plate/LICENSING.md" \
    "$ROOT/models/plate/ONNX_RUNTIME_LICENSE.txt" \
    "$ROOT/models/plate/PPOCR_MODEL_CARD.md" \
    "$ROOT/artifacts/ncc/plate/models/"
cp -R "$ROOT/models/plate/notices" "$ROOT/artifacts/ncc/plate/models/"
if test -f "$ROOT/artifacts/plate/plate-engine-aarch64"; then
    cp "$ROOT/artifacts/plate/plate-engine-aarch64" "$ROOT/artifacts/ncc/plate/plate-engine"
    cp "$ROOT/artifacts/plate/libonnxruntime.so.1.30.0" "$ROOT/artifacts/ncc/plate/libonnxruntime.so.1.30.0"
else
    cp "$ROOT/prebuilt/plate-engine/plate-engine-aarch64" "$ROOT/artifacts/ncc/plate/plate-engine"
    cp "$ROOT/prebuilt/plate-engine/libonnxruntime.so.1.30.0" "$ROOT/artifacts/ncc/plate/libonnxruntime.so.1.30.0"
fi
cp "$ROOT/prebuilt/plate-engine/licenses/"* "$ROOT/artifacts/ncc/plate/licenses/"
python3 "$ROOT/tools/sbom.py"
python3 "$ROOT/tools/third-party-licenses.py"
cp "$ROOT/LICENSE" "$ROOT/artifacts/ncc/LICENSE"
cp "$ROOT/THIRD_PARTY_LICENSES.md" "$ROOT/artifacts/ncc/THIRD_PARTY_LICENSES.md"
cp -R "$ROOT/licenses" "$ROOT/artifacts/ncc/"
python3 "$ROOT/tools/package.py" "$ROOT/artifacts/ncc"
python3 "$ROOT/tools/package-recorder-hook-update.py"
