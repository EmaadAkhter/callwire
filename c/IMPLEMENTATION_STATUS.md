# C Core Library Implementation Status

## Completed
- ✅ callwire.h: Public C ABI (frozen) — fixed a real bug where `struct callwire_value`
  was never named, making self-referential array/map fields (`items`, `keys`, `values`)
  an incomplete type that couldn't compile against.
- ✅ CMakeLists.txt: Build system
- ✅ mpack_stub.h: Full msgpack reader + writer (nil/bool/int64/float64/str/bin/array/map,
  all size classes — fix/8/16/32). Not a general-purpose library, just what the wire
  protocol needs. Swap for github.com/ludocode/mpack if broader msgpack coverage is needed.
- ✅ codec.c: Full encode (request/bidi_request/response/error/stream_chunk/stream_end/
  stream_close) and full decode (msgpack payload -> callwire_wire_message_t, including
  recursive array/map value decoding). `callwire_value_free()` and
  `callwire_wire_message_free()` implemented for correct cleanup.
- ✅ src/internal.h: Internal function declarations for client.c/server.c to call into codec.c.
- ✅ tests/test_codec.c: 5 round-trip tests (request, bidi request, string response,
  nested array/map, error + stream frames) — all passing (`gcc -std=c99 ... && run`).

## In Progress / Stub
- ⏳ framing.c: TCP I/O (basic functions sketched, not wired to codec yet)
- ⏳ client.c: Unary + streaming dispatch (depends on codec — now unblocked)
- ⏳ server.c: Handler registration + dispatch (depends on codec — now unblocked)
- ⏳ tls.c: OpenSSL integration
- ⏳ errors.c: Thread-local error buffer

## Estimate to Complete
- **Codec (msgpack encode/decode)**: ~~800 LOC~~ DONE (~700 LOC across codec.c + mpack_stub.h + tests)
- **Framing (frame I/O)**: 150 LOC
- **Client (dispatch + streaming)**: 1200 LOC
- **Server (dispatch + streaming)**: 1000 LOC
- **TLS integration**: 400 LOC

**Remaining: ~2750 LOC**

## Blocking
- Full C implementation → C++ can wrap
- Full C implementation → COBOL can CALL
- Full C implementation → Swift can @cImport

## Next Steps
1. framing.c: length-prefixed frame read/write over a socket fd (mirrors Go/Python framing.py)
2. client.c: connect/call/stream_begin/stream_recv using codec.c + framing.c
3. server.c: export/serve/accept-loop dispatch using codec.c + framing.c
4. Once client.c + server.c compile and a loopback round-trip test passes, C is a real Tier-1 SDK.
5. Then: C++ wraps C core, COBOL CALLs C core, Swift @cImports C core ABI.
