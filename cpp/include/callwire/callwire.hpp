// Callwire C++ SDK — header-only RAII wrapper over the C core ABI (callwire.h).
//
// Scope note: the C core ABI currently only exposes unary registration on the
// server side (callwire_server_export). Client-side streaming (server-stream,
// client-stream, bidi) is implemented in the C core and wrapped here; server-side
// streaming registration will be added once the C ABI grows it (see
// c/IMPLEMENTATION_STATUS.md).
#ifndef CALLWIRE_HPP
#define CALLWIRE_HPP

extern "C" {
#include "callwire.h"
}

#include <cstdint>
#include <cstring>
#include <functional>
#include <map>
#include <memory>
#include <stdexcept>
#include <string>
#include <tuple>
#include <type_traits>
#include <utility>
#include <vector>

namespace callwire {

// ---------------------------------------------------------------------------
// Value: a C++-idiomatic wrapper over callwire_value_t.
// ---------------------------------------------------------------------------

class Value {
public:
    Value() { raw_.type = CALLWIRE_NULL; }

    Value(std::nullptr_t) { raw_.type = CALLWIRE_NULL; }

    Value(bool b) {
        raw_.type = CALLWIRE_BOOL;
        raw_.val.is_true = b ? 1 : 0;
    }

    Value(int64_t i) {
        raw_.type = CALLWIRE_INT64;
        raw_.val.int_val = i;
    }
    Value(int i) : Value(static_cast<int64_t>(i)) {}

    Value(double d) {
        raw_.type = CALLWIRE_FLOAT64;
        raw_.val.float_val = d;
    }

    Value(const std::string &s) {
        raw_.type = CALLWIRE_STRING;
        char *copy = static_cast<char *>(malloc(s.size() + 1));
        memcpy(copy, s.data(), s.size());
        copy[s.size()] = '\0';
        raw_.val.str_val.data = copy;
        raw_.val.str_val.len = s.size();
        owns_ = true;
    }
    Value(const char *s) : Value(std::string(s)) {}

    Value(const std::vector<uint8_t> &bin) {
        raw_.type = CALLWIRE_BINARY;
        uint8_t *copy = static_cast<uint8_t *>(malloc(bin.size()));
        if (!bin.empty()) memcpy(copy, bin.data(), bin.size());
        raw_.val.bin_val.data = copy;
        raw_.val.bin_val.len = bin.size();
        owns_ = true;
    }

    // Takes the vector by value (not const&) so elements can be moved out —
    // Value is move-only (copy is deleted), so building an array/map must
    // consume its source elements rather than borrow them.
    Value(std::vector<Value> arr) {
        raw_.type = CALLWIRE_ARRAY;
        auto *items = static_cast<callwire_value_t *>(calloc(arr.size(), sizeof(callwire_value_t)));
        for (size_t i = 0; i < arr.size(); i++) {
            items[i] = arr[i].release_raw();
        }
        raw_.val.array_val.items = reinterpret_cast<struct callwire_value *>(items);
        raw_.val.array_val.count = arr.size();
        owns_ = true;
    }

    Value(std::map<std::string, Value> m) {
        raw_.type = CALLWIRE_MAP;
        auto *keys = static_cast<callwire_value_t *>(calloc(m.size(), sizeof(callwire_value_t)));
        auto *values = static_cast<callwire_value_t *>(calloc(m.size(), sizeof(callwire_value_t)));
        size_t i = 0;
        for (auto &kv : m) {
            keys[i] = Value(kv.first).release_raw();
            values[i] = kv.second.release_raw();
            i++;
        }
        raw_.val.map_val.keys = reinterpret_cast<struct callwire_value *>(keys);
        raw_.val.map_val.values = reinterpret_cast<struct callwire_value *>(values);
        raw_.val.map_val.count = m.size();
        owns_ = true;
    }

    // Wraps an existing callwire_value_t and takes ownership (will free it).
    static Value adopt(callwire_value_t raw) {
        Value v;
        v.raw_ = raw;
        v.owns_ = true;
        return v;
    }

    // Recursively duplicates a non-owned callwire_value_t (e.g. args handed
    // to a server handler by the C core's dispatch loop) into a fresh, owning
    // Value. Public so callers bridging raw C structs (like Server's
    // trampoline) don't need to reimplement this traversal.
    static Value deepCopy(const callwire_value_t &src);

    // Deep copy. RPC argument/result payloads are typically small, so a
    // recursive copy here trades a bit of performance for the ergonomics of
    // brace-init call sites (e.g. client.call("add", {Value(10), Value(20)})),
    // which std::initializer_list makes impossible for a move-only type.
    Value(const Value &other) { *this = deepCopy(other.raw_); }
    Value &operator=(const Value &other) {
        if (this != &other) {
            free_if_owned();
            *this = deepCopy(other.raw_);
        }
        return *this;
    }

    Value(Value &&other) noexcept : raw_(other.raw_), owns_(other.owns_) {
        other.owns_ = false;
        other.raw_.type = CALLWIRE_NULL;
    }
    Value &operator=(Value &&other) noexcept {
        if (this != &other) {
            free_if_owned();
            raw_ = other.raw_;
            owns_ = other.owns_;
            other.owns_ = false;
            other.raw_.type = CALLWIRE_NULL;
        }
        return *this;
    }

    ~Value() { free_if_owned(); }

    callwire_value_type_t type() const { return raw_.type; }
    bool isNull() const { return raw_.type == CALLWIRE_NULL; }

    bool asBool() const {
        require(CALLWIRE_BOOL);
        return raw_.val.is_true != 0;
    }
    int64_t asInt64() const {
        require(CALLWIRE_INT64);
        return raw_.val.int_val;
    }
    double asDouble() const {
        require(CALLWIRE_FLOAT64);
        return raw_.val.float_val;
    }
    std::string asString() const {
        require(CALLWIRE_STRING);
        return std::string(raw_.val.str_val.data, raw_.val.str_val.len);
    }
    std::vector<uint8_t> asBinary() const {
        require(CALLWIRE_BINARY);
        const uint8_t *d = raw_.val.bin_val.data;
        return std::vector<uint8_t>(d, d + raw_.val.bin_val.len);
    }

    // Generic typed accessor — as<int64_t>()/as<double>()/as<bool>()/as<std::string>()
    // dispatch to the matching asXxx() above. Lets Client::call<T>() convert
    // a result without the caller needing to name the accessor explicitly.
    template <typename T>
    T as() const;

    // Returns a non-owning view of the underlying C struct — valid only for
    // the lifetime of this Value. Used internally when passing args to the C ABI.
    const callwire_value_t &raw() const { return raw_; }

    // Releases ownership of the underlying C struct to the caller (used when
    // building a parent array/map that will own the child's memory instead).
    callwire_value_t release_raw() {
        callwire_value_t out = raw_;
        owns_ = false;
        raw_.type = CALLWIRE_NULL;
        return out;
    }

private:
    void require(callwire_value_type_t expected) const {
        if (raw_.type != expected) {
            throw std::runtime_error("callwire::Value: wrong type accessor called");
        }
    }
    void free_if_owned() {
        if (owns_) {
            callwire_value_free(&raw_);
            owns_ = false;
        }
    }

    callwire_value_t raw_{};
    bool owns_ = false;
};

inline Value Value::deepCopy(const callwire_value_t &src) {
    switch (src.type) {
        case CALLWIRE_NULL:
            return Value();
        case CALLWIRE_BOOL:
            return Value(src.val.is_true != 0);
        case CALLWIRE_INT64:
            return Value(src.val.int_val);
        case CALLWIRE_FLOAT64:
            return Value(src.val.float_val);
        case CALLWIRE_STRING:
            return Value(std::string(src.val.str_val.data, src.val.str_val.len));
        case CALLWIRE_BINARY: {
            const uint8_t *d = src.val.bin_val.data;
            return Value(std::vector<uint8_t>(d, d + src.val.bin_val.len));
        }
        case CALLWIRE_ARRAY: {
            std::vector<Value> items;
            items.reserve(src.val.array_val.count);
            auto *raw_items = reinterpret_cast<const callwire_value_t *>(src.val.array_val.items);
            for (size_t i = 0; i < src.val.array_val.count; i++) {
                items.push_back(deepCopy(raw_items[i]));
            }
            return Value(std::move(items));
        }
        case CALLWIRE_MAP: {
            std::map<std::string, Value> m;
            auto *raw_keys = reinterpret_cast<const callwire_value_t *>(src.val.map_val.keys);
            auto *raw_values = reinterpret_cast<const callwire_value_t *>(src.val.map_val.values);
            for (size_t i = 0; i < src.val.map_val.count; i++) {
                std::string key(raw_keys[i].val.str_val.data, raw_keys[i].val.str_val.len);
                m.emplace(std::move(key), deepCopy(raw_values[i]));
            }
            return Value(std::move(m));
        }
        default:
            return Value();
    }
}

template <> inline int64_t Value::as<int64_t>() const { return asInt64(); }
template <> inline double Value::as<double>() const { return asDouble(); }
template <> inline bool Value::as<bool>() const { return asBool(); }
template <> inline std::string Value::as<std::string>() const { return asString(); }

// ---------------------------------------------------------------------------
// Exceptions
// ---------------------------------------------------------------------------

class CallwireException : public std::runtime_error {
public:
    explicit CallwireException(std::string message)
        : std::runtime_error(std::move(message)) {}
};

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

class Client {
public:
    Client(const std::string &addr, int port) {
        client_ = callwire_client_connect(addr.c_str(), port);
        if (!client_) {
            throw CallwireException(std::string("connect failed: ") + callwire_error());
        }
    }

    ~Client() {
        if (client_) callwire_client_close(client_);
    }

    Client(const Client &) = delete;
    Client &operator=(const Client &) = delete;

    Client(Client &&other) noexcept : client_(other.client_) { other.client_ = nullptr; }
    Client &operator=(Client &&other) noexcept {
        if (this != &other) {
            if (client_) callwire_client_close(client_);
            client_ = other.client_;
            other.client_ = nullptr;
        }
        return *this;
    }

    // Unary call: call("add", {Value(10), Value(20)}) -> Value(30)
    Value call(const std::string &func, const std::vector<Value> &args = {}) {
        std::vector<callwire_value_t> raw_args;
        raw_args.reserve(args.size());
        for (const auto &a : args) raw_args.push_back(a.raw());

        callwire_value_t result{};
        int rc = callwire_client_call(client_, func.c_str(),
                                       raw_args.empty() ? nullptr : raw_args.data(),
                                       raw_args.size(), &result);
        if (rc != 0) {
            throw CallwireException(callwire_error());
        }
        return Value::adopt(result);
    }

    // Variadic typed call: no Value() wrapping, no braces, no .asXxx() on the
    // result — call<int64_t>("add", 10, 20) instead of
    // call("add", {Value(10), Value(20)}).asInt64().
    template <typename R, typename... Args>
    R call(const std::string &func, Args... args) {
        std::vector<Value> values;
        values.reserve(sizeof...(args));
        (values.emplace_back(Value(args)), ...);
        return call(func, values).template as<R>();
    }

    // Server-streaming: begin a stream, then call recv() repeatedly.
    // recv() returns nullopt-equivalent via the returned bool: false means
    // stream ended (no Value produced), true means chunk_out was filled.
    class ServerStream {
    public:
        // Returns true if a chunk was received (written to chunk_out), false
        // if the stream ended cleanly. Throws CallwireException on error.
        bool recv(Value &chunk_out) {
            callwire_value_t raw{};
            int rc = callwire_client_stream_recv(client_, id_, &raw);
            if (rc == 0) {
                chunk_out = Value::adopt(raw);
                return true;
            }
            if (rc == 1) return false;
            throw CallwireException(callwire_error());
        }

    private:
        friend class Client;
        ServerStream(callwire_client_t *client, uint64_t id) : client_(client), id_(id) {}
        callwire_client_t *client_;
        uint64_t id_;
    };

    ServerStream streamBegin(const std::string &func, const std::vector<Value> &args = {}) {
        std::vector<callwire_value_t> raw_args;
        raw_args.reserve(args.size());
        for (const auto &a : args) raw_args.push_back(a.raw());

        uint64_t id = callwire_client_stream_begin(client_, func.c_str(),
                                                     raw_args.empty() ? nullptr : raw_args.data(),
                                                     raw_args.size());
        if (id == 0) {
            throw CallwireException(callwire_error());
        }
        return ServerStream(client_, id);
    }

    // Client-streaming: send() repeatedly, then closeAndRecv() for the response.
    class ExportStream {
    public:
        void send(const Value &chunk) {
            callwire_value_t raw = chunk.raw();
            if (callwire_client_export_stream_send(client_, id_, &raw) != 0) {
                throw CallwireException(callwire_error());
            }
        }
        Value closeAndRecv() {
            callwire_value_t result{};
            if (callwire_client_export_stream_close(client_, id_, &result) != 0) {
                throw CallwireException(callwire_error());
            }
            return Value::adopt(result);
        }

    private:
        friend class Client;
        ExportStream(callwire_client_t *client, uint64_t id) : client_(client), id_(id) {}
        callwire_client_t *client_;
        uint64_t id_;
    };

    ExportStream exportStream(const std::string &func) {
        uint64_t id = callwire_client_export_stream_begin(client_, func.c_str());
        if (id == 0) {
            throw CallwireException(callwire_error());
        }
        return ExportStream(client_, id);
    }

    // Bidi-streaming: send()/recv() concurrently, closeSend() when done sending.
    class BidiStream {
    public:
        void send(const Value &chunk) {
            callwire_value_t raw = chunk.raw();
            if (callwire_client_bidi_stream_send(client_, id_, &raw) != 0) {
                throw CallwireException(callwire_error());
            }
        }
        bool recv(Value &chunk_out) {
            callwire_value_t raw{};
            int rc = callwire_client_bidi_stream_recv(client_, id_, &raw);
            if (rc == 0) {
                chunk_out = Value::adopt(raw);
                return true;
            }
            if (rc == 1) return false;
            throw CallwireException(callwire_error());
        }
        void closeSend() {
            if (callwire_client_bidi_stream_close_send(client_, id_) != 0) {
                throw CallwireException(callwire_error());
            }
        }

    private:
        friend class Client;
        BidiStream(callwire_client_t *client, uint64_t id) : client_(client), id_(id) {}
        callwire_client_t *client_;
        uint64_t id_;
    };

    BidiStream bidiStream(const std::string &func) {
        uint64_t id = callwire_client_bidi_stream_begin(client_, func.c_str());
        if (id == 0) {
            throw CallwireException(callwire_error());
        }
        return BidiStream(client_, id);
    }

private:
    callwire_client_t *client_ = nullptr;
};

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Handler signature: takes positional args, returns the result. Throw
// CallwireException (or any std::exception) to send an error response.
using Handler = std::function<Value(const std::vector<Value> &)>;

// Deduces a callable's argument/return types from its operator() (works for
// non-generic lambdas and std::function). Powers the typed exportFunc overload
// below so `[](int64_t a, int64_t b) { return a + b; }` doesn't need manual
// Value unpacking/wrapping.
template <typename F>
struct function_traits : function_traits<decltype(&F::operator())> {};

template <typename C, typename R, typename... Args>
struct function_traits<R (C::*)(Args...) const> {
    using ReturnType = R;
    using ArgsTuple = std::tuple<Args...>;
    static constexpr size_t arity = sizeof...(Args);
};

template <typename F, size_t... I>
Handler makeTypedHandlerImpl(F lambda, std::index_sequence<I...>) {
    using traits = function_traits<F>;
    return [lambda](const std::vector<Value> &args) -> Value {
        if (args.size() != traits::arity) {
            throw CallwireException("wrong number of arguments");
        }
        return Value(lambda(args[I].template as<typename std::tuple_element<I, typename traits::ArgsTuple>::type>()...));
    };
}

template <typename F>
Handler makeTypedHandler(F lambda) {
    return makeTypedHandlerImpl(std::move(lambda), std::make_index_sequence<function_traits<F>::arity>{});
}

// Streaming handler signatures:
// - StreamHandler (server-streaming): given args, call emit(value) per chunk.
// - ClientStreamHandler (client-streaming): given recv(), pull chunks until it
//   returns false, return the single result.
// - BidiHandler (bidi): given recv()/emit(), consume/produce in any order.
using EmitFn = std::function<void(Value)>;
using RecvFn = std::function<bool(Value &)>; // true + out param filled, false = client done
using StreamHandler = std::function<void(const std::vector<Value> &, EmitFn)>;
using ClientStreamHandler = std::function<Value(RecvFn)>;
using BidiHandler = std::function<void(RecvFn, EmitFn)>;

class Server {
public:
    Server(const std::string &addr, int port) {
        server_ = callwire_server_new(addr.c_str(), port);
        if (!server_) {
            throw CallwireException(std::string("server bind failed: ") + callwire_error());
        }
    }

    ~Server() {
        // Registered handlers are heap-allocated (see exportFunc/exportStream/
        // exportClientStream/exportBidi) and owned by this Server; free them
        // before the object goes away.
        for (auto *h : ownedHandlers_) delete h;
        for (auto *h : ownedStreamHandlers_) delete h;
        for (auto *h : ownedClientStreamHandlers_) delete h;
        for (auto *h : ownedBidiHandlers_) delete h;
        if (server_) callwire_server_close(server_);
    }

    Server(const Server &) = delete;
    Server &operator=(const Server &) = delete;

    // Registers a unary handler. Uses callwire_server_export_ctx (userdata
    // variant) so each registration gets its own std::function instance —
    // a heap-allocated Handler* passed through as userdata and cast back in
    // the trampoline — rather than requiring a single shared dispatch table
    // keyed by function name (which callwire_server_export's plain fn_ptr
    // signature, with no userdata, can't support for closures).
    void exportFunc(const std::string &name, Handler handler) {
        auto *heapHandler = new Handler(std::move(handler));
        ownedHandlers_.push_back(heapHandler);

        int rc = callwire_server_export_ctx(server_, name.c_str(), heapHandler, &Server::trampoline);
        if (rc != 0) {
            throw CallwireException(std::string("export failed: ") + callwire_error());
        }
    }

    // Typed overload: [](int64_t a, int64_t b) { return a + b; } instead of
    // [](const std::vector<Value>& args) -> Value { return Value(args[0].asInt64() + ...); }
    // SFINAE'd out when F is already callable as (const std::vector<Value>&) -> Value
    // (i.e. already matches the untyped Handler signature above) so existing
    // untyped call sites keep resolving to the non-template overload instead
    // of colliding with this one.
    template <typename F,
              typename std::enable_if<!std::is_invocable_r<Value, F, const std::vector<Value> &>::value, int>::type = 0>
    void exportFunc(const std::string &name, F lambda) {
        exportFunc(name, makeTypedHandler(std::move(lambda)));
    }

    // Server-streaming: [](const vector<Value>& args, EmitFn emit) { emit(Value(1)); emit(Value(2)); }
    void exportStream(const std::string &name, StreamHandler handler) {
        auto *heapHandler = new StreamHandler(std::move(handler));
        ownedStreamHandlers_.push_back(heapHandler);

        int rc = callwire_server_export_stream_ctx(server_, name.c_str(), heapHandler, &Server::streamTrampoline);
        if (rc != 0) {
            throw CallwireException(std::string("exportStream failed: ") + callwire_error());
        }
    }

    // Client-streaming: [](RecvFn recv) -> Value { Value v; int64_t sum=0;
    //   while (recv(v)) sum += v.asInt64(); return Value(sum); }
    void exportClientStream(const std::string &name, ClientStreamHandler handler) {
        auto *heapHandler = new ClientStreamHandler(std::move(handler));
        ownedClientStreamHandlers_.push_back(heapHandler);

        int rc = callwire_server_export_client_stream_ctx(server_, name.c_str(), heapHandler,
                                                            &Server::clientStreamTrampoline);
        if (rc != 0) {
            throw CallwireException(std::string("exportClientStream failed: ") + callwire_error());
        }
    }

    // Bidi: [](RecvFn recv, EmitFn emit) { Value v; while (recv(v)) emit(Value(v.asInt64()*2)); }
    void exportBidi(const std::string &name, BidiHandler handler) {
        auto *heapHandler = new BidiHandler(std::move(handler));
        ownedBidiHandlers_.push_back(heapHandler);

        int rc = callwire_server_export_bidi_ctx(server_, name.c_str(), heapHandler, &Server::bidiTrampoline);
        if (rc != 0) {
            throw CallwireException(std::string("exportBidi failed: ") + callwire_error());
        }
    }

    void serve() {
        int rc = callwire_server_serve(server_);
        if (rc != 0) {
            throw CallwireException(std::string("serve failed: ") + callwire_error());
        }
    }

    void close() {
        if (server_) {
            callwire_server_close(server_);
            server_ = nullptr;
        }
    }

private:
    // C-linkage trampoline: userdata is the heap-allocated Handler* for this
    // specific registration (set via callwire_server_export_ctx), so each
    // exported function gets its own correctly-routed closure — no shared
    // dispatch table or function-name lookup needed at this layer.
    static int trampoline(void *userdata, callwire_value_t *args, size_t args_count, callwire_value_t *result_out) {
        auto *handler = static_cast<Handler *>(userdata);
        std::vector<Value> cpp_args;
        cpp_args.reserve(args_count);
        // Args are owned by the caller (server.c's dispatch_request frees
        // msg->args after this call returns), so deep-copy each into an
        // owning Value rather than adopting the C core's memory.
        for (size_t i = 0; i < args_count; i++) {
            cpp_args.push_back(Value::deepCopy(args[i]));
        }

        try {
            Value result = (*handler)(cpp_args);
            *result_out = result.release_raw();
            return 0;
        } catch (const std::exception &) {
            result_out->type = CALLWIRE_NULL;
            return -1;
        }
    }

    static int streamTrampoline(void *userdata, callwire_value_t *args, size_t args_count,
                                 callwire_stream_emit_fn emit, void *emit_ctx) {
        auto *handler = static_cast<StreamHandler *>(userdata);
        std::vector<Value> cpp_args;
        cpp_args.reserve(args_count);
        for (size_t i = 0; i < args_count; i++) {
            cpp_args.push_back(Value::deepCopy(args[i]));
        }

        EmitFn emitFn = [emit, emit_ctx](Value v) {
            callwire_value_t raw = v.release_raw();
            emit(emit_ctx, &raw);
            callwire_value_free(&raw);
        };

        try {
            (*handler)(cpp_args, emitFn);
            return 0;
        } catch (const std::exception &) {
            return -1;
        }
    }

    static int clientStreamTrampoline(void *userdata, callwire_stream_recv_fn recv, void *recv_ctx,
                                       callwire_value_t *result_out) {
        auto *handler = static_cast<ClientStreamHandler *>(userdata);

        RecvFn recvFn = [recv, recv_ctx](Value &out) -> bool {
            callwire_value_t raw{};
            int rc = recv(recv_ctx, &raw);
            if (rc == 0) {
                out = Value::adopt(raw);
                return true;
            }
            return false; // rc == 1 (clean end) or rc == -1 (error) both stop the loop
        };

        try {
            Value result = (*handler)(recvFn);
            *result_out = result.release_raw();
            return 0;
        } catch (const std::exception &) {
            result_out->type = CALLWIRE_NULL;
            return -1;
        }
    }

    static int bidiTrampoline(void *userdata, callwire_stream_recv_fn recv, void *recv_ctx,
                               callwire_stream_emit_fn emit, void *emit_ctx) {
        auto *handler = static_cast<BidiHandler *>(userdata);

        RecvFn recvFn = [recv, recv_ctx](Value &out) -> bool {
            callwire_value_t raw{};
            int rc = recv(recv_ctx, &raw);
            if (rc == 0) {
                out = Value::adopt(raw);
                return true;
            }
            return false;
        };
        EmitFn emitFn = [emit, emit_ctx](Value v) {
            callwire_value_t raw = v.release_raw();
            emit(emit_ctx, &raw);
            callwire_value_free(&raw);
        };

        try {
            (*handler)(recvFn, emitFn);
            return 0;
        } catch (const std::exception &) {
            return -1;
        }
    }

    callwire_server_t *server_ = nullptr;
    std::vector<Handler *> ownedHandlers_;
    std::vector<StreamHandler *> ownedStreamHandlers_;
    std::vector<ClientStreamHandler *> ownedClientStreamHandlers_;
    std::vector<BidiHandler *> ownedBidiHandlers_;
};

}  // namespace callwire

#endif  // CALLWIRE_HPP
