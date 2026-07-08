// Swift export script: exports "add" on a fixed port. init() performs setup
// and is called first thing at the top level (Swift top-level code runs
// sequentially, so this is the script's entry point).
import Callwire

let matrixPort: Int32 = 9108
var server: Server!

func `init`() {
    server = try! Server(host: "0.0.0.0", port: matrixPort)
    try! server.exportTyped("add") { (a: Int64, b: Int64) in a + b }
}

`init`()
print("Swift matrix export listening on :\(matrixPort)")
try! server.serve()
