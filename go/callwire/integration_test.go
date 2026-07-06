package callwire

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func startPythonServer(t *testing.T, port string, pythonCode string) {
	t.Helper()
	pyBin := filepath.Join("..", "..", "python", ".venv", "bin", "python")
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	code := fmt.Sprintf(`
import sys
sys.path.insert(0, %q)

from callwire.server import export, serve

%s

serve("localhost", %s)
`, filepath.Join(repoRoot, "python"), pythonCode, port)

	cmd := exec.Command(pyBin, "-c", code)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start python server: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	waitForPort(t, port, 5*time.Second)
}

func waitForPort(t *testing.T, port string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "localhost:"+port, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for port %s", port)
}

func TestIntegrationSuccess(t *testing.T) {
	pyCode := `
@export
def double(x):
    return x * 2
`
	startPythonServer(t, "9192", pyCode)

	client, err := Connect("localhost:9192")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	result, err := Import[int, int](client, "double", []interface{}{21})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
	t.Log("SUCCESS: Go client called Python double(21) =", result)
}

func TestIntegrationError(t *testing.T) {
	pyCode := `
@export
def crash():
    raise ValueError("boom")
`
	startPythonServer(t, "9193", pyCode)

	client, err := Connect("localhost:9193")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	_, err = Import[int, int](client, "crash", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var wireErr *WireError
	if !as(err, &wireErr) {
		t.Fatalf("expected *WireError, got %T: %v", err, err)
	}
	if wireErr.ErrorType != "ValueError" {
		t.Fatalf("expected ValueError, got %s", wireErr.ErrorType)
	}
	if wireErr.Message != "boom" {
		t.Fatalf("expected 'boom', got %s", wireErr.Message)
	}
	t.Log("SUCCESS: error path works:", err)
}

func TestIntegrationNotFound(t *testing.T) {
	pyCode := `
@export
def exists():
    return "yes"
`
	startPythonServer(t, "9194", pyCode)

	client, err := Connect("localhost:9194")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	_, err = Import[string, string](client, "does_not_exist", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var wireErr *WireError
	if !as(err, &wireErr) {
		t.Fatalf("expected *WireError, got %T: %v", err, err)
	}
	if wireErr.ErrorType != "NotFoundError" {
		t.Fatalf("expected NotFoundError, got %s", wireErr.ErrorType)
	}
	t.Log("SUCCESS: not-found path works:", err)
}

func TestIntegrationConcurrency(t *testing.T) {
	pyCode := `
import time

@export
def slow_add(a, b):
    time.sleep(0.05)
    return a + b
`
	startPythonServer(t, "9195", pyCode)

	client, err := Connect("localhost:9195")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	results := make(chan struct{ id int; val int; err error }, 10)
	for i := 0; i < 10; i++ {
		i := i
		go func() {
			r, e := Import[int, int](client, "slow_add", []interface{}{i, i})
			results <- struct {
				id  int
				val int
				err error
			}{i, r, e}
		}()
	}

	seen := make(map[int]int)
	for i := 0; i < 10; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("call %d failed: %v", r.id, r.err)
			continue
		}
		seen[r.id] = r.val
	}
	for i := 0; i < 10; i++ {
		if seen[i] != i*2 {
			t.Errorf("call %d: expected %d, got %d", i, i*2, seen[i])
		}
	}
	t.Log("SUCCESS:", len(seen), "concurrent calls all returned correct results")
}

// ── Go→Go integration ──────────────────────────────────────────────

func TestGoToGoIntegration(t *testing.T) {
	Export("square", func(x int) int { return x * x })
	Export("adderr", func() (int, error) {
		return 0, &WireError{ErrorType: "ValueError", Message: "nope"}
	})

	go Serve("localhost:9196")
	waitForPort(t, "9196", 5*time.Second)

	client, err := Connect("localhost:9196")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	result, err := Import[int, int](client, "square", []interface{}{9})
	if err != nil {
		t.Fatalf("square: %v", err)
	}
	if result != 81 {
		t.Fatalf("expected 81, got %d", result)
	}
	t.Log("SUCCESS: Go→Go square(9) =", result)

	_, err = Import[int, int](client, "adderr", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var wireErr *WireError
	if !as(err, &wireErr) {
		t.Fatalf("expected *WireError, got %T", err)
	}
	if wireErr.ErrorType != "ValueError" {
		t.Fatalf("expected ValueError, got %s", wireErr.ErrorType)
	}
	t.Log("SUCCESS: Go→Go error path:", err)
}

// ── Python→Go integration ──────────────────────────────────────────

func TestPythonToGoIntegration(t *testing.T) {
	Export("greet", func(name string) string {
		return "hello " + name
	})

	go Serve("localhost:9197")
	waitForPort(t, "9197", 5*time.Second)

	pyCode := `
import sys
sys.path.insert(0, %q)

from callwire import Client

c = Client()
c.connect("localhost", 9197)

result = c.call("greet", ["world"])
print("RESULT:" + str(result))

try:
    c.call("nope", [])
except Exception as e:
    print("ERROR:" + str(e))

c.close()
`
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	pyBin := filepath.Join("..", "..", "python", ".venv", "bin", "python")
	code := fmt.Sprintf(pyCode, filepath.Join(repoRoot, "python"))
	cmd := exec.Command(pyBin, "-c", code)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python client failed: %v\n%s", err, out)
	}

	output := string(out)
	if !contains(output, "RESULT:hello world") {
		t.Fatalf("unexpected output: %s", output)
	}
	if !contains(output, "NotFoundError") {
		t.Fatalf("expected error, got: %s", output)
	}
	t.Log("SUCCESS: Python→Go works")
}

// ── Python→Python integration ──────────────────────────────────────

func TestPythonToPythonIntegration(t *testing.T) {
	pyServerCode := `
from callwire import export

@export
def reverse(s):
    return s[::-1]
`
	startPythonServer(t, "9198", pyServerCode)

	pyClientCode := `
import sys
sys.path.insert(0, %q)

from callwire import Client

c = Client()
c.connect("localhost", 9198)

result = c.call("reverse", ["hello"])
print("RESULT:" + str(result))

try:
    c.call("nope", [])
except Exception as e:
    print("ERROR:" + str(e))

c.close()
`
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	pyBin := filepath.Join("..", "..", "python", ".venv", "bin", "python")
	code := fmt.Sprintf(pyClientCode, filepath.Join(repoRoot, "python"))
	cmd := exec.Command(pyBin, "-c", code)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python client failed: %v\n%s", err, out)
	}

	output := string(out)
	if !contains(output, "RESULT:olleh") {
		t.Fatalf("unexpected output: %s", output)
	}
	if !contains(output, "NotFoundError") {
		t.Fatalf("expected error, got: %s", output)
	}
	t.Log("SUCCESS: Python→Python works")
}

// small helper since testify isn't a dependency
func as[T any](err error, target *T) bool {
	e, ok := err.(T)
	if ok {
		*target = e
	}
	return ok
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
