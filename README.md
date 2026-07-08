# Callwire

**High-performance, bidirectional RPC across 12+ languages — Go, Python, Rust, TypeScript, Java, C, C++, COBOL, C#, Kotlin, Swift, Ruby — over raw TCP with MessagePack framing.**

No schemas. No `.proto` files. No codegen. Export a function, call it from anywhere. All 4 gRPC streaming patterns, zero config.

---

## Features

- **Zero-schema RPC** — export any function, call it from any language
- **All 4 gRPC patterns** — unary, server-streaming, client-streaming, bidirectional-streaming (no `.proto` codegen)
- **Bidirectional** — clients and servers call each other over the same socket
- **12+ languages** — Go, Python, Rust, TypeScript (done); Java, C, C++, COBOL, C#, Kotlin, Swift, Ruby (in progress)
- **v3 Orchestration** — one `callwire.toml` spawns and connects workers automatically
- **Dynamic routing** — connect to registry, call any function without knowing worker addresses
- **TLS & mTLS** — secure transport with optional client certificate auth
- **Batch API** — fire multiple calls concurrently over a single connection
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

## Installation & Publishing

- **npm**: `npm install -g @emaad-ansari/callwire` (latest version 2.0.3)
- **cargo**: `cargo install callwire --version 2.0.3`
- **pip**: `pip install callwire==2.0.3`

These packages are automatically published on each release via the CI workflow.

## Orchestration (v2)

Workers are auto-discovered by the `callwire init` CLI and declared in `callwire.toml`:

```toml
[project]
name = "my-project"
version = "1.0.0"

[services.go-worker]
dev_cmd  = "cd go/callwire && go run examples/server.go"
prod_cmd = "./bin/go-worker"

[services.rust-worker]
dev_cmd  = "cd rust && cargo run --quiet --example my-worker"
prod_cmd = "./bin/rust-worker"
```

Generate it with any of the four native CLIs — they all produce the same output:

```bash
# Python
PYTHONPATH=python python3 -m callwire init

# Go
cd go/callwire && go run ./cmd/callwire/ init

# Rust
cargo run --manifest-path rust/Cargo.toml --bin callwire -- init

# TypeScript
npx tsx ts/src/cli.ts init
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

```typescript
// TypeScript — TLS server
const server = new Server();
await server.serve('0.0.0.0', 9090, {
  cert: fs.readFileSync('server.pem', 'utf8'),
  key:  fs.readFileSync('server.key', 'utf8'),
});

// TypeScript — TLS client (skip verify for self-signed)
const client = new Client({ tls: { rejectUnauthorized: false } });
await client.connect('127.0.0.1', 9090);

// TypeScript — TLS client with CA verification + mTLS
const clientMTLS = new Client({ tls: {
  ca:   fs.readFileSync('ca.pem', 'utf8'),
  cert: fs.readFileSync('client.pem', 'utf8'),
  key:  fs.readFileSync('client.key', 'utf8'),
}});
await clientMTLS.connect('127.0.0.1', 9090);
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

---

## How It Compares

### vs gRPC

| Dimension | Callwire | gRPC |
|-----------|----------|------|
| **Schema** | None — export any function | Required `.proto` files + codegen |
| **Latency (noop)** | **32.7 µs** | 57.7 µs |
| **Throughput** | **81K calls/sec** | 62K calls/sec |
| **Transport** | Raw TCP (4-byte length + msgpack) | HTTP/2 + HPACK |
| **Bidirectional** | Same socket, any order | HTTP/2 streams (half-duplex per stream) |
| **Orchestration** | Built-in `callwire.toml` + `init()` | External (Kubernetes, Consul, etc.) |
| **Languages** | **12+ (Go, Python, Rust, TS, Java, C, C++, COBOL, C#, Kotlin, Swift, Ruby)** | 11+ languages |
| **Streaming** | **All 4 (unary, server, client, bidi)** | All 4 |
| **Browser** | No | Yes (gRPC-Web) |
| **Ecosystem** | Minimal | Envoy, gRPC-Gateway, health probes, reflection |

**When to pick Callwire:** polyglot services, developer velocity over formal schemas, teams that want zero-config orchestration and legacy-system bridging (COBOL↔Go/Python/Rust in one wire protocol, no middleware).

**When to pick gRPC:** cross-org APIs, browser clients, extensive tooling ecosystem (reflection, health checks, gRPC-Gateway), mature production observability.

### vs protosocket (Momento)

Rust-only TCP RPC framework (v1: 100KHz, sub-ms p99.9). Callwire has protosocket beat on language coverage (4 runtimes vs 1) and built-in orchestration. protosocket is faster per-core for pure Rust workloads and has production battle-testing at Momento scale.

### vs ZeroRPC / Zero (zeroapi)

Python MessagePack-over-ZeroMQ RPC. Zero hits ~100K req/s on TCP but is Python-only and has a hard `gevent` dependency. Callwire matches that throughput **in every language** and adds TLS, streaming, orchestration, and cross-language interop.

### vs MagicOnion (C#)

MessagePack-over-gRPC for .NET/Unity. Shares Callwire's zero-schema philosophy (C# interfaces instead of `.proto`) but is C#-only and inherits gRPC's HTTP/2 overhead. Callwire is 1.3–1.7× faster on wire latency and spans 12+ runtimes including a dedicated C# SDK.

### vs Cap'n Proto RPC

Zero-copy RPC with time-travel (promise pipelining). Extremely fast deserialization, but requires `.capnp` schemas and supports only 6 languages. Callwire has no schema, wider language coverage, and built-in orchestration.

### vs Apache Thrift

Mature, 20+ language RPC with multiple transports. Requires `.thrift` schemas + codegen, no streaming. Callwire is simpler to set up and faster for the languages it supports.

### vs NPRPC

Feature-rich multi-transport RPC (TCP/WS/HTTP3/QUIC/SharedMemory) for C++/TS/Swift with FlatBuffers. Strong where Callwire doesn't go (browsers, QUIC). But Callwire has C++/TS/Swift support (via C core ABI), no schema/codegen, and built-in orchestration. NPRPC's multi-transport is valuable where protocols vary; Callwire focuses on raw-TCP performance and simplicity.

---

## Moat

Callwire's defensible advantages:

1. **Zero-schema across 12+ languages** — no other library lets you export a function in Go/Python/Rust/TS/Java/C/C++/COBOL/C#/Kotlin/Swift/Ruby and call it from any of the others without a schema definition or codegen step. Same zero-schema wire format everywhere.

2. **All 4 gRPC patterns, zero-config** — unary, server-streaming, client-streaming, bidi-streaming all supported. No `.proto` files, no codegen. Export a function that streams; it works from any language.

3. **Legacy-to-modern bridge** — the only RPC framework connecting COBOL mainframes directly to Go/Rust/TS/Python/Java microservices over the same zero-schema wire protocol. No gateway layer, no middleware required.

4. **Built-in orchestration** — `callwire init` auto-detects workers across all languages from a single config file. Competitors require external process managers (supervisord), Kubernetes, or shell scripts.

5. **Bidirectional symmetry** — the same socket serves both client and server roles. Only protosocket offers this; gRPC, Thrift, Cap'n Proto enforce client/server roles.

6. **Protocol simplicity** — 4-byte length prefix + MessagePack. Full spec fits on one page ([SPEC.md](SPEC.md)). Implementing from scratch takes hours, not weeks.

7. **C core ABI** — languages without hand-crafted SDKs can wrap the stable C ABI (`c/include/callwire.h`). Swift, COBOL, and others depend on this frozen interface. Lowers barrier for adding new runtimes.

8. **Per-language CLI** — each SDK ships its own `callwire init` with zero cross-language build dependencies.
