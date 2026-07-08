import CCallwire

// MARK: - Value

/// A Callwire RPC value: mirrors `callwire_value_t`'s tagged union.
public enum Value: Sendable, Equatable {
    case null
    case bool(Bool)
    case int64(Int64)
    case float64(Double)
    case string(String)
    case binary([UInt8])
    case array([Value])
    case map([String: Value])
}

public struct CallwireError: Error, CustomStringConvertible {
    public let message: String
    public init(message: String) { self.message = message }
    public var description: String { message }
}

// MARK: - Raw value marshalling
//
// callwire_value_t owns heap memory for strings/binary/array/map contents
// (freed via callwire_value_free). These helpers build a C struct from a
// Swift Value (caller must eventually free it) and decode a C struct into
// a Swift Value (deep copy, does not take ownership — caller still owns
// and must free the source struct).

func makeRawValue(_ value: Value) -> callwire_value_t {
    var raw = callwire_value_t()
    switch value {
    case .null:
        raw.type = CALLWIRE_NULL
    case .bool(let b):
        raw.type = CALLWIRE_BOOL
        raw.val.is_true = b ? 1 : 0
    case .int64(let i):
        raw.type = CALLWIRE_INT64
        raw.val.int_val = i
    case .float64(let d):
        raw.type = CALLWIRE_FLOAT64
        raw.val.float_val = d
    case .string(let s):
        raw.type = CALLWIRE_STRING
        let utf8 = Array(s.utf8)
        let buf = UnsafeMutablePointer<CChar>.allocate(capacity: utf8.count + 1)
        for (i, byte) in utf8.enumerated() { buf[i] = CChar(bitPattern: byte) }
        buf[utf8.count] = 0
        raw.val.str_val.data = UnsafePointer(buf)
        raw.val.str_val.len = utf8.count
    case .binary(let bytes):
        raw.type = CALLWIRE_BINARY
        let buf = UnsafeMutablePointer<UInt8>.allocate(capacity: max(bytes.count, 1))
        for (i, b) in bytes.enumerated() { buf[i] = b }
        raw.val.bin_val.data = UnsafePointer(buf)
        raw.val.bin_val.len = bytes.count
    case .array(let items):
        raw.type = CALLWIRE_ARRAY
        let count = items.count
        let buf = UnsafeMutablePointer<callwire_value_t>.allocate(capacity: max(count, 1))
        for (i, item) in items.enumerated() { buf[i] = makeRawValue(item) }
        raw.val.array_val.items = buf.withMemoryRebound(to: callwire_value.self, capacity: max(count, 1)) { $0 }
        raw.val.array_val.count = count
    case .map(let m):
        raw.type = CALLWIRE_MAP
        let count = m.count
        let keysBuf = UnsafeMutablePointer<callwire_value_t>.allocate(capacity: max(count, 1))
        let valsBuf = UnsafeMutablePointer<callwire_value_t>.allocate(capacity: max(count, 1))
        for (i, kv) in m.enumerated() {
            keysBuf[i] = makeRawValue(.string(kv.key))
            valsBuf[i] = makeRawValue(kv.value)
        }
        raw.val.map_val.keys = keysBuf.withMemoryRebound(to: callwire_value.self, capacity: max(count, 1)) { $0 }
        raw.val.map_val.values = valsBuf.withMemoryRebound(to: callwire_value.self, capacity: max(count, 1)) { $0 }
        raw.val.map_val.count = count
    }
    return raw
}

func decodeValue(_ raw: callwire_value_t) -> Value {
    switch raw.type {
    case CALLWIRE_NULL:
        return .null
    case CALLWIRE_BOOL:
        return .bool(raw.val.is_true != 0)
    case CALLWIRE_INT64:
        return .int64(raw.val.int_val)
    case CALLWIRE_FLOAT64:
        return .float64(raw.val.float_val)
    case CALLWIRE_STRING:
        let ptr = UnsafeRawPointer(raw.val.str_val.data)!.assumingMemoryBound(to: UInt8.self)
        let bytes = UnsafeBufferPointer(start: ptr, count: raw.val.str_val.len)
        return .string(String(decoding: bytes, as: UTF8.self))
    case CALLWIRE_BINARY:
        let ptr = raw.val.bin_val.data!
        let bytes = UnsafeBufferPointer(start: ptr, count: raw.val.bin_val.len)
        return .binary([UInt8](bytes))
    case CALLWIRE_ARRAY:
        let count = raw.val.array_val.count
        let items = UnsafeRawPointer(raw.val.array_val.items)!
            .assumingMemoryBound(to: callwire_value_t.self)
        var result: [Value] = []
        result.reserveCapacity(count)
        for i in 0..<count { result.append(decodeValue(items[i])) }
        return .array(result)
    case CALLWIRE_MAP:
        let count = raw.val.map_val.count
        let keys = UnsafeRawPointer(raw.val.map_val.keys)!
            .assumingMemoryBound(to: callwire_value_t.self)
        let values = UnsafeRawPointer(raw.val.map_val.values)!
            .assumingMemoryBound(to: callwire_value_t.self)
        var result: [String: Value] = [:]
        for i in 0..<count {
            if case .string(let k) = decodeValue(keys[i]) {
                result[k] = decodeValue(values[i])
            }
        }
        return .map(result)
    default:
        return .null
    }
}

func lastError() -> String {
    String(cString: callwire_error())
}

// MARK: - Client

/// Callwire client. One RPC in flight per connection at a time — matches the
/// C core's client.c documented simplification (no reader thread/pending map
/// yet); open multiple Client instances for concurrent calls.
public final class Client: @unchecked Sendable {
    private var handle: OpaquePointer?

    public init(host: String, port: Int32) throws {
        guard let h = callwire_client_connect(host, port) else {
            throw CallwireError(message: "connect failed: \(lastError())")
        }
        handle = h
    }

    deinit {
        if let h = handle { callwire_client_close(h) }
    }

    public func close() {
        if let h = handle {
            callwire_client_close(h)
            handle = nil
        }
    }

    /// Unary call.
    public func call(_ function: String, args: [Value] = []) throws -> Value {
        guard let h = handle else { throw CallwireError(message: "client is closed") }

        var rawArgs = args.map { makeRawValue($0) }
        defer { for i in 0..<rawArgs.count { withUnsafeMutablePointer(to: &rawArgs[i]) { callwire_value_free($0) } } }

        var result = callwire_value_t()
        let rc = rawArgs.withUnsafeMutableBufferPointer { buf -> Int32 in
            callwire_client_call(h, function, buf.baseAddress, buf.count, &result)
        }
        guard rc == 0 else {
            throw CallwireError(message: lastError())
        }
        defer { withUnsafeMutablePointer(to: &result) { callwire_value_free($0) } }
        return decodeValue(result)
    }

    /// Server-streaming: begin a stream, then call `next()` repeatedly until it returns nil.
    public func streamBegin(_ function: String, args: [Value] = []) throws -> ServerStream {
        guard let h = handle else { throw CallwireError(message: "client is closed") }

        var rawArgs = args.map { makeRawValue($0) }
        defer { for i in 0..<rawArgs.count { withUnsafeMutablePointer(to: &rawArgs[i]) { callwire_value_free($0) } } }

        let id = rawArgs.withUnsafeMutableBufferPointer { buf -> UInt64 in
            callwire_client_stream_begin(h, function, buf.baseAddress, buf.count)
        }
        guard id != 0 else { throw CallwireError(message: lastError()) }
        return ServerStream(handle: h, id: id)
    }

    public final class ServerStream: @unchecked Sendable {
        private let handle: OpaquePointer
        private let id: UInt64
        fileprivate init(handle: OpaquePointer, id: UInt64) { self.handle = handle; self.id = id }

        /// Returns the next chunk, or nil when the stream has ended cleanly.
        public func next() throws -> Value? {
            var chunk = callwire_value_t()
            let rc = callwire_client_stream_recv(handle, id, &chunk)
            if rc == 0 {
                defer { withUnsafeMutablePointer(to: &chunk) { callwire_value_free($0) } }
                return decodeValue(chunk)
            } else if rc == 1 {
                return nil
            } else {
                throw CallwireError(message: lastError())
            }
        }
    }

    /// Client-streaming: send chunks, then closeAndRecv() for the single response.
    public func exportStream(_ function: String) throws -> ExportStream {
        guard let h = handle else { throw CallwireError(message: "client is closed") }
        let id = callwire_client_export_stream_begin(h, function)
        guard id != 0 else { throw CallwireError(message: lastError()) }
        return ExportStream(handle: h, id: id)
    }

    public final class ExportStream: @unchecked Sendable {
        private let handle: OpaquePointer
        private let id: UInt64
        fileprivate init(handle: OpaquePointer, id: UInt64) { self.handle = handle; self.id = id }

        public func send(_ value: Value) throws {
            var raw = makeRawValue(value)
            defer { withUnsafeMutablePointer(to: &raw) { callwire_value_free($0) } }
            guard callwire_client_export_stream_send(handle, id, &raw) == 0 else {
                throw CallwireError(message: lastError())
            }
        }

        public func closeAndRecv() throws -> Value {
            var result = callwire_value_t()
            guard callwire_client_export_stream_close(handle, id, &result) == 0 else {
                throw CallwireError(message: lastError())
            }
            defer { withUnsafeMutablePointer(to: &result) { callwire_value_free($0) } }
            return decodeValue(result)
        }
    }

    /// Bidi-streaming: send/receive concurrently, closeSend() when done sending.
    public func bidiStream(_ function: String) throws -> BidiStream {
        guard let h = handle else { throw CallwireError(message: "client is closed") }
        let id = callwire_client_bidi_stream_begin(h, function)
        guard id != 0 else { throw CallwireError(message: lastError()) }
        return BidiStream(handle: h, id: id)
    }

    public final class BidiStream: @unchecked Sendable {
        private let handle: OpaquePointer
        private let id: UInt64
        fileprivate init(handle: OpaquePointer, id: UInt64) { self.handle = handle; self.id = id }

        public func send(_ value: Value) throws {
            var raw = makeRawValue(value)
            defer { withUnsafeMutablePointer(to: &raw) { callwire_value_free($0) } }
            guard callwire_client_bidi_stream_send(handle, id, &raw) == 0 else {
                throw CallwireError(message: lastError())
            }
        }

        public func next() throws -> Value? {
            var chunk = callwire_value_t()
            let rc = callwire_client_bidi_stream_recv(handle, id, &chunk)
            if rc == 0 {
                defer { withUnsafeMutablePointer(to: &chunk) { callwire_value_free($0) } }
                return decodeValue(chunk)
            } else if rc == 1 {
                return nil
            } else {
                throw CallwireError(message: lastError())
            }
        }

        public func closeSend() throws {
            guard callwire_client_bidi_stream_close_send(handle, id) == 0 else {
                throw CallwireError(message: lastError())
            }
        }
    }
}

// MARK: - Server

public typealias Handler = @Sendable ([Value]) throws -> Value

/// Callwire server. Registers unary handlers via the C core's userdata-carrying
/// export (`callwire_server_export_ctx`), same mechanism the C++ SDK uses —
/// each registration gets its own routed Swift closure, not a shared
/// name-keyed dispatch table.
public final class Server: @unchecked Sendable {
    private var handle: OpaquePointer?
    private var boxes: [HandlerBox] = []

    // A class wrapper around the closure so it has stable identity to pass
    // as `Unmanaged` userdata across the C boundary.
    private final class HandlerBox {
        let handler: Handler
        init(_ handler: @escaping Handler) { self.handler = handler }
    }

    public init(host: String, port: Int32) throws {
        guard let h = callwire_server_new(host, port) else {
            throw CallwireError(message: "server bind failed: \(lastError())")
        }
        handle = h
    }

    deinit {
        if let h = handle { callwire_server_close(h) }
    }

    public func export(_ name: String, handler: @escaping Handler) throws {
        guard let h = handle else { throw CallwireError(message: "server is closed") }
        let box = HandlerBox(handler)
        boxes.append(box) // keep alive for the server's lifetime

        let userdata = Unmanaged.passUnretained(box).toOpaque()
        let rc = callwire_server_export_ctx(h, name, userdata) { userdataRaw, args, argsCount, resultOut in
            guard let userdataRaw else { return -1 }
            let box = Unmanaged<HandlerBox>.fromOpaque(userdataRaw).takeUnretainedValue()

            var swiftArgs: [Value] = []
            if let args {
                swiftArgs.reserveCapacity(argsCount)
                for i in 0..<argsCount { swiftArgs.append(decodeValue(args[i])) }
            }

            do {
                let result = try box.handler(swiftArgs)
                resultOut?.pointee = makeRawValue(result)
                return 0
            } catch {
                resultOut?.pointee = makeRawValue(.null)
                return -1
            }
        }
        guard rc == 0 else { throw CallwireError(message: "export failed: \(lastError())") }
    }

    /// Blocks until the server is closed or an error occurs.
    public func serve() throws {
        guard let h = handle else { throw CallwireError(message: "server is closed") }
        guard callwire_server_serve(h) == 0 else {
            throw CallwireError(message: "serve failed: \(lastError())")
        }
    }

    public func close() {
        if let h = handle {
            callwire_server_close(h)
            handle = nil
        }
    }
}
