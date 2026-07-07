#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PY_BIN="$ROOT/python/.venv/bin/python"
if [[ ! -x "$PY_BIN" ]]; then
  PY_BIN="$(command -v python3)"
fi

for cmd in curl go "$PY_BIN"; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing dependency: $cmd"
    exit 1
  fi
done

PY_LOG="${TMPDIR:-/tmp}/callwire-python-demo.log"
GO_LOG="${TMPDIR:-/tmp}/callwire-go-demo.log"
PY_PID=""
GO_PID=""
HOST="${HOST:-localhost}"
GO_CW_PORT="${GO_CW_PORT:-19098}"
GO_HTTP_PORT="${GO_HTTP_PORT:-18089}"
PY_CW_PORT="${PY_CW_PORT:-19099}"
PY_HTTP_PORT="${PY_HTTP_PORT:-18088}"

cleanup() {
  echo ""
  echo "shutting down demo services..."
  if [[ -n "$PY_PID" ]]; then kill "$PY_PID" 2>/dev/null || true; fi
  if [[ -n "$GO_PID" ]]; then kill "$GO_PID" 2>/dev/null || true; fi
  if [[ -n "$PY_PID" ]]; then wait "$PY_PID" 2>/dev/null || true; fi
  if [[ -n "$GO_PID" ]]; then wait "$GO_PID" 2>/dev/null || true; fi
}
trap cleanup EXIT INT TERM

wait_http() {
  local url="$1"
  local name="$2"
  for _ in $(seq 1 30); do
    if curl -fsS -o /dev/null "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "failed waiting for $name at $url"
  echo "--- $name log ---"
  if [[ "$name" == "python" ]]; then cat "$PY_LOG"; else cat "$GO_LOG"; fi
  exit 1
}

pretty_json() {
  "$PY_BIN" -m json.tool
}

run_case() {
  local title="$1"
  local url="$2"
  local body=""
  echo ""
  echo "[$title]"
  for _ in $(seq 1 8); do
    body="$(curl -sS "$url" || true)"
    if echo "$body" | grep -q '"result"'; then
      echo "$body" | pretty_json
      return 0
    fi
    sleep 1
  done
  echo "request failed after retries: $url"
  echo "$body" | pretty_json
  exit 1
}

echo "Starting Python demo service..."
echo "  callwire: $HOST:$PY_CW_PORT, http: :$PY_HTTP_PORT"
CALLWIRE_DEMO_HOST="$HOST" \
PY_CALLWIRE_PORT="$PY_CW_PORT" \
PY_HTTP_PORT="$PY_HTTP_PORT" \
GO_CALLWIRE_ADDR="$HOST:$GO_CW_PORT" \
"$PY_BIN" "$ROOT/examples/python_all.py" >"$PY_LOG" 2>&1 &
PY_PID=$!

echo "Starting Go demo service..."
echo "  callwire: $HOST:$GO_CW_PORT, http: :$GO_HTTP_PORT"
(cd "$ROOT/go/callwire" && GO_CALLWIRE_PORT="$GO_CW_PORT" GO_HTTP_ADDR=":$GO_HTTP_PORT" go run ./cmd/all/ "$HOST:$PY_CW_PORT" ":$GO_HTTP_PORT" "$GO_CW_PORT") >"$GO_LOG" 2>&1 &
GO_PID=$!

wait_http "http://$HOST:$PY_HTTP_PORT/" "python"
wait_http "http://$HOST:$GO_HTTP_PORT/" "go"

echo ""
echo "Callwire demo is ready."
echo "Go HTTP:     http://$HOST:$GO_HTTP_PORT"
echo "Python HTTP: http://$HOST:$PY_HTTP_PORT"

run_case "Go -> Python: greet(world)" "http://$HOST:$GO_HTTP_PORT/go-to-python?func=greet&s=world"
run_case "Go -> Python: reverse(hello)" "http://$HOST:$GO_HTTP_PORT/go-to-python?func=reverse&s=hello"
run_case "Go -> Go: double(21)" "http://$HOST:$GO_HTTP_PORT/go-to-go?func=double&i=21"
run_case "Go -> Go: upper(hello)" "http://$HOST:$GO_HTTP_PORT/go-to-go?func=upper&s=hello"
run_case "Python -> Go: double(10)" "http://$HOST:$PY_HTTP_PORT/python-to-go?func=double&i=10"
run_case "Python -> Go: upper(world)" "http://$HOST:$PY_HTTP_PORT/python-to-go?func=upper&s=world"
run_case "Python -> Python: greet(world)" "http://$HOST:$PY_HTTP_PORT/python-to-python?func=greet&s=world"
run_case "Python -> Python: reverse(hello)" "http://$HOST:$PY_HTTP_PORT/python-to-python?func=reverse&s=hello"

echo ""
echo "All 4 directions are working with the same call shape."
