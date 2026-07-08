#!/bin/sh
# Build script for the Callwire COBOL SDK. Compiles the C core + COBOL shim,
# builds both the import-side test (client calling a server) and the
# export-side test (a COBOL-hosted server, called by a C++ client).
set -e
cd "$(dirname "$0")/.."

BUILD=/tmp/callwire_cobol_build
mkdir -p "$BUILD" "$BUILD/handlers"

# libcob headers/libs — Homebrew default path, override via COB_INCLUDE_DIR/
# COB_LIB_DIR env vars if your GnuCOBOL install lives elsewhere.
COB_INCLUDE_DIR="${COB_INCLUDE_DIR:-/opt/homebrew/include}"
COB_LIB_DIR="${COB_LIB_DIR:-/opt/homebrew/lib}"

for f in codec framing client server errors convenience; do
  gcc -std=c99 -pthread -Ic/include -c c/src/$f.c -o "$BUILD/$f.o"
done
gcc -std=c99 -I c/include -I "$COB_INCLUDE_DIR" -c cobol/src/cobol_shim.c -o "$BUILD/shim.o"

# --- Import-side test: COBOL client calling a live server ---
cobc -x cobol/tests/test_loopback.cob -o "$BUILD/test_loopback" \
  "$BUILD/shim.o" "$BUILD/codec.o" "$BUILD/framing.o" "$BUILD/client.o" "$BUILD/server.o" "$BUILD/errors.o" \
  -lpthread

echo "Built: $BUILD/test_loopback"
echo "  (run against a server listening on 127.0.0.1:19299 with 'add'/'greet' exported)"

# --- Export-side: COBOL-hosted server ---
# Handler subprograms compile as separate dynamically-loadable modules.
# GOTCHA: libcob's module loader matches the .dylib basename against the
# CALLed name, lowercased, with dashes PRESERVED — ADD-HANDLER must produce
# add-handler.dylib, not add_handler.dylib. cobc's -o controls this directly.
cobc -m cobol/tests/handlers/add_handler.cob -o "$BUILD/handlers/add-handler.dylib"
cobc -m cobol/tests/handlers/greet_handler.cob -o "$BUILD/handlers/greet-handler.dylib"

cobc -x cobol/tests/test_server.cob -o "$BUILD/test_server" \
  "$BUILD/shim.o" "$BUILD/codec.o" "$BUILD/framing.o" "$BUILD/client.o" "$BUILD/server.o" "$BUILD/errors.o" \
  -lpthread

echo "Built: $BUILD/test_server"

# --- Automated export-side round trip: start the COBOL server, hit it with
# a plain C client (via callwire_call_ints/callwire_call_str), tear down. ---
gcc -std=c99 -pthread -I c/include -c cobol/tests/verify_server.c -o "$BUILD/verify_server.o"
gcc -pthread "$BUILD/verify_server.o" "$BUILD/codec.o" "$BUILD/framing.o" "$BUILD/client.o" \
  "$BUILD/server.o" "$BUILD/errors.o" "$BUILD/convenience.o" -o "$BUILD/verify_server"

COB_LIBRARY_PATH="$BUILD/handlers" "$BUILD/test_server" > "$BUILD/server.log" 2>&1 &
SERVER_PID=$!
sleep 0.7
if "$BUILD/verify_server"; then
  echo "COBOL export (server) round trip: PASSED"
else
  echo "COBOL export (server) round trip: FAILED — see $BUILD/server.log"
  kill "$SERVER_PID" 2>/dev/null
  exit 1
fi
kill "$SERVER_PID" 2>/dev/null
