#!/usr/bin/env bash
# Builds the encx C-shared library that the PHP bindings load through FFI.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

OUT_DIR="${ENCX_PHP_LIB_DIR:-$ROOT/bindings/php/lib}"

case "$(uname -s)" in
  Darwin) LIB_NAME="libencx.dylib" ;;
  MINGW* | MSYS* | CYGWIN*) LIB_NAME="encx.dll" ;;
  *) LIB_NAME="libencx.so" ;;
esac

mkdir -p "$OUT_DIR"

echo "==> Building $OUT_DIR/$LIB_NAME (c-shared)"
CGO_ENABLED=1 go build -buildmode=c-shared -o "$OUT_DIR/$LIB_NAME" ./bindings/php/cshared

# cgo writes its own header next to the library. The bindings use the generated
# bindings/php/encx.h instead, which is FFI-parsable, so the cgo one is redundant.
rm -f "${OUT_DIR}/${LIB_NAME%.*}.h"

echo "==> Done: $OUT_DIR/$LIB_NAME"
