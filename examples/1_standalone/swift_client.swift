// Swift client calling a local server (any language) on localhost:9090.
//
// Build: same pattern as swift_server.swift — see its header comment,
// or swift/build.sh for a working reference invocation.
// Run: ./swift_client
import Callwire

let client = try! Client(host: "localhost", port: 9090)

let result = try! client.call("add", args: [.int64(10), .int64(20)])
print("add(10, 20) = \(result)")

let greeting = try! client.call("greet", args: [.string("World")])
print("greet(\"World\") = \(greeting)")

client.close()
