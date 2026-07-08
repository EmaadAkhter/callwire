// Swift import script: calls "add"(10,20) on every OTHER language's matrix
// export server (best-effort — SKIP if a port isn't reachable).
import Callwire

func `init`() {} // no setup needed for a pure client script

`init`()

let targets: [(String, Int32)] = [
    ("go", 9101), ("python", 9102), ("rust", 9103), ("ts", 9104),
    ("java", 9105), ("c", 9106), ("cpp", 9107), ("cobol", 9109),
]

for (name, port) in targets {
    do {
        let client = try Client(host: "127.0.0.1", port: port)
        let result = try client.add(10, 20)
        print("\(name)\tOK  add(10,20) = \(result)")
        client.close()
    } catch {
        print("\(name)\tSKIP (\(error))")
    }
}
