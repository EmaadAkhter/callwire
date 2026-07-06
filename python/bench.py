"""Callwire Python-side benchmarks.

Usage: python bench.py [--csv /tmp/callwire_py_bench.csv]
"""

import sys
import time
import csv
import os
import argparse
from callwire import export, configure, ref, Client

# ── exports for Go → Python benchmarks ──────────────────────────────────────

@export
def py_noop():
    pass

@export
def py_echo(x):
    return x

@export
def py_echo_str(s):
    return s

@export
def py_add(a, b):
    return a + b

@export
def py_raise_error():
    raise ValueError("bench-error")

@export
def py_big_result(size):
    return "x" * size

@export
def py_nested_map(depth):
    m = {}
    for i in range(depth):
        m[str(i)] = {"level": i, "data": list(range(10))}
    return m

@export
def py_slow(delay_ms):
    time.sleep(delay_ms / 1000.0)
    return delay_ms


def run_bench(name, fn, n=5000):
    """Run a timing benchmark. Returns (name, n, total_s, mean_us, min_us, max_us)."""
    # warmup
    for _ in range(100):
        fn()

    min_t = float("inf")
    max_t = 0.0
    total = 0.0

    for _ in range(n):
        t0 = time.perf_counter()
        fn()
        elapsed = (time.perf_counter() - t0) * 1e6  # microseconds
        total += elapsed
        if elapsed < min_t:
            min_t = elapsed
        if elapsed > max_t:
            max_t = elapsed

    mean = total / n
    return (name, n, total / 1e6, mean, min_t, max_t)


def bench_pack_unpack():
    from callwire.codec import pack_request, unpack
    import msgpack

    results = []

    # pack microbench
    for name, args in [
        ("pack-0-args", []),
        ("pack-1-arg", [42]),
        ("pack-5-args", [1, "two", 3.0, True, "five"]),
        ("pack-10-args", list(range(10))),
    ]:
        n = 20000
        t0 = time.perf_counter()
        for _ in range(n):
            pack_request(1, "fn", args)
        total = time.perf_counter() - t0
        mean_us = (total / n) * 1e6
        results.append((f"py_{name}", n, total, mean_us, 0, 0))
        print(f"  {name}: {mean_us:.2f} us")

    # unpack microbench
    payload = pack_request(1, "fn", [42, "hello"])
    n = 20000
    t0 = time.perf_counter()
    for _ in range(n):
        unpack(payload)
    total = time.perf_counter() - t0
    mean_us = (total / n) * 1e6
    results.append(("py_unpack", n, total, mean_us, 0, 0))
    print(f"  unpack: {mean_us:.2f} us")

    return results


def bench_py_to_py():
    """Python client → Python server (localhost, same process)."""
    configure(host="localhost", port=9203)
    # server is auto-started by @export above

    results = []
    client = Client()
    client.connect("localhost", 9203)

    benches = [
        ("py2py-noop", lambda: client.call("py_noop", [])),
        ("py2py-echo-int", lambda: client.call("py_echo", [42])),
        ("py2py-echo-str", lambda: client.call("py_echo_str", ["hello"])),
        ("py2py-add", lambda: client.call("py_add", [10, 20])),
        ("py2py-error", lambda: client.call("py_raise_error", [])),
    ]

    for name, fn in benches:
        if "error" in name:
            def wrap(fn=fn):
                try:
                    fn()
                except Exception:
                    pass
            r = run_bench(name, wrap, n=2000)
        else:
            r = run_bench(name, fn, n=5000)
        results.append(r)
        print(f"  {name}: {r[3]:.2f} us (min={r[4]:.2f} max={r[5]:.2f})")

    client.close()
    return results


def bench_py_to_go(go_port="9200"):
    """Python client → Go server."""
    client = Client()
    client.connect("localhost", go_port)

    results = []
    benches = [
        ("py2go-noop", lambda: client.call("noop", [])),
        ("py2go-echo-int", lambda: client.call("echo", [42])),
        ("py2go-echo-str", lambda: client.call("echoStr", ["hello"])),
        ("py2go-add", lambda: client.call("add", [10, 20])),
        ("py2go-error", lambda: client.call("errFn", [])),
        ("py2go-big-1KB", lambda: client.call("bigResult", [1024])),
    ]

    for name, fn in benches:
        if "error" in name:
            def wrap(fn=fn):
                try:
                    fn()
                except Exception:
                    pass
            r = run_bench(name, wrap, n=2000)
        else:
            r = run_bench(name, fn, n=5000)
        results.append(r)
        print(f"  {name}: {r[3]:.2f} us (min={r[4]:.2f} max={r[5]:.2f})")

    client.close()
    return results


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--csv", default="/tmp/callwire_py_bench.csv")
    parser.add_argument("--go-port", default="9200")
    parser.add_argument("--skip-py2go", action="store_true")
    args = parser.parse_args()

    all_results = []

    print("=== Python Benchmarks ===")

    print("\n--- Phase 1: Pack/Unpack ---")
    all_results.extend(bench_pack_unpack())

    print("\n--- Phase 2: Py→Py Latency ---")
    all_results.extend(bench_py_to_py())

    if not args.skip_py2go:
        print(f"\n--- Phase 2: Py→Go Latency (port {args.go_port}) ---")
        all_results.extend(bench_py_to_go(args.go_port))

    # Write CSV
    with open(args.csv, "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["name", "n", "total_s", "mean_us", "min_us", "max_us"])
        for r in all_results:
            w.writerow(r)

    print(f"\nResults written to {args.csv}")


if __name__ == "__main__":
    main()
