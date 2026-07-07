# Callwire vs gRPC — Benchmarks

Hardware: Apple M4, macOS, localhost only.  
Go 1.24.6, msgpack v5.  
All benchmarks: `go test -bench=. -benchtime=2s -benchmem` unless noted.

## Methodology

- Same function (simple arithmetic), same machine, localhost TCP.
- Measure latency (single call, sequential) and throughput (concurrent callers).
- gRPC numbers below are from an in-repo benchmark harness (`grpc_bench_test.go`) run on the same machine and in the same `go test -bench` session as Callwire.
- Transport is localhost TCP with insecure gRPC credentials (no TLS) to keep transport assumptions comparable.
- **Callwire client and server both in Go** unless marked "→Python"

## Microbenchmarks (in-process, no network)

| Benchmark | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| `encodeRequest` (0 args) | 429.9 ns | 288 | 4 |
| `encodeRequest` (1 arg) | 474.8 ns | 288 | 4 |
| `encodeRequest` (5 args) | 663.6 ns | 288 | 4 |
| `encodeRequest` (100 args) | 4,729.0 ns | 672 | 6 |
| `encodeResponse` (int) | 450.3 ns | 288 | 4 |
| `encodeResponse` (string 1KB) | 615.7 ns | 1,440 | 5 |
| `encodeResponse` (string 64KB) | 7,072.0 ns | 74,054 | 5 |
| `encodeResponse` (struct 10-field) | 826.4 ns | 416 | 5 |
| `encodeResponse` (array 100) | 4,642.0 ns | 672 | 6 |
| `decodeMessage` (response int) | 410.2 ns | 168 | 3 |
| `decodeMessage` (response string 1KB) | 595.8 ns | 1,208 | 5 |
| `decodeMessage` (error) | 492.6 ns | 200 | 5 |
| `dispatch` (success — reflection) | 811.9 ns | 586 | 10 |
| `dispatch` (not-found) | 655.5 ns | 868 | 8 |
| `writeFrame` + `readFrame` (100B) | 107.7 ns | 232 | 4 |
| `writeFrame` + `readFrame` (1KB) | 413.1 ns | 2,184 | 4 |
| `writeFrame` + `readFrame` (64KB) | 15,932.0 ns | 139,276 | 4 |
| Full encode→write→read→decode roundtrip | 1,337.0 ns | 648 | 16 |

## Point-to-Point Latency (Go→Go, single client)

| Function | Mean | Bytes/op | Allocs/op |
|----------|------|----------|-----------|
| Noop (void) | 32.7 µs | 1,931 | 35 |
| Echo int | 34.5 µs | 2,092 | 44 |
| Echo string 10B | 33.0 µs | 2,236 | 48 |
| Echo string 1KB | 37.0 µs | 14,137 | 53 |
| Sum (2 args) | 34.6 µs | 2,178 | 45 |
| Error path | 47.9 µs | 2,140 | 41 |

**Callwire Go→Go: ~33 µs per RPC, ~35-50 allocs, ~2 KB per call.**

## Throughput vs Concurrency (Go→Go)

| Workers | Latency (mean) | Calls/sec (approx) | Notes |
|---------|---------------|-------------------|-------|
| 1 | 32.8 µs | 30K | Sequential |
| 5 | 17.8 µs | 56K | Shared connection |
| 10 | 12.4 µs | 80K | |
| 50 | 14.0 µs | 71K | |
| 100 | 12.3 µs | 81K | Near saturation |

**Peak throughput: ~81K calls/sec on a single connection.**  
Write mutex + single readLoop goroutine becomes the bottleneck.

## Payload Variation

| Parameter | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| Arg count (0 args) | 31.3 µs | 2,093 | 36 |
| Arg count (100 args) | 45.0 µs | 6,336 | 43 |
| Result size (0B) | 36.9 µs | 2,123 | 44 |
| Result size (16KB) | 61.2 µs | 127,163 | 53 |
| Result size (1MB) | 1,068.6 µs | 8,338,346 | 69 |
| Struct 10-field | 46.7 µs | 4,974 | 84 |
| Struct 50-field | 72.9 µs | 20,963 | 282 |

**1MB result round-trip: ~1 ms.**  

## Resource Benchmarks

| Benchmark | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| Connection churn (connect+call+close) | 312 µs | 6,352 | 110 |
| Mem per connection | 271 µs | 4,272 | 74 |
| Ref (seamless) vs Import | Identical (~34 µs) | Identical | Identical |

---

## Callwire vs gRPC (same machine, same harness)

| Metric | Callwire | gRPC | Relative |
|--------|----------|------|----------|
| Latency (noop) | 32.7 µs | 57.7 µs | Callwire ~1.76× faster |
| Latency (add) | 34.6 µs | 58.8 µs | Callwire ~1.70× faster |
| Throughput workers=1 | 32.8 µs/op | 57.1 µs/op | Callwire ~1.74× faster |
| Throughput workers=10 | 12.4 µs/op | 20.5 µs/op | Callwire ~1.65× faster |
| Throughput workers=100 | 12.3 µs/op | 16.0 µs/op | Callwire ~1.30× faster |

**Key Takeaway:** Callwire remains ~1.3× to 1.7× faster than gRPC for unary workloads on Apple M4 loopbacks under high concurrency.
