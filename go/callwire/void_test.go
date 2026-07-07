package callwire

import (
	"context"
	"testing"
	"time"
)

func TestVoidFunction(t *testing.T) {
	MustExport("noop_void", func() {})
	MustExport("noop_int", func() int { return 42 })
	go Serve("localhost:9283")
	waitForBenchPort(t, "localhost:9283", 5*time.Second)

	client, err := Connect("localhost:9283")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	t.Log("calling noop_int...")
	r, err := Import[int](client, context.Background(), "noop_int", nil)
	t.Logf("noop_int: result=%d err=%v", r, err)

	t.Log("calling noop_void...")
	r2, err2 := Import[int](client, context.Background(), "noop_void", nil)
	t.Logf("noop_void: result=%d err=%v", r2, err2)
}