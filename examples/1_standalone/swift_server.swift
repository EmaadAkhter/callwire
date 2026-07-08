// Swift server exporting "add", "greet" functions.
// Can be called from Go, Python, Rust, TypeScript, Java, C++, or Swift clients.
//
// Build (see swift/README.md/build.sh for why not `swift build`):
//   swiftc -parse-as-library -Iswift/Sources/CCallwire/include \
//     -c swift/Sources/Callwire/Callwire.swift -o /tmp/Callwire.o \
//     -module-name Callwire -emit-module -emit-module-path /tmp/Callwire.swiftmodule
//   for f in codec framing client server errors; do
//     clang -c -std=c99 -Iswift/Sources/CCallwire/include -Iswift/Sources/CCallwire \
//       swift/Sources/CCallwire/$f.c -o /tmp/$f.o
//   done
//   swiftc -Iswift/Sources/CCallwire/include -I/tmp -c \
//     examples/1_standalone/swift_server.swift -o /tmp/swift_server.o
//   swiftc /tmp/Callwire.o /tmp/swift_server.o /tmp/codec.o /tmp/framing.o \
//     /tmp/client.o /tmp/server.o /tmp/errors.o -o swift_server
//
// Run: ./swift_server
import Callwire
import Dispatch
import Darwin

let server = try! Server(host: "0.0.0.0", port: 9090)

try! server.exportTyped("add") { (a: Int64, b: Int64) in a + b }
try! server.exportTyped("greet") { (name: String) in "Hello, \(name)!" }

print("Callwire Swift server listening on :9090")
try! server.serve()
