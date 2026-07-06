# Callwire vs gRPC — v1 Benchmark

*To be completed after running benchmarks against real workloads.*

## Methodology

- Same function (simple arithmetic), same hardware
- Measure latency (single call) and throughput (concurrent calls)
- Compare against gRPC with protocol buffers (unary, no streaming)

## Expected tradeoffs

| Aspect | Callwire (v1) | gRPC |
|--------|--------------|------|
| Serialization | msgpack (no schema) | protobuf (schema) |
| Framing | hand-rolled length prefix | HTTP/2 |
| Typing | runtime assertion / decode | compile-time generated |
| Throughput | likely higher (no HTTP/2 overhead) | lower but more robust |
| Latency (p50) | expected lower | higher due to HTTP/2 handshake |

## Results

*TODO*
