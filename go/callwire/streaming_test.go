package callwire

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ── Go→Go streaming ────────────────────────────────────────────────

func TestGoToGoStreaming(t *testing.T) {
	MustExport("countUp", func(n int) chan int {
		ch := make(chan int)
		go func() {
			defer close(ch)
			for i := 1; i <= n; i++ {
				ch <- i
			}
		}()
		return ch
	})

	go Serve("localhost:9400")
	waitForPort(t, "9400", 5*time.Second)

	client, err := Connect("localhost:9400")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	results, errc := ImportStream[int](client, context.Background(), "countUp", []interface{}{5})

	var got []int
	for v := range results {
		got = append(got, v)
	}
	if err := <-errc; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 values, got %d: %v", len(got), got)
	}
	for i, v := range got {
		if v != i+1 {
			t.Fatalf("expected got[%d]=%d, got %d", i, i+1, v)
		}
	}
	t.Log("SUCCESS: Go→Go streaming countUp(5) =", got)
}

// ── Go→Python streaming ────────────────────────────────────────────

func TestGoToPythonStreaming(t *testing.T) {
	pyCode := `
@export
def count_up(n):
    for i in range(1, n + 1):
        yield i
`
	startPythonServer(t, "9401", pyCode)

	client, err := Connect("localhost:9401")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	results, errc := ImportStream[int](client, context.Background(), "count_up", []interface{}{5})

	var got []int
	for v := range results {
		got = append(got, v)
	}
	if err := <-errc; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 values, got %d: %v", len(got), got)
	}
	for i, v := range got {
		if v != i+1 {
			t.Fatalf("expected got[%d]=%d, got %d", i, i+1, v)
		}
	}
	t.Log("SUCCESS: Go→Python streaming count_up(5) =", got)
}

// ── Go→Python streaming with structs ──────────────────────────────

func TestGoToPythonStreamingComposite(t *testing.T) {
	pyCode := `
@export
def infer_stream(req, tensor):
    # Yield each element scaled by req["scale"]
    scale = float(req["scale"])
    for v in tensor:
        yield float(v) * scale
`
	startPythonServer(t, "9402", pyCode)

	type inferRequest struct {
		Scale float64 `msgpack:"scale"`
	}

	client, err := Connect("localhost:9402")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	req := inferRequest{Scale: 2.0}
	tensor := []float64{1.0, 2.0, 3.0}
	results, errc := ImportStream[float64](client, context.Background(), "infer_stream", []interface{}{req, tensor})

	var got []float64
	for v := range results {
		got = append(got, v)
	}
	if err := <-errc; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(got))
	}
	expected := []float64{2.0, 4.0, 6.0}
	for i, v := range got {
		if fmt.Sprintf("%.1f", v) != fmt.Sprintf("%.1f", expected[i]) {
			t.Fatalf("chunk %d: expected %.1f, got %.1f", i, expected[i], v)
		}
	}
	t.Log("SUCCESS: Go→Python streaming with composite args =", got)
}

// ── Python→Go streaming ────────────────────────────────────────────

func TestPythonToGoStreaming(t *testing.T) {
	MustExport("count_up_go", func(n int) chan int {
		ch := make(chan int)
		go func() {
			defer close(ch)
			for i := 1; i <= n; i++ {
				ch <- i
			}
		}()
		return ch
	})

	go Serve("localhost:9403")
	waitForPort(t, "9403", 5*time.Second)

	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	pyBin := filepath.Join("..", "..", "python", ".venv", "bin", "python")
	pyCode := fmt.Sprintf(`
import sys
sys.path.insert(0, %q)

from callwire import Client

c = Client()
c.connect("localhost", 9403)

chunks = list(c.call_stream("count_up_go", [5]))
print("CHUNKS:" + ",".join(str(x) for x in chunks))

c.close()
`, filepath.Join(repoRoot, "python"))

	cmd := exec.Command(pyBin, "-c", pyCode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python client failed: %v\n%s", err, out)
	}

	output := string(out)
	if !contains(output, "CHUNKS:1,2,3,4,5") {
		t.Fatalf("expected CHUNKS:1,2,3,4,5, got: %s", output)
	}
	t.Log("SUCCESS: Python→Go streaming works:", strings.TrimSpace(output))
}

// ── Python→Python streaming ────────────────────────────────────────

func TestPythonToPythonStreaming(t *testing.T) {
	pyServerCode := `
from callwire import export

@export
def squares(n):
    for i in range(1, n + 1):
        yield i * i
`
	startPythonServer(t, "9404", pyServerCode)

	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	pyBin := filepath.Join("..", "..", "python", ".venv", "bin", "python")
	pyCode := fmt.Sprintf(`
import sys
sys.path.insert(0, %q)

from callwire import Client

c = Client()
c.connect("localhost", 9404)

chunks = list(c.call_stream("squares", [5]))
print("SQUARES:" + ",".join(str(x) for x in chunks))

c.close()
`, filepath.Join(repoRoot, "python"))

	cmd := exec.Command(pyBin, "-c", pyCode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python client failed: %v\n%s", err, out)
	}

	output := string(out)
	if !contains(output, "SQUARES:1,4,9,16,25") {
		t.Fatalf("expected SQUARES:1,4,9,16,25, got: %s", output)
	}
	t.Log("SUCCESS: Python→Python streaming works:", strings.TrimSpace(output))
}

// ── Client reconnect ───────────────────────────────────────────────

func TestClientReconnect(t *testing.T) {
	// Start a temporary Go server, connect a client with reconnect, then
	// bounce the server and verify the client can still make calls.
	MustExport("ping_reconnect", func() string { return "pong" })

	// First listener.
	ln1, err := net.Listen("tcp", "localhost:9405")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln1.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()
	waitForPort(t, "9405", 5*time.Second)

	client, err := ConnectWithReconnect("localhost:9405")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	// First call — should succeed.
	r1, err := Import[string](client, context.Background(), "ping_reconnect", nil)
	if err != nil || r1 != "pong" {
		t.Fatalf("pre-bounce call: %v %q", err, r1)
	}
	t.Log("pre-bounce call succeeded:", r1)

	// Kill the first listener, bringing down existing connections.
	ln1.Close()
	// Give the client a moment to detect the drop.
	time.Sleep(100 * time.Millisecond)

	// Bring the server back on the same address.
	ln2, err := net.Listen("tcp", "localhost:9405")
	if err != nil {
		t.Fatalf("re-listen: %v", err)
	}
	defer ln2.Close()
	go func() {
		for {
			conn, err := ln2.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()

	// Wait for the client to reconnect.
	deadline := time.Now().Add(6 * time.Second)
	var r2 string
	for time.Now().Before(deadline) {
		r2, err = Import[string](client, context.Background(), "ping_reconnect", nil)
		if err == nil && r2 == "pong" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil || r2 != "pong" {
		t.Fatalf("post-bounce call failed after 6s: %v %q", err, r2)
	}
	t.Log("SUCCESS: client reconnect works:", r2)
}
