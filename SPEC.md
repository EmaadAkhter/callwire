# Callwire Wire Protocol — v1

Plain TCP, length-prefixed msgpack. No TLS, no auth — localhost / trusted network only.

## Frame format

```
[4 bytes: big-endian uint32 payload length][N bytes: msgpack payload]
```

Length prefix is **payload only**, not including the 4-byte header itself.

## Message types

### Request

```json
{
  "id": 17,
  "type": "request",
  "func": "predict",
  "args": [1.5, 2.0]
}
```

- `id`: uint64, monotonically increasing per connection
- `type`: literal `"request"`
- `func`: string, name of the function to call on the server
- `args`: array of positional arguments

### Success response

```json
{
  "id": 17,
  "type": "response",
  "result": ...
}
```

- `id`: matches the request
- `type`: literal `"response"`
- `result`: any msgpack-encodable value

### Error response

```json
{
  "id": 17,
  "type": "error",
  "error_type": "ValueError",
  "message": "inches must be positive"
}
```

- `id`: matches the request
- `type`: literal `"error"`
- `error_type`: string, exception class name on Python side, mapped to `WireError.ErrorType` on Go side
- `message`: human-readable description

## Type mapping

| Python | Wire (msgpack) | Go |
|--------|----------------|-----|
| `int` | int | `int64` |
| `float` | float64 | `float64` |
| `str` | str | `string` |
| `None` | nil | `nil` / zero value |
| `bool` | bool | `bool` |
| `list` | array | `[]interface{}` |
| `dict` | map | `map[string]interface{}` |
| exception | error_type + message | `*WireError` |

### Known edge: Python `None` → Go value types

msgpack has no native `None` for Go's value types. A Python `None` passed as an `int` arg will unpack into Go as the type's zero value (`0`), **not** an explicit null. This is a documented limitation — don't try to "fix" it with special-case marshalling; the behavior is consistent and predictable across all msgpack implementations.

### Known edge: Go `msgpack.Marshal`/`Unmarshal` round-trip fragility

msgpack decodes into generic `interface{}` types (maps, slices). Using a raw type assertion on `msg.Result.(Resp)` works only for simple scalar types. For struct types, expect to need a two-step: `msgpack.Marshal(msg.Result)` then `msgpack.Unmarshal` into the concrete `Resp` type.

## Concurrency model

- **Python server:** thread-per-connection, blocking I/O, one request processed at a time per connection. No server-side multiplexing in v1.
- **Go client:** single-reader goroutine per connection writes responses to pending-call channels. Writes serialized with a mutex. Multiple in-flight requests on one connection supported (matched by `id`).

## Out of scope (v1)

- TLS / authentication
- Connection pooling / reconnection
- Streaming / server-side push
- Code generation
- Third language implementations
- Shared core library
