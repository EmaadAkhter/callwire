#!/usr/bin/env bash
set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cleanup() {
  echo ""; echo "shutting down..."
  kill $PY_PID $GO_PID 2>/dev/null; wait $PY_PID $GO_PID 2>/dev/null
}
trap cleanup EXIT INT TERM

echo "=== Starting Python (callwire :9099, http :8088) ==="
"$ROOT/python/.venv/bin/python" "$ROOT/examples/python_all.py" &
PY_PID=$!

echo "=== Starting Go   (callwire :9098, http :8089) ==="
(cd "$ROOT/go/callwire" && go run ./cmd/all/ 2>/dev/null) &
GO_PID=$!

echo "waiting for servers..."
for i in $(seq 1 15); do
  curl -sf -o /dev/null "http://localhost:8089/" && curl -sf -o /dev/null "http://localhost:8088/" && break
  sleep 1
done
echo ""

curl_it() { curl -s "$1" | python3 -m json.tool; }
retry() { local u="$1"; for i in $(seq 1 5); do
  local r=$(curl -s "$u")
  if echo "$r" | grep -q '"result"'; then echo "$r" | python3 -m json.tool; return 0; fi
  sleep 1; done; curl -s "$u" | python3 -m json.tool; }

echo "=== Go -> Python ==="
curl_it "http://localhost:8089/go-to-python?func=greet&s=world"
curl_it "http://localhost:8089/go-to-python?func=reverse&s=hello"

echo "=== Go -> Go ==="
curl_it "http://localhost:8089/go-to-go?func=double&i=21"
curl_it "http://localhost:8089/go-to-go?func=upper&s=hello"

echo "=== Python -> Go ==="
retry "http://localhost:8088/python-to-go?func=double&i=10"
retry "http://localhost:8088/python-to-go?func=upper&s=world"

echo "=== Python -> Python ==="
curl_it "http://localhost:8088/python-to-python?func=greet&s=world"
curl_it "http://localhost:8088/python-to-python?func=reverse&s=hello"

echo ""; echo "=== All 4 directions demonstrated ==="
