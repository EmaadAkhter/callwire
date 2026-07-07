package main

import (
	"context"
	"fmt"
	"log"

	"github.com/emaad/callwire"
)

// In Callwire, any normal Go function can be exported as an RPC method.
// Once exported, Callwire automatically spins up the server in the background!

func add(a, b int) int {
	return a + b
}

func greet(name string) string {
	return fmt.Sprintf("Hello, %s! Welcome to Callwire.", name)
}

func main() {
	// Register the functions.
	callwire.Export("add", add)
	callwire.Export("greet", greet)

	log.Println("Callwire Go RPC server registered functions: add, greet")

	// Init handles worker and orchestrator orchestration modes.
	handle, err := callwire.Init(context.Background())
	if err != nil {
		log.Fatalf("Callwire init failed: %v", err)
	}

	if handle != nil {
		defer handle.Shutdown()
		handle.Wait()
	} else {
		// Standalone mode or worker mode: keep the main thread alive.
		select {}
	}
}
