#!/bin/sh
# Build script for the Callwire COBOL SDK. Compiles the C core + COBOL shim,
# then compiles/links tests/test_loopback.cob against them.
set -e
cd "$(dirname "$0")/.."

mkdir -p /tmp/callwire_cobol_build
for f in codec framing client server errors; do
  gcc -std=c99 -pthread -Ic/include -c c/src/$f.c -o /tmp/callwire_cobol_build/$f.o
done
gcc -std=c99 -Ic/include -c cobol/src/cobol_shim.c -o /tmp/callwire_cobol_build/shim.o

cobc -x cobol/tests/test_loopback.cob -o /tmp/callwire_cobol_build/test_loopback \
  /tmp/callwire_cobol_build/shim.o \
  /tmp/callwire_cobol_build/codec.o \
  /tmp/callwire_cobol_build/framing.o \
  /tmp/callwire_cobol_build/client.o \
  /tmp/callwire_cobol_build/server.o \
  /tmp/callwire_cobol_build/errors.o \
  -lpthread

echo "Built: /tmp/callwire_cobol_build/test_loopback"
echo "Run against a server listening on 127.0.0.1:19299 with 'add' and 'greet' exported."
