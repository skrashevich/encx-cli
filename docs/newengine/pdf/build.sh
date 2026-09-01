#!/usr/bin/env bash
#
# Собирает docs/newengine/*.md в один PDF.
#
#   ./docs/newengine/pdf/build.sh [выходной-файл.pdf]
#
# Требуется: pandoc, node >= 22 (встроенный WebSocket), Google Chrome / Chromium.
# paged.js подкачивается один раз в docs/newengine/pdf/.cache (каталог не версионируется).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SRC="$(dirname "$HERE")"
OUT="${1:-$SRC/encounter-new-engine.pdf}"

PAGEDJS_VERSION="0.4.3"
PAGEDJS_URL="https://unpkg.com/pagedjs@${PAGEDJS_VERSION}/dist/paged.polyfill.js"
CACHE="$HERE/.cache"

die() { echo "ошибка: $*" >&2; exit 1; }

command -v pandoc >/dev/null || die "нужен pandoc (brew install pandoc)"
command -v node   >/dev/null || die "нужен node >= 22 (brew install node)"
[ "$(node -p 'parseInt(process.versions.node)')" -ge 22 ] \
  || die "нужен node >= 22: в более старых нет встроенного WebSocket, через который печатается PDF"

# ── paged.js: раскладка по страницам, колонтитулы, нумерация ────────────────
mkdir -p "$CACHE"
POLYFILL="$CACHE/paged.polyfill-${PAGEDJS_VERSION}.js"
if [ ! -s "$POLYFILL" ]; then
  echo "качаю paged.js ${PAGEDJS_VERSION}…"
  curl -sfL "$PAGEDJS_URL" -o "$POLYFILL" || die "не удалось скачать $PAGEDJS_URL"
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cp "$POLYFILL" "$WORK/paged.polyfill.js"

# ── факты для обложки берутся из README, чтобы не разойтись с ним ───────────
iso_to_ru() {
  local iso="$1" y m d months
  y="${iso%%-*}"; m="${iso:5:2}"; d="${iso:8:2}"
  months=(января февраля марта апреля мая июня июля августа сентября октября ноября декабря)
  echo "${d#0} ${months[$((10#$m - 1))]} $y"
}

SNAPSHOT_ISO="$(sed -n 's/^- Снимок сделан: \([0-9-]*\).*/\1/p' "$SRC/README.md" | head -1)"
SNAPSHOT="$([ -n "$SNAPSHOT_ISO" ] && iso_to_ru "$SNAPSHOT_ISO" || echo "дата не указана")"

SPEC="$(sed -n 's/^- Версия документа: \(.*\)/\1/p' "$SRC/README.md" | head -1 | sed 's/, / · /g')"
SPEC="${SPEC:-Swagger 2.0}"

# ── markdown → HTML ────────────────────────────────────────────────────────
mkdir -p "$WORK/src"

# 1. README: заголовок под роль обзорной главы, ссылки на файлы репозитория —
#    в PDF по ним не кликнешь, поэтому превращаются в текст.
sed -e 's|\[`swagger.json`\](swagger.json)|`docs/newengine/swagger.json`|' \
    -e 's|\[`parity.md`\](parity.md)|разделе «Матрица паритета движков»|' \
    -e 's|^# Новый движок Encounter (Go Backend API)|# Обзор: два движка, один клиент|' \
    "$SRC/README.md" > "$WORK/src/01-overview.md"

# 2. Протокол: ссылки на исходники бэкенда → моноширинный текст;
#    jsonc подсвечивается как javascript (комментарии в примерах кадров).
sed -e 's|\[\([^]]*\)\](\.\./internal/[^)]*)|`\1`|g' \
    -e 's|```jsonc|```javascript|' \
    "$SRC/engine-websocket.md" > "$WORK/src/02-websocket.md"

# 3. Матрица паритета — как есть.
cp "$SRC/parity.md" "$WORK/src/03-parity.md"

{ echo '<style>'; cat "$HERE/style.css"; echo '</style>'; } > "$WORK/style.html"

pandoc "$WORK/src/01-overview.md" "$WORK/src/02-websocket.md" "$WORK/src/03-parity.md" \
  --from=gfm \
  --to=html5 \
  --standalone \
  --template="$HERE/template.html" \
  --include-in-header="$WORK/style.html" \
  --toc --toc-depth=2 \
  --number-sections \
  --section-divs \
  --metadata title="Новый движок Encounter — Go Backend API" \
  --variable snapshot="$SNAPSHOT" \
  --variable spec="$SPEC" \
  --output="$WORK/doc.html"

# ── HTML → PDF ─────────────────────────────────────────────────────────────
# Печатает CDP-скрипт, а не `chrome --print-to-pdf`: тот снимает PDF по событию
# load, то есть в середине раскладки paged.js, и отдаёт первые две-три страницы.
node "$HERE/print.js" "$WORK/doc.html" "$OUT"

ls -lh "$OUT"
