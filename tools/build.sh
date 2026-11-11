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
python3 "$ROOT/tools/sbom.py"
cp "$ROOT/LICENSE" "$ROOT/artifacts/ncc/LICENSE"
python3 "$ROOT/tools/package.py" "$ROOT/artifacts/ncc"
