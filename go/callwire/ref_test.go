package callwire

import (
	"testing"
	"time"
)

func TestRefIntegration(t *testing.T) {
	Export("double", func(x int) int { return x * 2 })
	Export("greet", func(name string) string { return "hi " + name })

	go Serve("localhost:9199")
	waitForPort(t, "9199", 5*time.Second)

	client, err := Connect("localhost:9199")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	double := Ref[int](client, "double")
	result, err := double(21)
	if err != nil {
		t.Fatal(err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
	t.Log("Ref[int](double)(21) =", result)

	greet := Ref[string](client, "greet")
	result2, err2 := greet("world")
	if err2 != nil {
		t.Fatal(err2)
	}
	if result2 != "hi world" {
		t.Fatalf("expected 'hi world', got %s", result2)
	}
	t.Log("Ref[string](greet)(\"world\") =", result2)
}
