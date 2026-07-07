# Callwire v2

A high-performance, bidirectional RPC framework for Go, Python, and Rust over TCP with MessagePack framing. Both sides of any connection can act as a client and server simultaneously.

> [!IMPORTANT]
> **Language-Agnostic Design:** Callwire is designed around a simple, clean, and fully-specified wire protocol. You can implement clients and servers in any language by following the [SPEC.md](file:///Users/emaad/Developer/callwire/SPEC.md).

## Features

- **Multi-Language:** First-class support for Go, Python, and Rust.
- **Bidirectional:** Connection-symmetric. Clients can serve endpoints and servers can invoke client functions over the same socket.
- **TLS & mTLS:** Secure transport with server authentication and optional Mutual TLS (mTLS).
- **Service Discovery:** Built-in lightweight registry server and auto-refreshing client `DiscoverPool`.
- **Dynamic Reconnections:** Auto-reconnect with exponential backoff on connection drops.
- **Batch API:** Concurrent RPC multiplexing on a single connection.

---

## Quick Start

### Go

```go
import "github.com/emaad/callwire"

// 1. Export local function
callwire.Export("add", func(a, b int) int { return a + b })

// 2. Call remote function using client
client, _ := callwire.Connect("localhost:9090")
addFunc := callwire.RefWithClient[int](client, "add")
result, _ := addFunc(10, 20) // 30
```

### Python

```python
from callwire import export, serve, Client

# 1. Export local function
@export
def add(a, b):
    return a + b

# 2. Call remote function
client = Client()
client.connect("localhost", 9090)
result = client.call("add", [10, 20]) # 30
```

### Rust

```rust
use callwire::{Client, register_unary, serve_on};

// 1. Export local function
register_unary("add", |(a, b): (i64, i64)| -> Result<i64, String> {
    Ok(a + b)
});

// 2. Call remote function
let client = Client::connect("127.0.0.1:9090").await.unwrap();
let result: i64 = client.import("add", &(10i64, 20i64)).await.unwrap(); // 30
```

---

## TLS & Mutual TLS (mTLS)

Callwire v2 supports standard TLS and client certificate verification (mTLS).

### Go TLS Server & Client

```go
// Server
cfg := callwire.TLSConfig{CertPem: cert, KeyPem: key}
callwire.ServeWithTLS("0.0.0.0:9090", cfg)

// Client
clientCfg := callwire.TLSConfig{CAPem: caCert}
client, _ := callwire.ConnectWithReconnectTLS("localhost:9090", clientCfg)
```

### Python TLS Client

```python
client = Client()
client.connect("localhost", 9090, tls={
    "cafile": "ca.pem",
    "certfile": "client.pem", # for mTLS
    "keyfile": "client.key"    # for mTLS
})
```

### Rust TLS Client

```rust
let client_cfg = callwire::TlsConfig {
    cert_pem: vec![],
    key_pem: vec![],
    ca_pem: Some(ca_pem), // Server CA cert
};
let client = client_cfg.connect("127.0.0.1:9090").await.unwrap();
```

---

## Service Discovery

Callwire features a built-in Service Discovery registry (itself powered by Callwire RPC).

### Go Registry & Worker Setup

```go
// 1. Start the registry server
callwire.ServeRegistry("127.0.0.1:29090")

// 2. Start a worker and register it
callwire.Export("say_hello", func(name string) string { return "Hello " + name })
go callwire.Serve("127.0.0.1:29091")
callwire.RegisterWith("127.0.0.1:29090", "hello-service", "127.0.0.1:29091")

// 3. Resolve and call using DiscoverPool
pool, _ := callwire.NewDiscoverPool("127.0.0.1:29090", "hello-service")
sayHello := callwire.DiscoverRef[string](pool, "say_hello")
reply, _ := sayHello("World") // "Hello World"
```

### Python DiscoverPool

```python
from callwire import DiscoverPool

pool = DiscoverPool("127.0.0.1:29090", "hello-service")
client = pool.get()
result = client.call("say_hello", ["World"])
```

### Rust DiscoverPool

```rust
use callwire::DiscoverPool;

let pool = DiscoverPool::new("127.0.0.1:29090", "hello-service").await.unwrap();
let client = pool.get().unwrap();
let res: String = client.import("say_hello", &("World".to_string(),)).await.unwrap();
```

---

## Configuration

| Env Var | Default | Description |
|---|---|---|
| `CALLWIRE_HOST` | `localhost` | Default hostname for auto-serving & clients |
| `CALLWIRE_PORT` | `9090` | Default port for auto-serving & clients |
| `CALLWIRE_AUTO` | `1` | Set to `0` to disable automatic server launching on Export |

---

## Developer Testing

Run the full cross-language integration and unit suites:

```bash
# Go tests
cd go/callwire && go test -v ./...

# Python tests
cd python && .venv/bin/python3 -m unittest discover -s . -p "test_*.py"

# Rust tests
cd rust && cargo test -- --nocapture
```
