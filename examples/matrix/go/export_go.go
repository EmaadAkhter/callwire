// Go export script: exports "add" on a fixed port via init() (a real Go
// language feature — runs automatically before main()).
//
// Calls callwire.Serve() explicitly rather than relying on Export()'s
// auto-serve — auto-serve is gated on CALLWIRE_SPAWNED (unset = auto-start,
// "1" = don't), and the orchestrator (callwire.toml + init()) sets
// CALLWIRE_SPAWNED=1 on every spawned worker for its own registry-worker
// bookkeeping. Under the orchestrator, auto-serve would silently no-op and
// this server would never bind. An explicit Serve() call works either way.
package main

import (
	"fmt"
	"os"

	"github.com/emaad/callwire"
)

const matrixPort = 9101

func init() {
	// Force off Export()'s auto-serve (which would otherwise bind :9090,
	// the package default) — this script drives serving explicitly via
	// the fixed matrixPort below instead.
	os.Setenv("CALLWIRE_AUTO", "0")
	callwire.Export("add", func(a, b int64) int64 { return a + b })
}

func main() {
	fmt.Printf("Go matrix export listening on :%d\n", matrixPort)
	if err := callwire.Serve(fmt.Sprintf("0.0.0.0:%d", matrixPort)); err != nil {
		panic(err)
	}
}
