# Callwire

**High-performance, bidirectional RPC across Go, Python, Rust, and TypeScript — over raw TCP with MessagePack framing.**

No schemas. No `.proto` files. No codegen. Export a function, call it from anywhere.

---

## Features

- **Zero-schema RPC** — export any function, call it from any language
- **Bidirectional** — clients and servers can call each other over the same socket
- **v2 Orchestration** — one `callwire.toml` spawns and connects workers automatically
- **Dynamic routing** — connect to a registry, call any function without knowing worker addresses
- **TLS & mTLS** — secure transport with optional client certificate auth
- **Batch API** — fire multiple calls concurrently over a single connection
- **Streaming** — server-side streaming via generators / `AsyncIterable`
- **Auto-reconnect** — exponential backoff on connection drops

---

## Quick Start

### Go

```go
import "github.com/emaad/callwire"

// Export a function
callwire.Export("add", func(a, b int) int { return a + b })

// Call a remote function
client, _ := callwire.Connect("localhost:9090")
result, _ := callwire.Ref[int](client, "add")(10, 20) // 30
```

### Python

```python
import callwire

# 1. Export a local function (makes it server-ready)
@callwire.export
def add(a, b):
    return a + b

# 2. Dynamic module import (connects & invokes dynamically)
from callwire import add

result = add(10, 20)  # 30
```

### Rust

```rust
use callwire::{Client, register_unary};

register_unary("add", |(a, b): (i64, i64)| Ok(a + b));

let client = Client::connect("127.0.0.1:9090").await?;
let result: i64 = client.import("add", &(10i64, 20i64)).await?; // 30
```

### TypeScript

```typescript
import { Server, remote } from 'callwire';

// 1. Export local function
const server = new Server();
server.export('add', ([a, b]) => (a as number) + (b as number));
await server.serve('0.0.0.0', 9090);

// 2. Call dynamically using the remote Proxy
const result = await remote.add(10, 20); // 30
```

---

## Orchestration (v2)

Define your workers in `callwire.toml` at the project root:

```toml
[workers.go-worker]
cmd  = "go run examples/1_standalone/go_server.go"
lang = "go"

[workers.rust-worker]
cmd  = "cargo run --example cross_lang_client"
lang = "rust"
```

Then call `init()` — Callwire starts a registry, spawns workers, and routes everything automatically:

```python
import callwire

callwire.init()  # reads callwire.toml, spawns workers

# Import functions dynamically as if they were local!
from callwire import add, predict

res1 = add(15, 27)      # → routed to Go worker
res2 = predict("data")  # → routed to Rust worker

callwire.shutdown()
```

See the full demo → [examples/2_orchestrated/demo.py](examples/2_orchestrated/demo.py)

### FastAPI integration

```python
from contextlib import asynccontextmanager
from fastapi import FastAPI
import callwire

@asynccontextmanager
async def lifespan(app: FastAPI):
    await callwire.async_init()
    yield
    await callwire.async_shutdown()

app = FastAPI(lifespan=lifespan)
```

---

## Service Discovery & Dynamic Routing

Workers self-register with the registry. Clients connect once and call anything dynamically — no worker addresses needed.

```python
# Python — dynamic module import
from callwire import add
result = add(10, 20)  # routed transparently via registry
```

```rust
// Rust — connect to registry, route calls transparently
let client = callwire::Client::connect_registry("127.0.0.1:29000").await?;
let sum: i32 = client.import("add", &(10, 20)).await?;
```

```typescript
// TypeScript — connect to registry, route calls transparently
const client = new Client();
await client.connectRegistry('127.0.0.1', 29000);
const sum = await client.call<number>('add', [10, 20]);
```

For load-balancing across multiple workers of the same type, use `DiscoverPool`:

```go
pool, _ := callwire.NewDiscoverPool("127.0.0.1:29090", "my-service")
result, _ := callwire.DiscoverRef[string](pool, "say_hello")("World")
```

---

## TLS & mTLS

```go
// Go — TLS server
callwire.ServeWithTLS("0.0.0.0:9090", callwire.TLSConfig{
    CertPem: cert,
    KeyPem:  key,
})

// Go — TLS client (with optional mTLS)
client, _ := callwire.ConnectWithReconnectTLS("localhost:9090", callwire.TLSConfig{
    CAPem: caCert,
})
```

```python
# Python — TLS client
client.connect("localhost", 9090, tls={
    "cafile":   "ca.pem",
    "certfile": "client.pem",  # mTLS
    "keyfile":  "client.key",  # mTLS
})
```

```rust
// Rust — TLS client
let client = callwire::TlsConfig { ca_pem: Some(ca_pem), ..Default::default() }
    .connect("127.0.0.1:9090").await?;
```

---

## Streaming

```typescript
// TypeScript — server-side streaming
server.export('count_up', async function* ([n]) {
  for (let i = 1; i <= (n as number); i++) yield i;
});

for await (const chunk of client.callStream<number>('count_up', [5])) {
  console.log(chunk); // 1, 2, 3, 4, 5
}
```

---

## Examples

```
examples/
├── 1_standalone/   — One Go server, one client (Python / Rust / TypeScript)
└── 2_orchestrated/ — One command spawns Go + Rust workers automatically
```

→ [examples/README.md](examples/README.md)

---

## Configuration

| Env Var | Default | Description |
|---|---|---|
| `CALLWIRE_HOST` | `localhost` | Default hostname for auto-serving & clients |
| `CALLWIRE_PORT` | `9090` | Default port |
| `CALLWIRE_AUTO` | `1` | Set to `0` to disable auto-server on Export |
| `CALLWIRE_REGISTRY` | *(set by orchestrator)* | Registry address for worker mode |
| `CALLWIRE_SPAWNED` | *(set by orchestrator)* | `1` when running as a managed worker |

---

## Running Tests

```bash
# Go
cd go/callwire && go test -v ./...

# Python
cd python && .venv/bin/python3 -m unittest discover -s . -p "test_*.py"

# Rust
cd rust && cargo test -- --nocapture

# TypeScript
cd ts && npm test
```

---

## Wire Protocol

Callwire uses a simple, fully-specified binary protocol — implement it in any language.  
→ [SPEC.md](SPEC.md)

---

## Performance

`~33 µs` per round-trip · `~81K calls/sec` on a single connection · **1.3–1.7× faster than gRPC** for unary workloads on Apple M4.

| Metric | Callwire | gRPC | Δ |
|--------|----------|------|---|
| Latency — noop | **32.7 µs** | 57.7 µs | 1.76× faster |
| Latency — add(a, b) | **34.6 µs** | 58.8 µs | 1.70× faster |
| Throughput (10 workers) | **80K calls/sec** | 49K calls/sec | 1.65× faster |
| Throughput (100 workers) | **81K calls/sec** | 62K calls/sec | 1.30× faster |

Full breakdown → [benchmarks/compare_grpc.md](benchmarks/compare_grpc.md)
