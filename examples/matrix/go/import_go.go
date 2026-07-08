// Go import script: calls "add"(10,20) on every OTHER language's matrix
// export server (best-effort — prints SKIP for any port that isn't
// reachable, since not all 9 servers necessarily run at once).
package main

import (
	"fmt"

	"github.com/emaad/callwire"
)

func main() {
	targets := map[string]int{
		"python": 9102,
		"rust":   9103,
		"ts":     9104,
		"java":   9105,
		"c":      9106,
		"cpp":    9107,
		"swift":  9108,
		"cobol":  9109,
	}

	for name, port := range targets {
		client, err := callwire.Connect(fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			fmt.Printf("%-8s SKIP (not running: %v)\n", name, err)
			continue
		}
		result, err := callwire.RefWithClient[int64](client, "add")(int64(10), int64(20))
		if err != nil {
			fmt.Printf("%-8s SKIP (call failed: %v)\n", name, err)
			client.Close()
			continue
		}
		fmt.Printf("%-8s OK  add(10,20) = %d\n", name, result)
		client.Close()
	}
}
