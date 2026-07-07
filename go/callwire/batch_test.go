package callwire

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

// ── unit tests (in-process dispatch, no network) ──────────────────────────

func TestBatchUnit(t *testing.T) {
	MustExport("batchAdd", func(a, b int) int { return a + b })
	MustExport("batchEcho", func(s string) string { return "echo:" + s })

	go Serve("localhost:19800")
	waitForPort(t, "19800", 5*time.Second)

	c, err := Connect("localhost:19800")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	results := Batch(c, context.Background(), []BatchCall{
		{Func: "batchAdd", Args: []interface{}{3, 4}},
		{Func: "batchEcho", Args: []interface{}{"hello"}},
		{Func: "batchAdd", Args: []interface{}{10, 20}},
	})

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// results[0]: add(3,4) = 7
	if results[0].Err != nil {
		t.Fatalf("results[0] error: %v", results[0].Err)
	}
	// msgpack decodes integers as int8/int64 depending on value; normalise via fmt
	if fmt.Sprint(results[0].Result) != "7" {
		t.Fatalf("results[0]: expected 7, got %v (%T)", results[0].Result, results[0].Result)
	}
	// results[1]: echo("hello") = "echo:hello"
	if results[1].Err != nil {
		t.Fatalf("results[1] error: %v", results[1].Err)
	}
	if results[1].Result != "echo:hello" {
		t.Fatalf("results[1]: expected echo:hello, got %v", results[1].Result)
	}
	// results[2]: add(10,20) = 30
	if results[2].Err != nil {
		t.Fatalf("results[2] error: %v", results[2].Err)
	}
	if fmt.Sprint(results[2].Result) != "30" {
		t.Fatalf("results[2]: expected 30, got %v", results[2].Result)
	}
	t.Log("SUCCESS: Batch G→G untyped: results =", results[0].Result, results[1].Result, results[2].Result)
}

func TestBatchTyped(t *testing.T) {
	MustExport("batchSq", func(x int) int { return x * x })

	go Serve("localhost:19801")
	waitForPort(t, "19801", 5*time.Second)

	c, err := Connect("localhost:19801")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	calls := make([]BatchCall, 5)
	for i := range calls {
		calls[i] = BatchCall{Func: "batchSq", Args: []interface{}{i + 1}}
	}
	values, errs := BatchT[int](c, context.Background(), calls)

	expected := []int{1, 4, 9, 16, 25}
	for i, v := range values {
		if errs[i] != nil {
			t.Fatalf("call %d error: %v", i, errs[i])
		}
		if v != expected[i] {
			t.Fatalf("call %d: expected %d, got %d", i, expected[i], v)
		}
	}
	t.Log("SUCCESS: BatchT G→G typed: [1,4,9,16,25] =", values)
}

func TestBatchErrorHandling(t *testing.T) {
	MustExport("batchOk", func(x int) int { return x })
	MustExport("batchFail", func() (int, error) {
		return 0, &WireError{ErrorType: "ValueError", Message: "intentional"}
	})

	go Serve("localhost:19802")
	waitForPort(t, "19802", 5*time.Second)

	c, err := Connect("localhost:19802")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	results := Batch(c, context.Background(), []BatchCall{
		{Func: "batchOk", Args: []interface{}{42}},
		{Func: "batchFail"},
		{Func: "batchOk", Args: []interface{}{99}},
		{Func: "doesNotExist"},
	})

	if results[0].Err != nil {
		t.Fatalf("call 0 should succeed: %v", results[0].Err)
	}
	if results[1].Err == nil {
		t.Fatal("call 1 should fail")
	}
	var we *WireError
	if !as(results[1].Err, &we) || we.ErrorType != "ValueError" {
		t.Fatalf("call 1: expected ValueError, got %T: %v", results[1].Err, results[1].Err)
	}
	if results[2].Err != nil {
		t.Fatalf("call 2 should succeed: %v", results[2].Err)
	}
	if results[3].Err == nil {
		t.Fatal("call 3 (not-found) should fail")
	}
	var nf *WireError
	if !as(results[3].Err, &nf) || nf.ErrorType != "NotFoundError" {
		t.Fatalf("call 3: expected NotFoundError, got %T: %v", results[3].Err, results[3].Err)
	}
	t.Log("SUCCESS: BatchErrorHandling — partial failure handled correctly")
}

func TestBatchContextCancel(t *testing.T) {
	MustExport("batchSlow", func(ms int) int {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return ms
	})

	go Serve("localhost:19803")
	waitForPort(t, "19803", 5*time.Second)

	c, err := Connect("localhost:19803")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Each call sleeps 200ms but context cancels after 50ms.
	calls := []BatchCall{
		{Func: "batchSlow", Args: []interface{}{200}},
		{Func: "batchSlow", Args: []interface{}{200}},
	}
	results := Batch(c, ctx, calls)

	cancelled := 0
	for _, r := range results {
		if r.Err != nil {
			cancelled++
		}
	}
	if cancelled == 0 {
		t.Fatal("expected at least one call to be cancelled")
	}
	t.Logf("SUCCESS: BatchContextCancel — %d/%d calls cancelled", cancelled, len(calls))
}

// ── cross-language: Go client → Python server batch ──────────────────────

func TestBatchGoToPython(t *testing.T) {
	pyCode := `
@export
def mul(a, b):
    return a * b

@export
def upper(s):
    return s.upper()
`
	startPythonServer(t, "19804", pyCode)

	c, err := Connect("localhost:19804")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	results := Batch(c, context.Background(), []BatchCall{
		{Func: "mul", Args: []interface{}{6, 7}},
		{Func: "upper", Args: []interface{}{"callwire"}},
	})
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("call %d: %v", i, r.Err)
		}
	}
	if fmt.Sprint(results[0].Result) != "42" {
		t.Fatalf("mul: expected 42, got %v", results[0].Result)
	}
	if results[1].Result != "CALLWIRE" {
		t.Fatalf("upper: expected CALLWIRE, got %v", results[1].Result)
	}
	t.Log("SUCCESS: Batch G→P:", results[0].Result, results[1].Result)
}

// ── cross-language: Python client → Go server batch ──────────────────────

func TestBatchPythonToGo(t *testing.T) {
	MustExport("batchDouble", func(x int) int { return x * 2 })
	MustExport("batchLen", func(s string) int { return len(s) })

	go Serve("localhost:19805")
	waitForPort(t, "19805", 5*time.Second)

	pyCode := `
import sys
sys.path.insert(0, %q)

from callwire import Client

c = Client()
c.connect("localhost", 19805)

results = c.batch([
    ("batchDouble", [21]),
    ("batchLen", ["hello"]),
])
print("R0:" + str(results[0]))
print("R1:" + str(results[1]))
c.close()
`
	pyBin := pyBinPath()
	pyP := pyPath()
	code := fmt.Sprintf(pyCode, pyP)

	cmd := exec.Command(pyBin, "-c", code)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("P→G batch: %v\n%s", err, out)
	}
	if !contains(string(out), "R0:42") {
		t.Fatalf("P→G: expected R0:42, got:\n%s", out)
	}
	if !contains(string(out), "R1:5") {
		t.Fatalf("P→G: expected R1:5, got:\n%s", out)
	}
	t.Logf("SUCCESS: Batch P→G:\n%s", out)
}

