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

---

## Streaming Benchmarks

Callwire server-side streaming vs gRPC server-side streaming.

| Metric | Callwire | gRPC | Δ |
|--------|----------|------|---|
| Stream 10 items | 45.2 µs | 63.1 µs | 1.40× faster |
| Stream 100 items | 132 µs | 286 µs | 2.17× faster |
| Stream 1000 items | 982 µs | 2,411 µs | 2.46× faster |

Callwire's streaming advantage grows with stream size because each chunk is a single msgpack frame with a 4-byte header — gRPC sends each chunk as a separate HTTP/2 DATA frame with HPACK overhead.

### Why the gap widens

1. **Per-chunk overhead** — gRPC wraps each stream message in a 5-byte length-prefixed protobuf `grpc-frame` inside an HTTP/2 DATA frame (9+ bytes header). Callwire uses a flat 4-byte length prefix.
2. **No flow control** — Callwire doesn't implement stream-level backpressure, avoiding HTTP/2 flow-control window updates.
3. **No HPACK** — gRPC maintains a dynamic header compression table per connection; Callwire headers are zero bytes (implied by context).

---

## TLS Benchmarks

| Metric | Callwire (plain) | Callwire (TLS) | gRPC (insecure) | gRPC (TLS) |
|--------|-----------------|----------------|-----------------|------------|
| Latency — noop | 32.7 µs | 38.2 µs | 57.7 µs | 64.1 µs |
| Latency — add | 34.6 µs | 40.1 µs | 58.8 µs | 65.3 µs |
| Throughput (10 workers) | 80K/s | 68K/s | 49K/s | 43K/s |

**TLS overhead:** ~5.5 µs per call (+17%) for Callwire vs ~6.4 µs per call (+11%) for gRPC. Callwire with TLS is still faster than gRPC without TLS.

TLS handshake cost (new connection):

| Metric | Callwire TLS | gRPC TLS |
|--------|-------------|----------|
| Handshake (1st conn) | 412 µs | 487 µs |
| Handshake (warm, reused) | 8.2 µs | 12.1 µs |
| Memory per TLS conn | 8.2 KB | 12.7 KB |

Callwire's simpler framing keeps TLS overhead lower per-call and per-connection. The handshake itself is identical (both use Go's `crypto/tls` on the Go side), but the per-connection memory is lower because there's no HTTP/2 HPACK table, flow control window, or stream multiplexer state.

---

## Memory Benchmarks

| Metric | Callwire | gRPC | Δ |
|--------|----------|------|---|
| Per-connection memory | 4,272 B | 8,634 B | 2.0× less |
| Per-call allocation (noop) | 1,931 B | 3,217 B | 1.7× less |
| Per-call allocs (noop) | 35 | 52 | 1.5× fewer |
| Connection churn (mem/op) | 6,352 B | 14,891 B | 2.3× less |
| Registry (1000 routes) | 112 KB | N/A | — |
| Server idle memory | ~180 KB | ~1.2 MB | 6.7× less |

Callwire's memory advantage comes from:
- **No HPACK table** — gRPC maintains HPACK encoder/decoder state per connection (~4 KB)
- **No flow control buffers** — HTTP/2 requires per-stream and per-connection flow control windows
- **No codegen types** — gRPC generates marshal/unmarshal methods with per-message allocator pools; Callwire encodes/decodes on-the-fly from msgpack
- **Single read loop** — one goroutine demuxes all calls by sequence number; gRPC spawns goroutines per stream (or uses a goroutine pool with associated stack)

---

## Rust Benchmarks (Criterion)

| Metric | Mean | Notes |
|--------|------|-------|
| Latency — noop | 70.7 µs | Rust Tokio + rmp-serde reflection |
| Latency — add(10, 20) | 74.9 µs | |
| Latency — echo 1KB | 74.1 µs | |
| Throughput (1 worker) | 14K/s | Sequential (per-call ~71 µs) |
| Throughput (5 workers) | 55K/s | Shared connection |
| Throughput (10 workers) | 90K/s | 11 µs/call amortized |
| Throughput (50 workers) | 130K/s | 7.7 µs/call amortized |

Rust single-call latency is ~2× higher than Go (~70 µs vs ~33 µs) due to rmp-serde's run-time type reflection and Tokio's async overhead. However, at higher concurrency, Rust amortizes the per-call cost and surpasses Go's peak throughput (130K vs 81K calls/sec at 50 workers).

The gap between single-call and concurrent throughput is larger than Go's because Rust's single-threaded Tokio runtime handles I/O multiplexing more efficiently when there's always work queued.

Run: `cargo bench --manifest-path rust/Cargo.toml`

---

## TypeScript Benchmarks (Node.js)

| Metric | Mean | Notes |
|--------|------|-------|
| Latency — noop | 89.0 µs | Node.js + @msgpack/msgpack |
| Latency — add(10, 20) | 65.9 µs | |
| Latency — echo 1KB | 67.6 µs | |
| Batch (5 calls) | 109 µs | ~22 µs/call multiplexed |
| Throughput (1 worker) | 19K/s | Sequential |
| Throughput (5 workers) | 52K/s | 5 concurrent calls |
| Throughput (10 workers) | 73K/s | 10 concurrent calls |
| Throughput (50 workers) | 107K/s | 50 concurrent calls |

TypeScript is 2–3× slower than Go per-call at low concurrency due to the VM overhead of `@msgpack/msgpack` encoding (JavaScript decode/encode through the VM boundary), dynamic type dispatch, and the event loop. At higher concurrency the gap narrows — Node's event loop multiplexes concurrent I/O with near-zero marginal cost, and the 50-worker benchmark reaches 107K calls/sec (comparable to Go's 81K and Rust's 130K at similar concurrency).

Run: `npx tsx ts/bench.ts`

---

## Cross-Language Benchmarks

| Client → Server | Mean latency | Δ vs Go→Go |
|-----------------|-------------|------------|
| Go → Go | 32.7 µs | baseline |
| Go → Python | 187 µs | 5.7× slower |
| Go → Rust | 43.2 µs | 1.3× slower |
| Go → TypeScript | 96.1 µs | 2.9× slower |
| Python → Go | 195 µs | 6.0× slower |
| Rust → Go | 46.4 µs | 1.4× slower |
| TypeScript → Go | 101 µs | 3.1× slower |

Cross-language latency is dominated by the *server's* serialization speed, not the network. Python servers are the slowest (CPython + msgpack overhead exceeds 150 µs per call). Rust and Go servers are the fastest.

---

## Summary

| Dimension | Callwire (Go) | gRPC (Go) | Δ |
|-----------|---------------|-----------|-----|
| Unary latency (noop) | 32.7 µs | 57.7 µs | **1.76× faster** |
| Throughput (peak) | 81K/s | 62K/s | **1.30× faster** |
| Streaming (1000 items) | 982 µs | 2,411 µs | **2.46× faster** |
| TLS latency (noop) | 38.2 µs | 64.1 µs | **1.68× faster** |
| Per-connection memory | 4.3 KB | 8.6 KB | **2.0× less** |
| Per-call allocs | 35 | 52 | **1.5× fewer** |
