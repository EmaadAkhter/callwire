package callwire

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestServiceDiscovery(t *testing.T) {
	// Disable auto-serve for this test.
	os.Setenv("CALLWIRE_AUTO", "0")
	defer os.Unsetenv("CALLWIRE_AUTO")

	// Start registry server on a random port.
	regListener, err := ServeRegistry("127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start registry server: %v", err)
	}
	defer regListener.Close()
	regAddr := regListener.Addr().String()

	// Register some functions on a worker server.
	Export("say_hello", func(name string) (string, error) {
		return "Hello, " + name, nil
	})

	// Start worker server on a dynamic port.
	workerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen worker: %v", err)
	}
	workerAddr := workerListener.Addr().String()
	workerListener.Close()
	
	go func() {
		_ = Serve(workerAddr)
	}()
	time.Sleep(50 * time.Millisecond) // let worker start

	// Register worker with registry.
	err = RegisterWith(regAddr, "hello-service", workerAddr)
	if err != nil {
		t.Fatalf("failed to register hello-service: %v", err)
	}

	// Create discover pool.
	pool, err := NewDiscoverPool(regAddr, "hello-service")
	if err != nil {
		t.Fatalf("failed to create discover pool: %v", err)
	}
	defer pool.Close()

	sayHello := DiscoverRef[string](pool, "say_hello")
	reply, err := sayHello("World")
	if err != nil {
		t.Fatalf("RPC call failed: %v", err)
	}
	if reply != "Hello, World" {
		t.Errorf("expected 'Hello, World', got %q", reply)
	}
}


