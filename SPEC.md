# Callwire Wire Protocol — v2 Specification

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
| `type` | `string` | One of: `"request"`, `"response"`, `"error"`, `"stream_chunk"`, `"stream_end"` |
| `func` | `string` | (Request only) The name of the function to invoke |
| `args` | `array` | (Request only) Positional arguments for the function |
| `result` | `any` | (Response/Stream only) Return value |
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

## Bidirectional & Batch RPCs

- **Bidirectional Symmetry:** Both clients and servers can initiate requests over the same TCP socket.
- **Batching:** Clients can send multiple request frames sequentially without waiting for individual responses. The server executes these concurrently and returns matching response frames out-of-order.

---

## Built-In Service Registry

The registry is itself a standard Callwire server exporting two RPC endpoints:
1. `register(service_name: string, addr: string) -> nil`
2. `discover(service_name: string) -> []string`
