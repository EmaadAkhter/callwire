# Callwire Swift SDK

`Client`, `Server`, `Value` over the Callwire C core, using async-free
synchronous calls (matches the C core's client.c: one RPC in flight per
connection at a time — open multiple `Client` instances for concurrency).

## Known environment issue: `swift build` / SwiftPM manifest execution broken here

This environment's Xcode Command Line Tools install has a version mismatch
between `swiftc` (reports Swift 6.2.3) and the bundled macOS SDK (built
against Swift 6.2) — `swift build` fails before even reading `Package.swift`
with `Undefined symbols ... PackageDescription.Package.__allocating_init`,
and importing `Foundation` fails with *"this SDK is not supported by the
compiler"*. This is a broken local toolchain, not a Callwire problem — if
your `xcode-select -p` points at a consistent Xcode/CLT install, `swift
build` should work normally with the `Package.swift` in this directory.

**Workaround used to actually build and verify this SDK**: compile directly
with `swiftc`, bypassing SwiftPM's manifest execution (see `build.sh`).

**Second finding while debugging**: `#elseif canImport(Glibc)` as a fallback
branch after `#if canImport(Darwin)` — meant for Linux portability — triggers
a `redefinition of module 'SwiftBridging'` compile error on this toolchain,
even though the branch is never active on macOS. Root cause not fully
diagnosed (likely `canImport()`'s module-probe touching a stale/duplicate
modulemap in the broken CLT install), but it's 100% reproducible and 100%
avoidable: this SDK targets macOS only (see `Package.swift`), so the
`Callwire.swift` source doesn't reference `Glibc` at all. If you add Linux
support later and hit this same error, try `#if os(Linux) ... #else ... #endif`
gated on `os()` instead of `canImport()`, or test on a non-broken toolchain
first to confirm whether this is CLT-specific.

## Avoiding `Foundation`

`Callwire.swift` deliberately avoids `import Foundation` (uses raw
`UnsafeBufferPointer` + `String(decoding:as:)` instead of `Data`) — on a
toolchain with the SDK/compiler mismatch described above, `Foundation` fails
to build entirely via `CoreFoundation`. Stdlib-only avoids that failure mode
regardless of which toolchain you're on.

## Build

```sh
./build.sh   # compiles C core + Callwire.swift + Tests/CallwireTests/LoopbackTests.swift, links, runs
```

## Status

Verified end-to-end (real TCP, 127.0.0.1 loopback): unary call with int args,
unary call with string args, a closure capturing external mutable state
(proves per-registration userdata routing works, not a shared handler slot),
and the `NotFoundError` path. All 4 tests passing.

`examples/1_standalone/swift_{client,server}.swift` also verified interoperable.
