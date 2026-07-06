package callwire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Test data generators ───────────────────────────────────────────────────

type testStruct10 struct {
	A int     `msgpack:"a"`
	B string  `msgpack:"b"`
	C float64 `msgpack:"c"`
	D bool    `msgpack:"d"`
	E int64   `msgpack:"e"`
	F string  `msgpack:"f"`
	G float64 `msgpack:"g"`
	H bool    `msgpack:"h"`
	I int32   `msgpack:"i"`
	J string  `msgpack:"j"`
}

type testStruct50 struct {
	A01 int     `msgpack:"a01"`
	A02 string  `msgpack:"a02"`
	A03 float64 `msgpack:"a03"`
	A04 bool    `msgpack:"a04"`
	A05 int64   `msgpack:"a05"`
	A06 string  `msgpack:"a06"`
	A07 float64 `msgpack:"a07"`
	A08 bool    `msgpack:"a08"`
	A09 int32   `msgpack:"a09"`
	A10 string  `msgpack:"a10"`
	A11 int     `msgpack:"a11"`
	A12 string  `msgpack:"a12"`
	A13 float64 `msgpack:"a13"`
	A14 bool    `msgpack:"a14"`
	A15 int64   `msgpack:"a15"`
	A16 string  `msgpack:"a16"`
	A17 float64 `msgpack:"a17"`
	A18 bool    `msgpack:"a18"`
	A19 int32   `msgpack:"a19"`
	A20 string  `msgpack:"a20"`
	A21 int     `msgpack:"a21"`
	A22 string  `msgpack:"a22"`
	A23 float64 `msgpack:"a23"`
	A24 bool    `msgpack:"a24"`
	A25 int64   `msgpack:"a25"`
	A26 string  `msgpack:"a26"`
	A27 float64 `msgpack:"a27"`
	A28 bool    `msgpack:"a28"`
	A29 int32   `msgpack:"a29"`
	A30 string  `msgpack:"a30"`
	A31 int     `msgpack:"a31"`
	A32 string  `msgpack:"a32"`
	A33 float64 `msgpack:"a33"`
	A34 bool    `msgpack:"a34"`
	A35 int64   `msgpack:"a35"`
	A36 string  `msgpack:"a36"`
	A37 float64 `msgpack:"a37"`
	A38 bool    `msgpack:"a38"`
	A39 int32   `msgpack:"a39"`
	A40 string  `msgpack:"a40"`
	A41 int     `msgpack:"a41"`
	A42 string  `msgpack:"a42"`
	A43 float64 `msgpack:"a43"`
	A44 bool    `msgpack:"a44"`
	A45 int64   `msgpack:"a45"`
	A46 string  `msgpack:"a46"`
	A47 float64 `msgpack:"a47"`
	A48 bool    `msgpack:"a48"`
	A49 int32   `msgpack:"a49"`
	A50 string  `msgpack:"a50"`
}

func genNestedMap(depth int) map[string]interface{} {
	if depth <= 0 {
		return map[string]interface{}{"leaf": true}
	}
	return map[string]interface{}{
		"level": depth,
		"next":  genNestedMap(depth - 1),
	}
}

func genBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func newStruct50() testStruct50 {
	var s testStruct50
	v := reflect.ValueOf(&s).Elem()
	for i := 0; i < 50; i++ {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Int, reflect.Int32, reflect.Int64:
			f.SetInt(int64(i + 1))
		case reflect.String:
			f.SetString(fmt.Sprintf("field-%d", i+1))
		case reflect.Float64:
			f.SetFloat(float64(i+1) * 1.5)
		case reflect.Bool:
			f.SetBool(i%2 == 0)
		}
	}
	return s
}

var (
	benchSrvOnce  sync.Once
	benchSrvAddr  = "localhost:9200"
	benchPyPort   = "9201"
)

func startBenchGoServerOnce(b testing.TB) {
	benchSrvOnce.Do(func() {
		Export("noop", func() {})
		Export("echo", func(x int) int { return x })
		Export("echoStr", func(s string) string { return s })
		Export("add", func(a, b int) int { return a + b })
		Export("errFn", func() error { return &WireError{ErrorType: "ValueError", Message: "bench-error"} })
		Export("slow", func(delayMs int) int {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
			return delayMs
		})
		Export("bigResult", func(size int) string {
			return strings.Repeat("x", size)
		})
		Export("manyArgs", func(args ...int) int {
			s := 0
			for _, a := range args {
				s += a
			}
			return s
		})
		Export("nestedMap", func(depth int) map[string]interface{} {
			return genNestedMap(depth)
		})
		Export("echo10", func(s testStruct10) testStruct10 { return s })
		Export("echo50", func(s testStruct50) testStruct50 { return s })
		Export("fib20", func() int {
			a, b := 0, 1
			for i := 0; i < 20; i++ {
				a, b = b, a+b
			}
			return b
		})
		Export("fib30", func() int {
			a, b := 0, 1
			for i := 0; i < 30; i++ {
				a, b = b, a+b
			}
			return b
		})
		Export("jsonMarshal", func() string {
			data, _ := json.Marshal(map[string]int{"a": 1, "b": 2, "c": 3})
			return string(data)
		})
		Export("sleep1ms", func() { time.Sleep(time.Millisecond) })
		Export("jsonResult", func() string {
			data, _ := json.Marshal(map[string]interface{}{
				"id": 1, "name": "test", "tags": []string{"a", "b"},
			})
			return string(data)
		})

		go Serve(benchSrvAddr)
	})
}

func waitForBenchPort(t testing.TB, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", addr)
}

func startBenchPythonServer(t testing.TB, port string, code string) {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	pyBin := filepath.Join(repoRoot, "python", ".venv", "bin", "python")

	code = fmt.Sprintf(`
import sys
sys.path.insert(0, %q)

from callwire.server import export
%s

import time; time.sleep(3600)
`, filepath.Join(repoRoot, "python"), code)

	cmd := exec.Command(pyBin, "-c", code)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start python bench server: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitForBenchPort(t, "localhost:"+port, 10*time.Second)
}

func benchClient(t testing.TB, addr string) *Client {
	t.Helper()
	c, err := Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// ── Phase 1: Microbenchmarks ────────────────────────────────────────────────

func BenchmarkEncodeRequest(b *testing.B) {
	benches := []struct {
		name string
		args []interface{}
	}{
		{"no-args", []interface{}{}},
		{"1-arg", []interface{}{42}},
		{"5-args", []interface{}{1, "two", 3.0, true, "five"}},
		{"10-args", func() []interface{} {
			a := make([]interface{}, 10)
			for i := range a {
				a[i] = i
			}
			return a
		}()},
		{"100-args", func() []interface{} {
			a := make([]interface{}, 100)
			for i := range a {
				a[i] = i
			}
			return a
		}()},
	}
	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				encodeRequest(1, "fn", bb.args)
			}
		})
	}
}

func BenchmarkEncodeResponse(b *testing.B) {
	s10 := testStruct10{A: 1, B: "hi", C: 3.14, D: true, E: 99, F: "x", G: 2.71, H: false, I: 42, J: "end"}
	benches := []struct {
		name string
		res  interface{}
	}{
		{"int", 42},
		{"string-small", "hello"},
		{"string-1KB", strings.Repeat("x", 1024)},
		{"string-64KB", strings.Repeat("x", 64*1024)},
		{"struct-10", s10},
		{"array-100", func() []interface{} {
			a := make([]interface{}, 100)
			for i := range a {
				a[i] = i
			}
			return a
		}()},
	}
	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				encodeResponse(1, bb.res)
			}
		})
	}
}

func BenchmarkDecodeMessage(b *testing.B) {
	payloads := map[string][]byte{}
	payloads["request-no-args"], _ = encodeRequest(1, "fn", []interface{}{})
	payloads["response-int"], _ = encodeResponse(1, 42)
	payloads["response-string-1KB"], _ = encodeResponse(1, strings.Repeat("x", 1024))
	payloads["error"], _ = encodeError(1, "ValueError", "something went wrong")

	for name, p := range payloads {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				decodeMessage(p)
			}
		})
	}
}

func BenchmarkDispatch(b *testing.B) {
	Export("bench_double", func(x int) int { return x * 2 })

	payload, _ := encodeRequest(1, "bench_double", []interface{}{21})

	b.Run("success", func(b *testing.B) {
		b.ReportAllocs()
		msg, _ := decodeMessage(payload)
		conn := &mockConn{buf: bytes.NewBuffer(nil)}
		for i := 0; i < b.N; i++ {
			dispatch(conn, msg)
		}
	})

	b.Run("not-found", func(b *testing.B) {
		b.ReportAllocs()
		msg, _ := decodeMessage(payload)
		msg.Func = "bench_nonexistent"
		conn := &mockConn{buf: bytes.NewBuffer(nil)}
		for i := 0; i < b.N; i++ {
			dispatch(conn, msg)
		}
	})
}

func BenchmarkFraming(b *testing.B) {
	sizes := []struct {
		name string
		n    int
	}{
		{"100B", 100},
		{"1KB", 1024},
		{"64KB", 64 * 1024},
	}
		for _, s := range sizes {
		b.Run(s.name, func(b *testing.B) {
			data := genBytes(s.n)
			var buf bytes.Buffer
			conn := &mockConn{buf: &buf}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				writeFrame(conn, data)
				readFrame(conn)
			}
		})
	}
}

func BenchmarkEncodeDecodeRoundtrip(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p, _ := encodeRequest(1, "fn", []interface{}{42, "hello"})
		decodeMessage(p)
	}
}

func BenchmarkAllocations(b *testing.B) {
	b.Run("frame-roundtrip", func(b *testing.B) {
		var buf bytes.Buffer
		conn := &mockConn{buf: &buf}
		data := []byte("hello-bench")
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buf.Reset()
			writeFrame(conn, data)
			readFrame(conn)
		}
	})
	b.Run("encode-decode-roundtrip", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			p, _ := encodeRequest(1, "fn", []interface{}{42, "hello"})
			decodeMessage(p)
		}
	})
}

// ── Phase 2: Point-to-Point Latency ─────────────────────────────────────────

func BenchmarkLatencyGoToGo(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	benches := []struct {
		name string
		fn   func()
	}{
		{"noop", func() { _, _ = Import[int, int](client, "noop", nil) }},
		{"echo-int", func() { _, _ = Import[int, int](client, "echo", []interface{}{42}) }},
		{"echo-string-10B", func() {
			_, _ = Import[string, string](client, "echoStr", []interface{}{"hello world"})
		}},
		{"echo-string-1KB", func() {
			_, _ = Import[string, string](client, "echoStr", []interface{}{strings.Repeat("x", 1024)})
		}},
		{"add", func() { _, _ = Import[int, int](client, "add", []interface{}{10, 20}) }},
		{"error-path", func() { _, _ = Import[int, int](client, "errFn", nil) }},
	}

	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bb.fn()
			}
		})
	}
}

func BenchmarkLatencyGoToPy(b *testing.B) {
	pyCode := `
from callwire import export

@export
def noop():
    pass

@export
def echo(x):
    return x

@export
def echo_str(s):
    return s

@export
def add(a, b):
    return a + b

@export
def raise_error():
    raise ValueError("bench-error")
`
	startBenchPythonServer(b, benchPyPort, pyCode)
	client := benchClient(b, "localhost:"+benchPyPort)

	benches := []struct {
		name string
		fn   func()
	}{
		{"noop", func() { _, _ = Import[int, int](client, "noop", nil) }},
		{"echo-int", func() { _, _ = Import[int, int](client, "echo", []interface{}{42}) }},
		{"echo-string", func() {
			_, _ = Import[string, string](client, "echo_str", []interface{}{"hello"})
		}},
		{"add", func() { _, _ = Import[int, int](client, "add", []interface{}{10, 20}) }},
		{"error", func() { _, _ = Import[int, int](client, "raise_error", nil) }},
	}

	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bb.fn()
			}
		})
	}
}

func BenchmarkLatencyDistribution(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	b.Run("echo-int", func(b *testing.B) {
		var total time.Duration
		min := time.Hour
		var max time.Duration
		start := time.Now()

		for i := 0; i < b.N; i++ {
			t0 := time.Now()
			Import[int, int](client, "echo", []interface{}{42})
			elapsed := time.Since(t0)
			total += elapsed
			if elapsed < min {
				min = elapsed
			}
			if elapsed > max {
				max = elapsed
			}
		}

		b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "calls/sec")
		b.ReportMetric(float64(min.Microseconds()), "min-us")
		b.ReportMetric(float64(max.Microseconds()), "max-us")
		b.ReportMetric(float64(total.Microseconds())/float64(b.N), "mean-us")
	})
}

// ── Phase 3: Throughput & Concurrency ────────────────────────────────────────

func BenchmarkThroughputConcurrency(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)

	levels := []int{1, 5, 10, 50, 100}
	for _, n := range levels {
		b.Run(fmt.Sprintf("workers-%d", n), func(b *testing.B) {
			client := benchClient(b, benchSrvAddr)
			var wg sync.WaitGroup
			work := make(chan struct{})

			b.ResetTimer()
			for w := 0; w < n; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range work {
						Import[int, int](client, "noop", nil)
					}
				}()
			}
			for i := 0; i < b.N; i++ {
				work <- struct{}{}
			}
			close(work)
			wg.Wait()
		})
	}
}

func BenchmarkSharedConnVsMultiConn(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)

	b.Run("shared-conn", func(b *testing.B) {
		c := benchClient(b, benchSrvAddr)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				Import[int, int](c, "noop", nil)
			}
		})
	})

	b.Run("per-goroutine-conn", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			c := benchClient(b, benchSrvAddr)
			for pb.Next() {
				Import[int, int](c, "noop", nil)
			}
		})
	})
}

func BenchmarkHeadOfLineBlocking(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	b.Run("no-contention", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Import[int, int](client, "noop", nil)
		}
	})

	b.Run("with-slow-call", func(b *testing.B) {
		var slowOnce sync.Once
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				slow := false
				slowOnce.Do(func() { slow = true })
				if slow {
					Import[int, int](client, "slow", []interface{}{10})
				} else {
					Import[int, int](client, "noop", nil)
				}
			}
		})
	})
}

func BenchmarkFuncComplexity(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	benches := []struct {
		name string
		fn   func()
	}{
		{"noop", func() { _, _ = Import[int, int](client, "noop", nil) }},
		{"fib-20", func() { _, _ = Import[int, int](client, "fib20", nil) }},
		{"fib-30", func() { _, _ = Import[int, int](client, "fib30", nil) }},
		{"json-marshal", func() { _, _ = Import[string, string](client, "jsonMarshal", nil) }},
		{"sleep-1ms", func() { _, _ = Import[int, int](client, "sleep1ms", nil) }},
	}
	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bb.fn()
			}
		})
	}
}

// ── Phase 4: Payload & Argument Variation ────────────────────────────────────

func BenchmarkArgCount(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	counts := []int{0, 1, 5, 20, 100}
	for _, n := range counts {
		b.Run(fmt.Sprintf("%d-args", n), func(b *testing.B) {
			args := make([]interface{}, n)
			for i := range args {
				args[i] = i
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Import[int, int](client, "manyArgs", args)
			}
		})
	}
}

func BenchmarkResultSize(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	sizes := []int{0, 256, 1024, 16 * 1024, 256 * 1024, 1024 * 1024}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("%dB", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Import[int, string](client, "bigResult", []interface{}{n})
			}
			b.StopTimer()
			b.ReportMetric(float64(n), "bytes/result")
		})
	}
}

func BenchmarkStructRoundTrip(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	s10 := testStruct10{A: 1, B: "hello", C: 3.14, D: true, E: 999, F: "world", G: 2.71, H: false, I: 42, J: "end"}
	s50 := newStruct50()

	b.Run("10-field", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Import[testStruct10, testStruct10](client, "echo10", []interface{}{s10})
		}
	})

	b.Run("50-field", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Import[testStruct50, testStruct50](client, "echo50", []interface{}{s50})
		}
	})
}

func BenchmarkStringSize(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	sizes := []int{10, 1024, 64 * 1024, 512 * 1024}
	for _, n := range sizes {
		b.Run(fmt.Sprintf("%dB", n), func(b *testing.B) {
			payload := strings.Repeat("x", n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Import[string, string](client, "echoStr", []interface{}{payload})
			}
			b.StopTimer()
			b.ReportMetric(float64(n), "bytes/arg")
		})
	}
}

func BenchmarkNestedData(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	depths := []int{1, 3, 5}
	for _, d := range depths {
		b.Run(fmt.Sprintf("depth-%d", d), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Import[int, map[string]interface{}](client, "nestedMap", []interface{}{d})
			}
		})
	}
}

// ── Phase 5: Resource & Stability ────────────────────────────────────────────

func BenchmarkConnChurn(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := Connect(benchSrvAddr)
		if err != nil {
			b.Fatal(err)
		}
		Import[int, int](c, "noop", nil)
		c.Close()
	}
}

func BenchmarkRegistrySize(b *testing.B) {
	names := []int{1, 10, 100, 1000}
	for _, n := range names {
		b.Run(fmt.Sprintf("%d-funcs", n), func(b *testing.B) {
			registryMu.Lock()
			saved := registry
			registry = make(map[string]interface{}, n)
			for i := 0; i < n; i++ {
				registry[fmt.Sprintf("fn_%d", i)] = func(x int) int { return x }
			}
			registryMu.Unlock()

			payload, _ := encodeRequest(1, "fn_0", []interface{}{42})
			msg, _ := decodeMessage(payload)
			conn := &mockConn{buf: bytes.NewBuffer(nil)}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dispatch(conn, msg)
			}

			registryMu.Lock()
			registry = saved
			registryMu.Unlock()
		})
	}
}

func BenchmarkRefWrapper(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	b.Run("Import", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			Import[int, int](client, "add", []interface{}{3, 4})
		}
	})

	b.Run("RefWithClient", func(b *testing.B) {
		add := RefWithClient[int](client, "add")
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			add(3, 4)
		}
	})
}

func BenchmarkRefSeamless(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	Configure("localhost", 9200)

	b.Run("Ref", func(b *testing.B) {
		echo := Ref[int]("echo")
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			echo(42)
		}
	})
}

func BenchmarkGoroutineCount(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)

	for _, n := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("%d-conns", n), func(b *testing.B) {
			conns := make([]*Client, n)
			for i := range conns {
				c, err := Connect(benchSrvAddr)
				if err != nil {
					b.Fatal(err)
				}
				conns[i] = c
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Import[int, int](conns[i%n], "noop", nil)
			}
			b.StopTimer()
			for _, c := range conns {
				c.Close()
			}
		})
	}
}

func BenchmarkPythonVsGoReflection(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)
	client := benchClient(b, benchSrvAddr)

	b.Run("fib-10", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Import[int, int](client, "fib20", nil)
		}
	})
	b.Run("fib-30", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			Import[int, int](client, "fib30", nil)
		}
	})
}

func BenchmarkMemPerConn(b *testing.B) {
	startBenchGoServerOnce(b)
	waitForBenchPort(b, benchSrvAddr, 5*time.Second)

	b.Run("connect+close", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			c, err := Connect(benchSrvAddr)
			if err != nil {
				b.Fatal(err)
			}
			c.Close()
		}
	})
}
