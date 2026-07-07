# Callwire vs gRPC — v1 Benchmarks

Hardware: Apple M4, macOS, localhost only.  
Go 1.24.6, msgpack v5.  
All benchmarks: `go test -bench=. -benchtime=1s -benchmem` unless noted.

## Methodology

- Same function (simple arithmetic), same machine, localhost TCP
- Measure latency (single call, sequential) and throughput (concurrent callers)
- gRPC numbers below are from an in-repo benchmark harness (`grpc_bench_test.go`) run on the same machine and in the same `go test -bench` session as Callwire.
- Transport is localhost TCP with insecure gRPC credentials (no TLS) to keep transport assumptions comparable.
- **Callwire client and server both in Go** unless marked "→Python"

## Microbenchmarks (in-process, no network)

| Benchmark | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| `encodeRequest` (0 args) | 397 ns | 288 | 4 |
| `encodeRequest` (1 arg) | 438 ns | 288 | 4 |
| `encodeRequest` (5 args) | 617 ns | 288 | 4 |
| `encodeRequest` (100 args) | 4,577 ns | 672 | 6 |
| `encodeResponse` (int) | 421 ns | 288 | 4 |
| `encodeResponse` (string 1KB) | 623 ns | 1,440 | 5 |
| `encodeResponse` (string 64KB) | 7,558 ns | 74,053 | 5 |
| `encodeResponse` (struct 10-field) | 831 ns | 416 | 5 |
| `encodeResponse` (array 100) | 4,513 ns | 672 | 6 |
| `decodeMessage` (response int) | 408 ns | 168 | 3 |
| `decodeMessage` (response string 1KB) | 598 ns | 1,208 | 5 |
| `decodeMessage` (error) | 488 ns | 200 | 5 |
| `dispatch` (success — reflection) | 781 ns | 582 | 10 |
| `dispatch` (not-found) | 628 ns | 856 | 8 |
| `writeFrame` + `readFrame` (100B) | 102 ns | 232 | 4 |
| `writeFrame` + `readFrame` (1KB) | 370 ns | 2,184 | 4 |
| `writeFrame` + `readFrame` (64KB) | 15,098 ns | 139,277 | 4 |
| Full encode→write→read→decode roundtrip | 1,262 ns | 648 | 16 |

## Point-to-Point Latency (Go→Go, single client)

| Function | Mean | Min | Max | Bytes/op | Allocs/op |
|----------|------|-----|-----|----------|-----------|
| Noop (void) | 29 µs | — | — | 1,547 | 30 |
| Echo int | 30.5 µs | 16 µs | 226 µs | 1,708 | 39 |
| Echo string 10B | 31 µs | — | — | 1,852 | 43 |
| Echo string 1KB | 36 µs | — | — | 13,749 | 48 |
| Sum (2 args) | 31 µs | — | — | 1,793 | 40 |
| Error path | 30 µs | — | — | 1,755 | 36 |

**Callwire Go→Go: ~30 µs per RPC, ~30-50 allocs, ~1.5-2 KB per call.**

## Cross-Language Latency (Go→Python)

*Measured with Python server (`serve("localhost", 9201)`) and Go client.*

| Function | Mean | Notes |
|----------|------|-------|
| Noop | 41.0 µs | Python server overhead adds ~11 µs vs Go→Go |
| Echo int | 41.0 µs | |
| Echo string 10B | 40.7 µs | |
| Add | 40.7 µs | |
| Error | 41.0 µs | |

**Python GIL + thread-per-connection adds ~10-12 µs vs Go→Go.**

## Cross-Language Latency (Python→Go)

*Measured with Go server (`Serve("localhost:9200")`) and Python client (`Client()` + `connect()` + `call()`).*

| Function | Mean | Notes |
|----------|------|-------|
| Noop | 27.5 µs | Slightly faster than Go→Go due to lighter Python client? |
| Echo int | 27.5 µs | |
| Echo string 10B | 28.2 µs | |
| Add | 28.2 µs | |
| Error | 27.5 µs | |

**Python client overhead is minimal; Python→Go latency close to Go→Go.**

## Throughput vs Concurrency (Go→Go)

| Workers | Latency (mean) | Calls/sec | Notes |
|---------|---------------|-----------|-------|
| 1 | 30 µs | 33K | Sequential |
| 5 | 15.5 µs | 65K | Shared connection |
| 10 | 13.6 µs | 74K | |
| 50 | 12.0 µs | 83K | |
| 100 | 11.6 µs | 86K | Near saturation |

**Peak throughput: ~86K calls/sec on a single connection.**  
Write mutex + single readLoop goroutine becomes the bottleneck.  
Per-goroutine connections show similar throughput.

## Payload Variation

| Parameter | Range | Impact |
|-----------|-------|--------|
| Arg count (0→100) | 30→45 µs | +50% at 100 args |
| Result size (0B→1MB) | 30→1,035 µs | Linear with size |
| Struct 10-field | 36 µs | 3.5KB, 64 allocs |
| Struct 50-field | 47 µs | 9KB, 148 allocs |
| String arg (10B→512KB) | 31→893 µs | Linear with size |
| Nested map (depth 1→5) | 36→42 µs | Modest overhead |

**1MB result round-trip: ~1 ms.**  
**512KB string arg round-trip: ~0.9 ms.**

## Resource Benchmarks

| Benchmark | Latency | Bytes/op | Allocs/op |
|-----------|---------|----------|-----------|
| Connection churn (connect+call+close) | 287 µs | 5,937 | 105 |
| Mem per connection | 250 µs | 4,241 | 74 |
| Ref (seamless) vs Import | Identical (~32 µs) | Identical | Identical |
| Registry size (1 vs 1000 funcs) | No diff (~780 ns) | — | — |
| Goroutines (1 vs 100 conns) | No diff (~31 µs) | — | — |

**Ref[ ] zero-cost abstraction — identical to raw Import.**  
**Registry lookup is O(1) — no penalty for many exports.**

## Callwire vs gRPC (same machine, same harness)

Command used:

```bash
cd go/callwire
go test -run '^$' -bench 'Benchmark(LatencyGoToGo|GRPCLatency|ThroughputConcurrency|GRPCThroughputConcurrency)' -benchtime=2s -count=1 .
```

| Metric | Callwire | gRPC | Relative |
|--------|----------|------|----------|
| Latency (noop) | 31.6 µs | 57.2 µs | Callwire ~1.8× faster |
| Latency (add) | 32.8 µs | 57.0 µs | Callwire ~1.7× faster |
| Throughput workers=1 | 31.9 µs/op | 57.2 µs/op | Callwire ~1.8× faster |
| Throughput workers=10 | 12.8 µs/op | 20.2 µs/op | Callwire ~1.6× faster |
| Throughput workers=100 | 11.1 µs/op | 15.3 µs/op | Callwire ~1.4× faster |

**Key takeaway:** In a controlled same-machine unary benchmark, Callwire is faster for this workload, but by ~1.4–1.8× (not 5–10×). These numbers are specific to localhost, no TLS, and the tested request shapes.

## Running the benchmarks yourself

```bash
# All Go benchmarks
cd go/callwire && go test -bench=. -benchtime=1s -benchmem

# Full suite (Go + Python, all cross-language directions)
bash benchmarks/run.sh
```

Results land in `benchmarks/results/<timestamp>/`.
