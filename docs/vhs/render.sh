#!/usr/bin/env bash
# Render all VHS demo GIFs for encli and encx-mock.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VHS_DIR="$ROOT/docs/vhs"
BIN_DIR="$VHS_DIR/bin"
GIF_DIR="$ROOT/docs/gifs"

command -v vhs >/dev/null 2>&1 || { echo "vhs not found; install: brew install vhs" >&2; exit 1; }
command -v ttyd >/dev/null 2>&1 || { echo "ttyd not found; install: brew install ttyd" >&2; exit 1; }
command -v ffmpeg >/dev/null 2>&1 || { echo "ffmpeg not found; install: brew install ffmpeg" >&2; exit 1; }

mkdir -p "$BIN_DIR" "$GIF_DIR"

echo "==> Building encli and encx-mock..."
(cd "$ROOT" && go build -o "$BIN_DIR/encli" ./cmd/encli/)
(cd "$ROOT" && go build -o "$BIN_DIR/encx-mock" ./cmd/encx-mock/)

# Ensure VHS Require finds our freshly built binaries.
export PATH="$BIN_DIR:$PATH"

TAPES=(
  encli-quickstart.tape
  encli-gameplay.tape
  encx-mock-server.tape
  encx-mock-features.tape
)

cd "$VHS_DIR"

for tape in "${TAPES[@]}"; do
  echo "==> Rendering $tape..."
  vhs "$tape"
done

echo "==> Done. GIFs written to $GIF_DIR (gitignored; CI publishes to GitHub Pages):"
ls -lh "$GIF_DIR"/*.gif
