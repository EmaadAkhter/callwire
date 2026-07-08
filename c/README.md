# Callwire C Core Library

Zero-schema RPC protocol implementation in C99, providing a stable C ABI for building higher-level language bindings.

## Architecture

The C core is the foundation for:
- **C++ SDK**: thin idiomatic wrapper over the C ABI (RAII, templates)
- **COBOL SDK**: calls into the C core via `CALL` (GnuCOBOL FFI)
- **Swift SDK**: wraps C ABI via module map + async/await
- **Potential others**: any language with C interop (Zig, Nim, etc.)

## Build

```bash
mkdir build && cd build
cmake .. -DCALLWIRE_WITH_TLS=ON  # or OFF for bare builds
cmake --build .
cmake --install .
```

## Public C ABI (`include/callwire.h`)

- **Opaque types**: `callwire_client_t`, `callwire_server_t` (hide implementation details)
- **Tagged union**: `callwire_value_t` (dynamic msgpack values)
- **Functions**: unary calls, server-streaming, client-streaming, bidi-streaming
- **Memory**: caller owns allocations; use `callwire_value_free()` for cleanup

## API Surface

**Client:**
- `callwire_client_connect()` — dial addr:port
- `callwire_client_call()` — unary RPC
- `callwire_client_stream_begin/recv()` — server-streaming
- `callwire_client_export_stream_begin/send/close()` — client-streaming
- `callwire_client_bidi_stream_*()` — bidirectional streaming

**Server:**
- `callwire_server_new()` — bind addr:port
- `callwire_server_export()` — register handler
- `callwire_server_serve()` — accept loop (blocks)

## Implementation Notes

- **msgpack**: vendored `mpack` (single-file, MIT, zero deps)
- **TLS**: optional OpenSSL support (`CALLWIRE_WITH_TLS`)
- **Threading**: clients use pthread read loop; servers use thread pool
- **Error handling**: thread-local error buffer via `callwire_error()`

## Current Status

**Stubs complete**, full implementation in progress. The ABI is frozen; higher-level SDKs can depend on this header immediately and implement callsites knowing the signatures won't change.

## Development

Each function has a `TODO` comment marking unimplemented sections. Priority order:
1. Codec (msgpack encode/decode via mpack)
2. Framing (socket I/O)
3. Client (unary + all streaming patterns)
4. Server (dispatch + streaming handler patterns)
5. TLS (OpenSSL integration)

See the top-level plan for sequencing new language SDKs once this core is complete.
