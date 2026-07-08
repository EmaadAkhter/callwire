import Callwire
import Dispatch
import Darwin

// End-to-end Swift SDK test: real server + real client over TCP.
// Not XCTest-based (SPM's test runner needs the working manifest execution
// path, which is broken in this environment's toolchain — see
// swift/Sources/CCallwire/README.md's build notes). Run directly as an
// executable instead; assertion failures cause a nonzero exit.

func assertEqual(_ a: Value, _ b: Value, _ msg: String) {
    guard a == b else {
        print("FAIL: \(msg) — \(a) != \(b)")
        exit(1)
    }
}

let port: Int32 = 19399

let server = try! Server(host: "0.0.0.0", port: port)

try! server.export("add") { args in
    guard case .int64(let a) = args[0], case .int64(let b) = args[1] else {
        throw CallwireError(message: "bad args")
    }
    return .int64(a + b)
}

try! server.export("greet") { args in
    guard case .string(let name) = args[0] else {
        throw CallwireError(message: "bad args")
    }
    return .string("Hello, \(name)!")
}

var counter: Int64 = 0
try! server.export("counter") { _ in
    counter += 1
    return .int64(counter)
}

// Typed export overloads — no .int64(let a) pattern-matching, no .int64(...) wrapping.
try! server.exportTyped("addTyped") { (a: Int64, b: Int64) in a + b }
try! server.exportTyped("greetTyped") { (name: String) in "Hello, \(name)!" }

DispatchQueue.global().async {
    try? server.serve()
}
usleep(300_000)

let client = try! Client(host: "127.0.0.1", port: port)

let addResult = try! client.call("add", args: [.int64(10), .int64(20)])
assertEqual(addResult, .int64(30), "add(10,20)")
print("test_unary_add: OK")

let greetResult = try! client.call("greet", args: [.string("World")])
assertEqual(greetResult, .string("Hello, World!"), "greet(World)")
print("test_unary_greet: OK")

// Dynamic call sugar: client.addTyped(10, 20) — no .call("addTyped", args: [...]).
let dynamicSum = try! client.addTyped(10, 20)
assertEqual(dynamicSum, .int64(30), "dynamic addTyped(10,20)")
print("test_dynamic_call_add: OK (\(dynamicSum))")

let dynamicGreeting = try! client.greetTyped("World")
assertEqual(dynamicGreeting, .string("Hello, World!"), "dynamic greetTyped(World)")
print("test_dynamic_call_greet: OK (\(dynamicGreeting))")

let c1 = try! client.call("counter")
let c2 = try! client.call("counter")
assertEqual(c1, .int64(1), "counter first call")
assertEqual(c2, .int64(2), "counter second call")
print("test_closure_state: OK (counter reached \(c2))")

do {
    _ = try client.call("nonexistent")
    print("FAIL: expected CallwireError")
    exit(1)
} catch let e as CallwireError {
    print("test_not_found: OK (\(e.message))")
}

client.close()
server.close()

print("All Swift loopback tests passed.")
