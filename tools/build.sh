#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT/frontend"
npm ci
npm run build
cd "$ROOT/backend"
go test ./...
go vet ./...
mkdir -p "$ROOT/artifacts/ncc/bin" "$ROOT/artifacts/ncc/tools"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o "$ROOT/artifacts/ncc/bin/ncc" ./cmd/ncc
go version -m "$ROOT/artifacts/ncc/bin/ncc" > "$ROOT/artifacts/ncc/build-info.txt"
cp -R "$ROOT/systemd" "$ROOT/artifacts/ncc/"
cp "$ROOT/tools/device/"* "$ROOT/artifacts/ncc/tools/"
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
if test -f "$ROOT/artifacts/ncc-native-wifi-sta/bin/ncc-native-wifi-sta-helper"; then mkdir -p "$ROOT/artifacts/ncc/experimental"; cp "$ROOT/artifacts/ncc-native-wifi-sta/bin/ncc-native-wifi-sta-helper" "$ROOT/artifacts/ncc/experimental/"; fi
if test -f "$ROOT/artifacts/stock-start-helper/ncc-stock-start-helper"; then
    mkdir -p "$ROOT/artifacts/ncc/experimental"
    cp "$ROOT/artifacts/stock-start-helper/ncc-stock-start-helper" "$ROOT/artifacts/ncc/experimental/"
fi
# Plate recognition: models and the inference engine (freshly built if
# present, otherwise the checked-in prebuilt binary) with their notices.
mkdir -p "$ROOT/artifacts/ncc/plate/models" "$ROOT/artifacts/ncc/plate/licenses"
cp "$ROOT/models/plate/detector.tflite" "$ROOT/models/plate/ocr.tflite" "$ROOT/models/plate/plate-model.json" "$ROOT/models/plate/README.md" "$ROOT/models/plate/LICENSING.md" "$ROOT/artifacts/ncc/plate/models/"
cp -R "$ROOT/models/plate/notices" "$ROOT/artifacts/ncc/plate/models/"
if test -f "$ROOT/artifacts/plate/plate-engine-aarch64"; then
    cp "$ROOT/artifacts/plate/plate-engine-aarch64" "$ROOT/artifacts/ncc/plate/plate-engine"
else
    cp "$ROOT/prebuilt/plate-engine/plate-engine-aarch64" "$ROOT/artifacts/ncc/plate/plate-engine"
fi
cp "$ROOT/prebuilt/plate-engine/licenses/"* "$ROOT/artifacts/ncc/plate/licenses/"
python3 "$ROOT/tools/sbom.py"
python3 "$ROOT/tools/third-party-licenses.py"
cp "$ROOT/LICENSE" "$ROOT/artifacts/ncc/LICENSE"
cp "$ROOT/THIRD_PARTY_LICENSES.md" "$ROOT/artifacts/ncc/THIRD_PARTY_LICENSES.md"
cp -R "$ROOT/licenses" "$ROOT/artifacts/ncc/"
python3 "$ROOT/tools/package.py" "$ROOT/artifacts/ncc"
