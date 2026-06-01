#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR="${1:-$ROOT/mobile/build}"
mkdir -p "$OUT_DIR"

echo "==> Ensuring gomobile dependency..."
go get golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
go mod tidy

echo "==> Installing gomobile toolchain..."
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest
gomobile init

echo "==> Running tests..."
go test ./mobile/encxmobile/ -count=1

echo "==> Building Encx.xcframework for iOS..."
gomobile bind -target=ios -o "$OUT_DIR/Encx.xcframework" ./mobile/encxmobile

echo "==> Done: $OUT_DIR/Encx.xcframework"
