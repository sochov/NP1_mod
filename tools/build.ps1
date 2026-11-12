param([string]$Go = 'go', [switch]$SkipInstall)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
function Check { if ($LASTEXITCODE -ne 0) { throw "Command failed: $LASTEXITCODE" } }
Push-Location "$repo/frontend"
try { if (-not $SkipInstall) { npm.cmd ci; Check }; npm.cmd run build; Check } finally { Pop-Location }
Push-Location "$repo/backend"
try {
    & $Go test ./...; Check
    & $Go vet ./...; Check
    $env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='arm64'
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/bin" | Out-Null
    & $Go build -trimpath -ldflags '-s -w' -o "$repo/artifacts/ncc/bin/ncc" ./cmd/ncc; Check
    & $Go version -m "$repo/artifacts/ncc/bin/ncc" | Out-File -Encoding utf8 "$repo/artifacts/ncc/build-info.txt"
} finally { Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED -ErrorAction SilentlyContinue; Pop-Location }
Copy-Item -Recurse -Force "$repo/systemd" "$repo/artifacts/ncc/"
New-Item -ItemType Directory -Force "$repo/artifacts/ncc/tools" | Out-Null
Copy-Item -Force "$repo/tools/device/*" "$repo/artifacts/ncc/tools/"
if (Test-Path "$repo/artifacts/native-helper/ncc-native-setting-helper") {
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/experimental" | Out-Null
    Copy-Item -Force "$repo/artifacts/native-helper/ncc-native-setting-helper" "$repo/artifacts/ncc/experimental/"
}
if (Test-Path "$repo/artifacts/native-alexa-helper/ncc-native-alexa-helper") {
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/experimental" | Out-Null
    Copy-Item -Force "$repo/artifacts/native-alexa-helper/ncc-native-alexa-helper" "$repo/artifacts/ncc/experimental/"
}
if (Test-Path "$repo/artifacts/native-extended-helper/ncc-native-setting-extended-helper") {
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/experimental" | Out-Null
    Copy-Item -Force "$repo/artifacts/native-extended-helper/ncc-native-setting-extended-helper" "$repo/artifacts/ncc/experimental/"
}
if (Test-Path "$repo/artifacts/native-choice-helper/ncc-native-setting-choice-helper") {
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/experimental" | Out-Null
    Copy-Item -Force "$repo/artifacts/native-choice-helper/ncc-native-setting-choice-helper" "$repo/artifacts/ncc/experimental/"
}
if (Test-Path "$repo/artifacts/native-notification-helper/ncc-native-setting-notification-helper") {
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/experimental" | Out-Null
    Copy-Item -Force "$repo/artifacts/native-notification-helper/ncc-native-setting-notification-helper" "$repo/artifacts/ncc/experimental/"
}
if (Test-Path "$repo/artifacts/native-audio-helper/ncc-native-audio-helper") {
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/experimental" | Out-Null
    Copy-Item -Force "$repo/artifacts/native-audio-helper/ncc-native-audio-helper" "$repo/artifacts/ncc/experimental/"
}
if (Test-Path "$repo/artifacts/stock-start-helper/ncc-stock-start-helper") {
    New-Item -ItemType Directory -Force "$repo/artifacts/ncc/experimental" | Out-Null
    Copy-Item -Force "$repo/artifacts/stock-start-helper/ncc-stock-start-helper" "$repo/artifacts/ncc/experimental/"
}
# Plate recognition: models and the inference engine (freshly built if
# present, otherwise the checked-in prebuilt binary) with their notices.
New-Item -ItemType Directory -Force "$repo/artifacts/ncc/plate/models","$repo/artifacts/ncc/plate/licenses" | Out-Null
foreach ($f in 'detector.tflite','ocr.tflite','plate-model.json','README.md','LICENSING.md') { Copy-Item -Force "$repo/models/plate/$f" "$repo/artifacts/ncc/plate/models/" }
Copy-Item -Recurse -Force "$repo/models/plate/notices" "$repo/artifacts/ncc/plate/models/"
$engine = if (Test-Path "$repo/artifacts/plate/plate-engine-aarch64") { "$repo/artifacts/plate/plate-engine-aarch64" } else { "$repo/prebuilt/plate-engine/plate-engine-aarch64" }
Copy-Item -Force $engine "$repo/artifacts/ncc/plate/plate-engine"
Copy-Item -Force "$repo/prebuilt/plate-engine/licenses/*" "$repo/artifacts/ncc/plate/licenses/"
python "$repo/tools/sbom.py"
Check
python "$repo/tools/third-party-licenses.py"
Check
Copy-Item -Force "$repo/LICENSE" "$repo/artifacts/ncc/LICENSE"
Copy-Item -Force "$repo/THIRD_PARTY_LICENSES.md" "$repo/artifacts/ncc/THIRD_PARTY_LICENSES.md"
Copy-Item -Recurse -Force "$repo/licenses" "$repo/artifacts/ncc/"
python "$repo/tools/package.py" "$repo/artifacts/ncc"
Check
