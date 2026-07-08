# Callwire Wire Protocol — v3 Specification

Callwire is a language-agnostic, bidirectional RPC protocol designed for high-performance and lightweight communication. It uses a length-prefixed MessagePack framing format over TCP.

Because the protocol is fully documented and language-agnostic, you can build implementations in any language (e.g. C#, JS, C++, Go, Python, Rust) simply by adhering to this specification.

---

## Transport & Framing

- **Transport:** Raw TCP or TCP wrapped in TLS (supporting server authentication and optional Mutual TLS).
- **Framing:** Each message is prefixed with a 4-byte big-endian `uint32` indicating the length of the subsequent MessagePack payload.

```
[4 bytes: big-endian uint32 payload length][N bytes: msgpack payload]
```

---

## Message Schema (MessagePack Map)

Every payload is serialized as a MessagePack map (string keys). The following fields are defined:

| Field | Type | Description |
|---|---|---|
| `id` | `uint64` | Monotonically increasing identifier matching requests to responses |
| `type` | `string` | One of: `"request"`, `"response"`, `"error"`, `"stream_chunk"`, `"stream_end"`, `"stream_close"` |
| `func` | `string` | (Request only) The name of the function to invoke |
| `args` | `array` | (Request only) Positional arguments for the function |
| `stream` | `bool` | (Request only, optional, default `false`) Mark this call as bidirectional streaming |
| `result` | `any` | (Response/Stream only) Return value; for client-streaming, chunks carry payloads in this field |
| `error_type`| `string` | (Error only) Classification/name of the error |
| `message` | `string` | (Error only) Human-readable error message |

### Request Example
```json
{
  "id": 101,
  "type": "request",
  "func": "add",
  "args": [10, 20]
}
```

### Success Response Example
```json
{
  "id": 101,
  "type": "response",
  "result": 30
}
```

### Error Response Example
```json
{
  "id": 101,
  "type": "error",
  "error_type": "ValueError",
  "message": "Invalid input values"
}
```

---

## Streaming Patterns

Callwire supports four RPC call patterns:

### Unary
Client sends one `request` frame; server replies with one `response` or `error` frame.

### Server Streaming
Client sends one `request` frame with `stream: false` (or unset). Server replies with one or more `stream_chunk` frames, each with `result` containing one chunk of data, then a terminal `stream_end` frame. All frames share the same `id`.

### Client Streaming
Client sends one initial `request` frame (which may have empty or partial `args`), then sends one or more `stream_chunk` frames each with `result` containing one chunk of data. Client sends a terminal `stream_close` frame to signal "no more chunks." Server then replies with one `response` or `error` frame. All frames share the same `id`.

### Bidirectional Streaming
Client sends one `request` frame with `stream: true`. Both peers may then send `stream_chunk` frames under the same `id` (from either direction). Each peer sends a `stream_end` frame when its sending direction is complete. The call is complete when both peers have sent `stream_end` frames, or one sends an `error`. All frames share the same `id`.

---

## Streaming Correlation Rules

Streaming uses a single `id` reused across multiple frames to correlate chunks:
- **Server streaming:** Request `id` used for all `stream_chunk` + `stream_end` frames from server.
- **Client streaming:** Request `id` used for all client-side `stream_chunk` frames, the `stream_close` frame, and the final `response`/`error` from server.
- **Bidirectional streaming:** Request `id` used for all `stream_chunk` frames from both peers and both peers' `stream_end` frames.
- Out-of-order delivery: Server may reply with frames in any order (matched by `id`); clients must correlate by `id`, not arrival order.

---

## Bidirectional & Batch RPCs

- **Bidirectional Symmetry:** Both clients and servers can initiate requests over the same TCP socket (separate from bidirectional streaming pattern — this refers to call *initiation* not call *payload direction*).
- **Batching:** Clients can send multiple request frames sequentially without waiting for individual responses. The server executes these concurrently and returns matching response frames out-of-order.

---

## Built-In Service Registry

The registry is itself a standard Callwire server exporting two RPC endpoints:
1. `register(service_name: string, addr: string) -> nil`
2. `discover(service_name: string) -> []string`
