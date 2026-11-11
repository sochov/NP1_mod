param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
function Check { if ($LASTEXITCODE -ne 0) { throw "Command failed: $LASTEXITCODE" } }
Push-Location "$repo/frontend"
try { npm.cmd ci; Check; npm.cmd run build; Check } finally { Pop-Location }
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
python "$repo/tools/sbom.py"
Check
Copy-Item -Force "$repo/LICENSE" "$repo/artifacts/ncc/LICENSE"
python "$repo/tools/package.py" "$repo/artifacts/ncc"
Check
