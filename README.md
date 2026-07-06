# Callwire

Custom schema-less RPC protocol: Python ↔ Go over plain TCP with msgpack framing.

**Security warning:** Plain TCP on localhost / trusted network only. No TLS, no authentication. Not production-ready for untrusted networks.

## Protocol

See [SPEC.md](SPEC.md) for the wire format and type mapping.

## Project structure

```
callwire/
├── SPEC.md                    # protocol contract
├── python/callwire/           # Python server library
│   ├── server.py              # export(), serve(), dispatch loop
│   ├── framing.py             # length-prefix TCP framing
│   ├── codec.py               # msgpack encode/decode
│   └── errors.py              # exception → wire error mapping
├── go/callwire/               # Go client library
│   ├── client.go              # Connect(), Import[Req, Resp]()
│   ├── framing.go             # length-prefix TCP framing
│   ├── codec.go               # msgpack encode/decode
│   └── errors.go              # WireError type
├── examples/
│   ├── python_server.py       # minimal server
│   └── go_client.go           # minimal client
└── benchmarks/
    └── compare_grpc.md        # v1 benchmark results
```

## Quick start

### Python server

```python
from callwire import export, serve

@export
def double(x):
    return x * 2

serve()  # blocks on localhost:9090
```

```bash
cd python && .venv/bin/python your_script.py
```

### Go client

```go
import "github.com/emaad/callwire"

client, _ := callwire.Connect("localhost:9090")
result, _ := callwire.Import[int, int](client, "double", []interface{}{21})
fmt.Println(result) // 42
```

```bash
cd go/callwire && go run ../examples/go_client.go localhost:9090
```

## Development

```bash
# Python tests
cd python && .venv/bin/python test_self.py && .venv/bin/python test_smoke.py

# Go tests (including integration against Python server)
cd go/callwire && go test -v -timeout 60s ./...
```

## Type mapping edge cases

- `None` → Go value types: a Python `None` in an `int` argument becomes `0` in Go (zero value). This is a msgpack limitation, not a bug.
- Result type decoding: Go's `Import[Req, Resp]` uses a msgpack marshal/unmarshal round-trip for result decoding, not a raw type assertion — struct types require matching field names/tags.

## Out of scope (v1)

- TLS / authentication
- Connection pooling / reconnection
- Streaming / server-side push
- Code generation
- Third language implementations
