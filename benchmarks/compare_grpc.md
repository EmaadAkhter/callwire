# Callwire vs gRPC

**Hardware:** Apple M4, macOS, localhost TCP only.  
**Go version:** 1.24.6, `msgpack v5`.  
**Method:** `go test -bench=. -benchtime=2s -benchmem` for both Callwire and gRPC in the same test session.

> gRPC numbers come from an in-repo benchmark harness (`grpc_bench_test.go`) on the same machine and same `go test` run — same function (simple arithmetic), same transport (localhost TCP, insecure, no TLS).

---

## TL;DR

| Metric | Callwire | gRPC | Δ |
|--------|----------|------|---|
| Latency — noop | **32.7 µs** | 57.7 µs | **1.76× faster** |
| Latency — add(a, b) | **34.6 µs** | 58.8 µs | **1.70× faster** |
| Throughput (1 worker) | **30K calls/sec** | 17K calls/sec | **1.74× faster** |
| Throughput (10 workers) | **80K calls/sec** | 49K calls/sec | **1.65× faster** |
| Throughput (100 workers) | **81K calls/sec** | 62K calls/sec | **1.30× faster** |

**Callwire is 1.3× to 1.7× faster than gRPC for unary workloads** on Apple M4 loopbacks under all concurrency levels tested.

---

## Encode / Decode Microbenchmarks

In-process, no network. Measures pure serialization cost.

| Benchmark | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| `encodeRequest` — 0 args | 429.9 ns | 288 | 4 |
| `encodeRequest` — 1 arg | 474.8 ns | 288 | 4 |
| `encodeRequest` — 5 args | 663.6 ns | 288 | 4 |
| `encodeRequest` — 100 args | 4,729 ns | 672 | 6 |
| `encodeResponse` — int | 450.3 ns | 288 | 4 |
| `encodeResponse` — string 1 KB | 615.7 ns | 1,440 | 5 |
| `encodeResponse` — string 64 KB | 7,072 ns | 74,054 | 5 |
| `encodeResponse` — struct 10-field | 826.4 ns | 416 | 5 |
| `encodeResponse` — array 100 | 4,642 ns | 672 | 6 |
| `decodeMessage` — response int | 410.2 ns | 168 | 3 |
| `decodeMessage` — response string 1 KB | 595.8 ns | 1,208 | 5 |
| `decodeMessage` — error | 492.6 ns | 200 | 5 |
| `dispatch` — success (reflection) | 811.9 ns | 586 | 10 |
| `dispatch` — not found | 655.5 ns | 868 | 8 |
| `writeFrame + readFrame` — 100 B | 107.7 ns | 232 | 4 |
| `writeFrame + readFrame` — 1 KB | 413.1 ns | 2,184 | 4 |
| `writeFrame + readFrame` — 64 KB | 15,932 ns | 139,276 | 4 |
| **Full encode→write→read→decode roundtrip** | **1,337 ns** | 648 | 16 |

---

## Point-to-Point Latency (Go → Go, 1 client)

| Function | Mean | Bytes/op | Allocs/op |
|----------|------|----------|-----------|
| Noop (void → void) | 32.7 µs | 1,931 | 35 |
| Echo int | 34.5 µs | 2,092 | 44 |
| Echo string 10 B | 33.0 µs | 2,236 | 48 |
| Echo string 1 KB | 37.0 µs | 14,137 | 53 |
| Sum (2 args) | 34.6 µs | 2,178 | 45 |
| Error path | 47.9 µs | 2,140 | 41 |

**Baseline: ~33 µs per RPC, ~2 KB on the wire, ~40 allocs.**

---

## Throughput vs Concurrency (Go → Go)

| Concurrent callers | Latency (mean) | Calls/sec | Notes |
|--------------------|----------------|-----------|-------|
| 1 | 32.8 µs | ~30K | Sequential |
| 5 | 17.8 µs | ~56K | Shared connection |
| 10 | 12.4 µs | ~80K | |
| 50 | 14.0 µs | ~71K | |
| 100 | 12.3 µs | ~81K | Near saturation |

**Peak: ~81K calls/sec on a single connection.**  
Bottleneck is the write mutex + single `readLoop` goroutine — a natural trade-off for connection symmetry.

---

## Payload Sensitivity

| Parameter | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| 0 args | 31.3 µs | 2,093 | 36 |
| 100 args | 45.0 µs | 6,336 | 43 |
| Result 0 B | 36.9 µs | 2,123 | 44 |
| Result 16 KB | 61.2 µs | 127,163 | 53 |
| Result 1 MB | 1,068 µs | 8,338,346 | 69 |
| Struct 10-field | 46.7 µs | 4,974 | 84 |
| Struct 50-field | 72.9 µs | 20,963 | 282 |

**1 MB response round-trip: ~1 ms.**

---

## Resource Benchmarks

| Benchmark | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| Connection churn (connect + call + close) | 312 µs | 6,352 | 110 |
| Memory per connection | 271 µs | 4,272 | 74 |
| `Ref` (seamless) vs `Import` | ~34 µs (identical) | identical | identical |

---

## Callwire vs gRPC — Side by Side

| Metric | Callwire | gRPC | Relative |
|--------|----------|------|----------|
| Latency — noop | **32.7 µs** | 57.7 µs | **1.76× faster** |
| Latency — add(a, b) | **34.6 µs** | 58.8 µs | **1.70× faster** |
| Throughput, 1 worker | **32.8 µs/op** | 57.1 µs/op | **1.74× faster** |
| Throughput, 10 workers | **12.4 µs/op** | 20.5 µs/op | **1.65× faster** |
| Throughput, 100 workers | **12.3 µs/op** | 16.0 µs/op | **1.30× faster** |

### Why is Callwire faster?

1. **No codegen overhead** — MessagePack encodes Go values directly; Protobuf requires generated marshal/unmarshal methods with extra allocations.
2. **Simpler framing** — 4-byte length prefix + raw msgpack vs HTTP/2 + HPACK header compression.
3. **Single read loop** — one goroutine per connection demuxes all in-flight calls by sequence number; no per-request goroutine spawn.
4. **Schema-free dispatch** — function lookup is a plain map; no reflection over generated descriptor trees.

### When gRPC has the edge

- Cross-org contracts that require a versioned, language-neutral schema (`.proto` files as API contracts)
- Languages not yet supported by Callwire
- Existing gRPC infrastructure (Envoy, gRPC-Gateway, etc.)
- HTTP/2 browser support (gRPC-Web)
