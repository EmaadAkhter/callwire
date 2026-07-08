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

## Completed (continued)
- ✅ errors.c: thread-local `callwire_error()` buffer + internal `callwire_error_set()`
  (printf-style, via vsnprintf). Fixed a link-time bug: `callwire_value_free` was
  defined in both codec.c AND errors.c (duplicate symbol) — removed from errors.c.
- ✅ framing.c: length-prefixed frame read/write, fully wired to codec.c. Fixed a real
  bug: the original stub called `recv()`/`send()` once and assumed it returned the
  full requested length — TCP makes no such guarantee (short reads/writes are
  normal). Now loops via `recv_all`/`send_all` helpers until the full frame is
  transferred or the connection genuinely fails.
- ✅ client.c: `callwire_client_connect/close/call/stream_begin/stream_recv/
  export_stream_begin/export_stream_send/export_stream_close/bidi_stream_begin/
  bidi_stream_send/bidi_stream_recv/bidi_stream_close_send` — all implemented.
  **Known simplification** (documented in-code): one RPC call in flight per
  connection at a time — no background reader thread or id-keyed pending map like
  Go/Rust/Java. A caller wanting concurrent calls opens multiple client connections.
  Correct and sufficient for a first working C SDK; upgrade path is documented.
- ✅ server.c: `callwire_server_new/export/serve/close` — real accept loop
  (pthread-per-connection), dynamic registry (realloc-grown array, mutex-protected),
  full request dispatch (lookup, invoke, encode response/error, write frame).
  **Scope note**: the public C ABI (`callwire_server_export`) only exposes unary
  registration — client-streaming/bidi server-side dispatch is not yet part of the
  C ABI (stream_chunk/close/end frames are read and silently ignored). This matches
  what callwire.h actually declares; extending the ABI for streaming registration
  is a separate, deliberate API-design step, not an oversight.
- ✅ tests/test_loopback.c: real TCP round-trip — server thread + client connection
  on 127.0.0.1, exercises unary call (int args), unary call (string args/result),
  and the NotFoundError path. **All 3 tests pass.**

## Not Yet Implemented
- ⏳ tls.c: OpenSSL integration (client/server TLS + mTLS)
- ⏳ Streaming registration on the server ABI (client-streaming/bidi handlers)
- ⏳ Background reader thread for concurrent calls per connection (see client.c note above)

## Estimate to Complete
- **Codec**: DONE
- **Framing**: DONE
- **Client (unary + streaming, single-call-per-connection)**: DONE
- **Server (unary dispatch)**: DONE
- **Streaming registration on server ABI**: ~400 LOC
- **TLS integration**: ~400 LOC
- **Concurrent-call client upgrade (reader thread + pending map)**: ~500 LOC

**Remaining: ~1300 LOC** (down from ~3700 at session start)

## Status: C is now a working Tier-1 SDK for unary RPC over plaintext TCP.
Server-streaming/client-streaming/bidi are implemented client-side (matching the
public ABI) but the server ABI doesn't yet expose streaming registration — that's
the next increment, not a blocker for C++/COBOL/Swift to start wrapping the unary
path today.

## Next Steps
1. C++ header-only wrapper over this C core ABI (unary now, streaming as C core catches up)
2. COBOL CALL bindings via the same C ABI (unary only — matches COBOL's typical scope)
3. Swift @cImport over the C ABI
4. Extend callwire.h with streaming server registration once a language actually needs it
