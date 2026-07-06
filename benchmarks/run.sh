#!/usr/bin/env bash
set -euo pipefail

# ── Callwire Benchmark Suite ─────────────────────────────────────────────────
# Runs all 5 phases across Go and Python.
# Results go to benchmarks/results/<timestamp>/

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$DIR")"
GO_DIR="$REPO_ROOT/go/callwire"
PY_DIR="$REPO_ROOT/python"
PY_BENCH="$PY_DIR/bench.py"
PY_BIN="$PY_DIR/.venv/bin/python"

RESULTS_DIR="$DIR/results/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$RESULTS_DIR"
echo "Results → $RESULTS_DIR"

# Kill leftover processes on exit
cleanup() {
    echo ""
    echo "Cleaning up..."
    pkill -f "bench.py" 2>/dev/null || true
    pkill -f "python -c" 2>/dev/null || true
}
trap cleanup EXIT

echo "============================================"
echo " Callwire Benchmark Suite"
echo "============================================"
echo ""

# ── Phase 1: Microbenchmarks (in-process) ────────────────────────────────────
echo "=== Phase 1: Microbenchmarks ==="
(
    cd "$GO_DIR"
    go test -bench='Benchmark(Encode|Decode|Dispatch|Framing|Allocations|EncodeResponse)' \
        -benchtime=3s -count=3 -benchmem 2>&1
) | tee "$RESULTS_DIR/phase1_micro.txt"

echo ""

# ── Phase 2: Latency ─────────────────────────────────────────────────────────
echo "=== Phase 2: Point-to-Point Latency ==="
(
    cd "$GO_DIR"
    # Go→Go latency
    go test -bench='BenchmarkLatencyGoToGo' -benchtime=5s -count=3 -benchmem 2>&1
    echo "---"
    # Go→Py latency (starts Python server)
    go test -bench='BenchmarkLatencyGoToPy' -benchtime=3s -count=1 -benchmem 2>&1
    echo "---"
    # Latency distribution
    go test -bench='BenchmarkLatencyDistribution' -benchtime=10s -count=1 2>&1
) | tee "$RESULTS_DIR/phase2_latency.txt"

echo ""

# ── Phase 3: Throughput & Concurrency ────────────────────────────────────────
echo "=== Phase 3: Throughput & Concurrency ==="
(
    cd "$GO_DIR"
    go test -bench='BenchmarkThroughputConcurrency' -benchtime=10s -count=3 2>&1
    echo "---"
    go test -bench='BenchmarkSharedConnVsMultiConn' -benchtime=10s -count=3 2>&1
    echo "---"
    go test -bench='BenchmarkHeadOfLineBlocking' -benchtime=5s -count=3 2>&1
    echo "---"
    go test -bench='BenchmarkFuncComplexity' -benchtime=5s -count=3 2>&1
) | tee "$RESULTS_DIR/phase3_throughput.txt"

echo ""

# ── Phase 4: Payload & Argument Variation ────────────────────────────────────
echo "=== Phase 4: Payload & Argument ==="
(
    cd "$GO_DIR"
    go test -bench='BenchmarkArgCount' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkResultSize' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkStructRoundTrip' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkStringSize' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkNestedData' -benchtime=3s -count=3 -benchmem 2>&1
) | tee "$RESULTS_DIR/phase4_payload.txt"

echo ""

# ── Phase 5: Resource & Stability ────────────────────────────────────────────
echo "=== Phase 5: Resource & Stability ==="
(
    cd "$GO_DIR"
    go test -bench='BenchmarkConnChurn' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkRegistrySize' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkRefWrapper' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkRefSeamless' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkGoroutineCount' -benchtime=3s -count=3 2>&1
    echo "---"
    go test -bench='BenchmarkPythonVsGoReflection' -benchtime=3s -count=3 -benchmem 2>&1
    echo "---"
    go test -bench='BenchmarkMemPerConn' -benchtime=3s -count=3 -benchmem 2>&1
) | tee "$RESULTS_DIR/phase5_resource.txt"

echo ""

# ── Python Benchmarks ────────────────────────────────────────────────────────
echo "=== Python Benchmarks ==="
PY_CSV="$RESULTS_DIR/python_results.csv"
(
    # The Go server at :9200 should already be running from phases above
    # (via BenchmarkThroughputConcurrency which auto-starts it)
    # Start it explicitly just in case
    cd "$GO_DIR"
    go test -bench='BenchmarkThroughputConcurrency' -benchtime=1s -count=1 -run='^$' 2>/dev/null &
    GO_PID=$!
    sleep 2

    echo "Running Python benchmarks..."
    "$PY_BIN" "$PY_BENCH" --csv "$PY_CSV" --go-port 9200 2>&1

    kill $GO_PID 2>/dev/null || true
) | tee "$RESULTS_DIR/python_bench.txt"

echo ""
echo "============================================"
echo " All benchmarks complete!"
echo " Results: $RESULTS_DIR"
echo "============================================"
