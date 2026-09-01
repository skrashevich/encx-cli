#!/usr/bin/env bash
# Runs the PHP end-to-end test: builds the encx shared library, starts the mock
# Encounter server on a free port, and points bindings/php/tests/e2e.php at it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

WORK_DIR="$(mktemp -d)"
MOCK_PID=""

cleanup() {
  if [[ -n "$MOCK_PID" ]]; then
    kill "$MOCK_PID" 2>/dev/null || true
    # Give the server a moment to shut down on its own, then insist: a mock that
    # outlives the run keeps its port and confuses the next one.
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      kill -0 "$MOCK_PID" 2>/dev/null || break
      sleep 0.2
    done
    kill -9 "$MOCK_PID" 2>/dev/null || true
    wait "$MOCK_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

# Prints a port nothing is listening on. The socket is closed before the mock
# binds it, so the caller must be ready for the port to be taken meanwhile.
free_port() {
  php -r '
    $socket = @stream_socket_server("tcp://127.0.0.1:0", $errno, $errstr);
    if ($socket === false) {
        fwrite(STDERR, "cannot open a probe socket: $errstr\n");
        exit(1);
    }
    $name = stream_socket_get_name($socket, false);
    fclose($socket);
    echo substr($name, strrpos($name, ":") + 1), "\n";
  '
}

# Polls the mock until it answers an HTTP request, or the process dies, or the
# deadline passes. Any status line counts: it only proves the server is serving.
wait_for_mock() {
  local addr="$1" pid="$2"
  local deadline=$((SECONDS + 15))

  while ((SECONDS < deadline)); do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 1
    fi

    if ENCX_PROBE_ADDR="$addr" php -r '
        $socket = @stream_socket_client("tcp://" . getenv("ENCX_PROBE_ADDR"), $errno, $errstr, 0.5);
        if ($socket === false) {
            exit(1);
        }
        fwrite($socket, "GET / HTTP/1.0\r\nHost: probe\r\n\r\n");
        stream_set_timeout($socket, 1);
        $status = fgets($socket, 128);
        fclose($socket);
        exit(is_string($status) && str_starts_with($status, "HTTP/") ? 0 : 1);
      ' 2>/dev/null; then
      return 0
    fi

    sleep 0.2
  done

  return 1
}

bash "$ROOT/bindings/php/build.sh"

# Mirror the library location build.sh just wrote to, so the test cannot pick up
# a stale copy from the default search path.
case "$(uname -s)" in
  Darwin) LIB_NAME="libencx.dylib" ;;
  MINGW* | MSYS* | CYGWIN*) LIB_NAME="encx.dll" ;;
  *) LIB_NAME="libencx.so" ;;
esac
export ENCX_LIBRARY="${ENCX_PHP_LIB_DIR:-$ROOT/bindings/php/lib}/$LIB_NAME"
export ENCX_HEADER="$ROOT/bindings/php/encx.h"

echo "==> Building the mock Encounter server"
(cd "$ROOT" && go build -o "$WORK_DIR/encx-mock" ./cmd/encx-mock)

ADDR=""
for attempt in 1 2 3 4 5; do
  ADDR="127.0.0.1:$(free_port)"

  echo "==> Starting the mock on $ADDR (attempt $attempt)"
  ENCX_MOCK_ADDR="$ADDR" "$WORK_DIR/encx-mock" >"$WORK_DIR/mock.log" 2>&1 &
  MOCK_PID=$!

  if wait_for_mock "$ADDR" "$MOCK_PID"; then
    break
  fi

  kill "$MOCK_PID" 2>/dev/null || true
  wait "$MOCK_PID" 2>/dev/null || true
  MOCK_PID=""
done

if [[ -z "$MOCK_PID" ]]; then
  echo "==> The mock server never became reachable. Its log:" >&2
  cat "$WORK_DIR/mock.log" >&2
  exit 1
fi

echo "==> Running bindings/php/tests/e2e.php"
# Not named "status": that is a read-only special in zsh, which would break the
# script for anyone sourcing or re-running it under a different shell.
test_status=0
ENCX_MOCK_ADDR="$ADDR" php "$ROOT/bindings/php/tests/e2e.php" || test_status=$?

exit "$test_status"
