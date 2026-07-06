# Callwire

Bidirectional RPC: Python ↔ Go over TCP with msgpack framing. Both sides can serve and call.

**Security:** Plain TCP on localhost / trusted network only. No TLS. Not for untrusted networks.

## Quick start

### Python

```python
from callwire import export, ref

@export                                   # registers + auto-starts server on localhost:9090
def double(x):
    return x * 2

d = ref("double")                         # connect to local server
print(d(21))                              # 42 — call over TCP
```

Server auto-starts on the first `@export`. No `serve()` needed.

### Go

```go
import "github.com/emaad/callwire"

double := callwire.Ref[int]("double")   // lazy default client to localhost:9090
result, _ := double(21)
fmt.Println(result) // 42
```

For a custom address:

```go
callwire.Configure("0.0.0.0", 9091)
d := callwire.Ref[int]("double")
```

Or with an explicit client:

```go
client, _ := callwire.Connect("localhost:9090")
double := callwire.RefWithClient[int](client, "double")
```

### Go server side

```go
callwire.Export("upper", func(s string) string { return strings.ToUpper(s) })
// server auto-starts on localhost:9090 (CALLWIRE_HOST:CALLWIRE_PORT)
```

For a different port:

```go
callwire.Configure("0.0.0.0", 9098)
callwire.Export("upper", func(s string) string { return strings.ToUpper(s) })
```

`callwire.Serve(":9098")` is still available for explicit blocking servers.

## All 4 directions

Both sides are symmetric — add to either or both:

```
Go client  →  Python server    (Connect + Ref  →  @export + auto-serve)
Python client → Go server      (ref  →  Export + Serve)
Python → Python                 (ref → @export on another Python process)
Go → Go                         (Connect + Ref → Export + Serve on another Go process)
```

Run the demo:

```bash
./examples/demo.sh
```

## Project structure

```
callwire/
├── SPEC.md                          # wire format + type mapping
├── python/callwire/
│   ├── __init__.py                  # export, serve, configure, Client, ref
│   ├── server.py                    # @export, serve(), auto-start, configure()
│   ├── client.py                    # Client class (reader goroutine + write mutex)
│   ├── ref.py                       # ref() — seamless function call proxy
│   ├── framing.py                   # length-prefix TCP (looping recv)
│   ├── codec.py                     # msgpack pack/unpack
│   └── errors.py                    # allowlist-based error exposure
├── go/callwire/
│   ├── client.go                    # Connect(), Import(), Ref[Resp](), RefWithClient[Resp](), Configure()
│   ├── server.go                    # Export(), Serve(), reflection dispatch
│   ├── framing.go                   # io.ReadFull for partial-read safety
│   ├── codec.go                     # msgpack encode/decode
│   └── errors.go                    # WireError type
├── examples/
│   ├── python_all.py                # Python all-in-one (callwire + HTTP)
│   ├── go_all.go → go/callwire/cmd/all/main.go
│   └── demo.sh                      # starts both, runs all 4 directions
├── benchmarks/
│   └── compare_grpc.md
```

## Configuration

| Env var | Default | Effect |
|---------|---------|--------|
| `CALLWIRE_HOST` | `localhost` | Auto-start bind host / default client address |
| `CALLWIRE_PORT` | `9090` | Auto-start bind port / default client address |
| `CALLWIRE_AUTO` | `1` | Set to `0` to disable auto-start (Python only) |

Python:

```python
from callwire import configure
configure(host="0.0.0.0", port=9091)
```

Go:

```go
callwire.Configure("0.0.0.0", 9091)
```

Must be called before any `@export` / `Ref[ ]`.

## Development

```bash
# Python tests
cd python && .venv/bin/python test_self.py && .venv/bin/python test_smoke.py && .venv/bin/python test_client.py

# Go tests (includes integration against Python server)
cd go/callwire && go test -v -timeout 120s ./...
```

## Known edges

- **`None` → Go value types:** A Python `None` in an `int` argument unpacks to Go's zero value (`0`). msgpack limitation, not a bug.
- **Result type decoding:** Go `Import[Req, Resp]` uses a msgpack marshal/unmarshal round-trip — struct types need matching field names/tags.
- **Python `ref()`** caches one TCP connection per address. Lazy-connects on first call.

## Out of scope (v1)

TLS, connection pooling, reconnection, streaming, code generation, third languages.
