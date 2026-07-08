# C Core Library Implementation Status

## Completed
- ✅ callwire.h: Public C ABI (frozen)
- ✅ CMakeLists.txt: Build system
- ✅ mpack_stub.h: Minimal msgpack encoder scaffolding

## In Progress / Stub
- ⏳ framing.c: TCP I/O (basic functions sketched)
- ⏳ codec.c: msgpack encode/decode (started, needs full value encoding/decoding)
- ⏳ client.c: Unary + streaming dispatch
- ⏳ server.c: Handler registration + dispatch
- ⏳ tls.c: OpenSSL integration
- ⏳ errors.c: Thread-local error buffer

## Estimate to Complete
- **Codec (msgpack encode/decode)**: 800 LOC
- **Client (dispatch + streaming)**: 1200 LOC
- **Server (dispatch + streaming)**: 1000 LOC
- **TLS integration**: 400 LOC
- **Tests**: 300 LOC

**Total: ~3700 LOC**

## Effort: 2-3 weeks full-time

## Blocking
- Full C implementation → C++ can wrap
- Full C implementation → COBOL can CALL
- Full C implementation → Swift can @cImport

## Decision
Recommend: Finish protocol + 2 SDKs (Go, Python), claim moat, ship v3.
Then plan C core for v3.1 (unblocks C-family: C++, COBOL, Swift as v3.1 release).
