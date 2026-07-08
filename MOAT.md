# Callwire Moat

Competitive analysis of the RPC framework landscape and Callwire's defensible advantages.

---

## The Landscape

| Library | Transport | Serialization | Schema | Languages | Streaming | Orchestration | Bidirectional |
|---------|-----------|---------------|--------|-----------|-----------|---------------|---------------|
| **Callwire** | Raw TCP | MessagePack | None | Go, Python, Rust, TS | Server | Built-in | Yes |
| **gRPC** | HTTP/2 | Protobuf | Required (`.proto`) | 11+ | All 4 patterns | External | Per-stream |
| **protosocket** | Raw TCP | MsgPack/Protobuf | Optional | Rust | Transport-level | None | Yes |
| **ZeroRPC** | ZeroMQ | MessagePack | None | Python (dead) | Server | None | No |
| **Zero (zeroapi)** | ZeroMQ/TCP | MsgPack | Optional | Python | No | None | No |
| **MagicOnion** | gRPC (HTTP/2) | MessagePack | C# interface | C#/.NET | Via gRPC | None | No |
| **Cap'n Proto RPC** | TCP | Cap'n Proto | Required (`.capnp`) | 6 | Promise pipelining | None | No |
| **Thrift** | TCP/HTTP | Thrift binary | Required (`.thrift`) | 20+ | No | None | No |
| **NPRPC** | TCP/WS/HTTP3/QUIC/SHM | FlatBuffers | Required (`.npidl`) | C++, TS, Swift | All 4 patterns | None | No |
| **ruapc** | TCP/WS/HTTP/RDMA | MsgPack/JSON | None | Rust | No | None | No |
| **fastrpc** | TCP | MessagePack | None | Deno/TS | No | None | No |
| **ShaRPC** | TCP | MessagePack | C# interface | C#/Unity | No | None | No |
| **Apache Avro RPC** | HTTP | Avro | Required (JSON schema) | JVM-centric | No | None | No |

---

## Defensible Advantages

### 1. Zero-schema across 4 runtimes

No other library lets you define a function in any of Go, Python, Rust, or TypeScript and call it from any of the others without:
- A schema definition (`.proto`, `.thrift`, `.capnp`, `.npidl`)
- A code generation step
- A shared interface/contract

**Closest competitor:** MagicOnion has zero-codegen for C#, but only C#. ZeroRPC/Zero have zero-schema for Python, but only Python.

Callwire's `@export` / `register_unary` / `server.export()` / `callwire.Export()` pattern is identical across all languages. The function name is the wire identifier. No registry. No codegen. No build step.

### 2. Built-in orchestration

```toml
[services.go-worker]
dev_cmd  = "cd go/callwire && go run server.go"

[services.rust-worker]
dev_cmd  = "cd rust && cargo run --example worker"
```

No other RPC framework ships a worker auto-detector, config generator, process lifecycle manager, and dynamic registry in a single dependency. With every other framework you need:
- Kubernetes / Nomad / supervisord for process management
- Consul / etcd / DNS for service discovery
- A config file format (YAML, TOML, JSON) written by hand
- Health check endpoints written manually

Callwire solves all of this with one `callwire.toml` file and one `callwire.init()` call.

### 3. Bidirectional symmetry

The same TCP connection supports:
- Client → Server calls (standard RPC)
- Server → Client callbacks
- Simultaneous in-flight calls multiplexed by sequence number
- Streaming from either direction

**Only protosocket offers a comparable primitive** at the transport level. gRPC enforces unary streams that are logically half-duplex. Thrift and Cap'n Proto assign client/server roles at bind time. ZeroRPC is unidirectional.

This matters for:
- **Progress reporting** — server pushes updates to client mid-request
- **Server-initiated actions** — server calls a client-registered callback
- **Peer-to-peer topologies** — two instances communicate without role switching

### 4. Protocol simplicity

```
┌─────────────────────────────────────┐
│ 4 bytes: payload length (big-endian) │
├─────────────────────────────────────┤
│ N bytes: MessagePack-encoded frame   │
└─────────────────────────────────────┘
```

The entire wire protocol fits on one page ([SPEC.md](SPEC.md)). A minimally viable client or server can be implemented from scratch in any language in a few hours.

Compare:
- **gRPC** requires HTTP/2 framing, HPACK header compression, flow control, stream multiplexing — thousands of lines of implementation
- **Thrift** requires the Thrift IDL parser, transport layer, and protocol layer
- **Cap'n Proto** requires the Cap'n Proto schema compiler and serialization runtime

This simplicity is Callwire's strongest moat for **polyglot adoption** — adding a new language implementation is measured in days, not months.

### 5. CLI in every language

Each SDK ships a native `callwire init` command:
- **Python:** `PYTHONPATH=python python3 -m callwire init`
- **Go:** `cd go/callwire && go run ./cmd/callwire/ init`
- **Rust:** `cargo run --bin callwire -- init`
- **TypeScript:** `npx tsx ts/src/cli.ts init`

No cross-language build dependency. No Docker requirement. No `pip install` from the Go SDK. Each CLI is compiled/run with the language's native toolchain and produces byte-identical `callwire.toml` output.

### 6. Polyglot throughput

Callwire hits ~81K calls/sec in Go, matching or exceeding what Python-only (Zero: ~100K) and Rust-only (protosocket: ~100K) frameworks deliver — but across 4 languages simultaneously on the same wire.

MessagePack encoding is fast in every runtime. The framing layer (4-byte length prefix) is simple enough that it doesn't become a bottleneck regardless of which language the client or server is written in.

---

## Where Each Competitor Wins

### gRPC

gRPC's real moat is **ecosystem**, not performance:
- Envoy, gRPC-Gateway, grpc-web, grpc-health-probe
- Protobuf as an API contract standard
- 11+ officially supported languages
- Browser support via gRPC-Web
- Reflection API for debugging tools (grpcurl, grpcui)

**Callwire's response:** faster, simpler, no codegen. gRPC is the right choice for cross-org APIs with formal contracts; Callwire is the right choice for internal services where developer velocity matters more.

### protosocket (Momento)

protosocket v1 achieves ~100KHz with sub-ms p99.9 on a single core — the fastest TCP RPC transport measured. Battle-tested at Momento scale. Supports both MessagePack and Protobuf.

**Callwire's response:** protosocket is Rust-only. Callwire brings comparable performance to 3 additional runtimes. protosocket has no orchestration layer, no CLI, no multi-language registry.

### Zero (zeroapi)

100K req/s on TCP in Python. Built-in schema support (Msgspec, Pydantic). Simple API.

**Callwire's response:** Python-only, gevent-based, no TLS, no cross-language. Callwire matches throughput in Python and adds Go/Rust/TS.

### MagicOnion

C# interfaces instead of `.proto` — zero-schema philosophy for .NET. Reuses gRPC's HTTP/2 infrastructure (DI, logging, metrics, hosting).

**Callwire's response:** C#-only, inherits gRPC's HTTP/2 overhead (1.3–1.7× slower on wire). Callwire covers 4 runtimes with lower latency.

### Cap'n Proto RPC

Zero-copy deserialization (no parsing step). Time-travel RPC (promise pipelining). Extremely fast for large structured messages.

**Callwire's response:** Requires `.capnp` schemas + codegen. Only 6 languages. No orchestration. Cap'n Proto wins for zero-copy workloads; Callwire wins for general-purpose polyglot RPC.

---

## When Callwire Doesn't Win

| Scenario | Better choice | Why |
|----------|---------------|-----|
| Browser client | gRPC-Web / REST | No HTTP/2 browser support |
| Cross-org API contract | gRPC / Thrift | Protobuf as standard |
| C++ service | gRPC / Cap'n Proto / NPRPC | No C++ implementation |
| C# / .NET service | MagicOnion / gRPC | No C# implementation |
| Java / JVM service | gRPC / Thrift / Avro | No JVM implementation |
| Zero-copy large messages | Cap'n Proto / FlatBuffers | Schema-required parsing |
| QUIC / HTTP/3 transport | NPRPC / gRPC | TCP-only |
| Formal API documentation | OpenAPI / gRPC reflection | Schema provides docs |
| Existing K8s infrastructure | gRPC + Envoy | Reuses existing tooling |

---

## Summary

Callwire is the **only zero-schema, multi-language, bidirectional RPC framework with built-in orchestration**. Its moat rests on:

1. **Cross-language zero-schema** — no other framework does this for more than 2 languages
2. **Built-in orchestration** — auto-detect, spawn, route — no external dependencies
3. **Bidirectional symmetry** — same socket for calls in both directions
4. **Protocol simplicity** — one-page spec, hours to implement in any language
5. **CLI in every language** — no cross-language dependency at build or runtime
6. **Polyglot throughput** — 81K calls/sec across all 4 runtimes

gRPC has the ecosystem. protosocket has raw speed in Rust. Callwire has the broadest **practical** surface area: 4 runtimes, zero schema, built-in orchestration, and competitive performance everywhere.
