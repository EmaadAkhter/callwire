// Go export script: exports "add" on a fixed port, auto-starts via Go's own
// init() (a real language feature — runs automatically before main(), no
// explicit setup call needed by the caller).
package main

import (
	"fmt"

	"github.com/emaad/callwire"
)

const matrixPort = 9101

func init() {
	callwire.Configure("0.0.0.0", matrixPort)
	callwire.Export("add", func(a, b int64) int64 { return a + b })
}

func main() {
	fmt.Printf("Go matrix export listening on :%d\n", matrixPort)
	select {} // block forever; callwire.Export already started the server
}
