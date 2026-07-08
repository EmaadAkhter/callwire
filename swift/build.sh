#!/bin/sh
# Build script for the Callwire Swift SDK. Bypasses `swift build` — see
# README.md for why (broken SwiftPM manifest execution in some environments).
set -e
cd "$(dirname "$0")"

BUILD=/tmp/callwire_swift_build
mkdir -p "$BUILD"

# 1. C core (vendored copy in Sources/CCallwire).
for f in codec framing client server errors; do
  clang -c -std=c99 -Wall -Wextra -ISources/CCallwire/include -ISources/CCallwire \
    Sources/CCallwire/$f.c -o "$BUILD/$f.o"
done

# 2. Callwire.swift (the Swift wrapper), as its own module.
swiftc -parse-as-library -I Sources/CCallwire/include \
  -c Sources/Callwire/Callwire.swift -o "$BUILD/Callwire.o" \
  -module-name Callwire -emit-module -emit-module-path "$BUILD/Callwire.swiftmodule"

# 3. Test script (top-level executable code, not XCTest — see README.md).
swiftc -I Sources/CCallwire/include -I "$BUILD" \
  -c Tests/CallwireTests/LoopbackTests.swift -o "$BUILD/LoopbackTests.o" -module-name main

# 4. Link and run.
swiftc "$BUILD/Callwire.o" "$BUILD/LoopbackTests.o" \
  "$BUILD/codec.o" "$BUILD/framing.o" "$BUILD/client.o" "$BUILD/server.o" "$BUILD/errors.o" \
  -o "$BUILD/loopback_test"

echo "Built: $BUILD/loopback_test"
"$BUILD/loopback_test"
