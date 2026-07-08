#ifndef CALLWIRE_H
#define CALLWIRE_H

#ifdef __cplusplus
extern "C" {
#endif

#include <stdint.h>
#include <stddef.h>

/* Opaque types for client and server connections. */
typedef struct callwire_client callwire_client_t;
typedef struct callwire_server callwire_server_t;

/* Tagged union for msgpack values. Mirrors Python's dynamic value handling. */
typedef enum {
    CALLWIRE_NULL,
    CALLWIRE_BOOL,
    CALLWIRE_INT64,
    CALLWIRE_FLOAT64,
    CALLWIRE_STRING,
    CALLWIRE_BINARY,
    CALLWIRE_ARRAY,
    CALLWIRE_MAP,
} callwire_value_type_t;

typedef struct callwire_value {
    callwire_value_type_t type;
    union {
        int is_true;                   /* CALLWIRE_BOOL */
        int64_t int_val;                /* CALLWIRE_INT64 */
        double float_val;               /* CALLWIRE_FLOAT64 */
        struct {
            const char *data;
            size_t len;
        } str_val;                      /* CALLWIRE_STRING */
        struct {
            const uint8_t *data;
            size_t len;
        } bin_val;                      /* CALLWIRE_BINARY */
        struct {
            struct callwire_value *items;
            size_t count;
        } array_val;                    /* CALLWIRE_ARRAY */
        struct {
            struct callwire_value *keys;
            struct callwire_value *values;
            size_t count;
        } map_val;                      /* CALLWIRE_MAP */
    } val;
} callwire_value_t;

/* Wire message envelope (mirrors Python/Go/Rust wireMessage). */
typedef struct {
    uint64_t id;
    const char *type;                   /* "request", "response", "error", "stream_chunk", "stream_end", "stream_close" */
    const char *func;                   /* request only */
    callwire_value_t *args;             /* request only, array of values */
    size_t args_count;
    int stream;                         /* request only, marks bidi-streaming */
    callwire_value_t result;            /* response/stream, can be any type */
    const char *error_type;             /* error only */
    const char *message;                /* error only */
} callwire_wire_message_t;

/* Client API */

/**
 * Connect to a callwire server at addr:port.
 * Returns NULL on error; call callwire_client_error() for details.
 */
callwire_client_t *callwire_client_connect(const char *addr, int port);

/**
 * Close and free a client connection.
 */
void callwire_client_close(callwire_client_t *client);

/**
 * Perform a unary RPC call. Blocks until response or timeout.
 * Result is written to *result_out (caller must free via callwire_value_free).
 * Returns 0 on success, non-zero on error.
 */
int callwire_client_call(callwire_client_t *client, const char *func,
                         callwire_value_t *args, size_t args_count,
                         callwire_value_t *result_out);

/**
 * Convenience: unary call with up to 8 int64 args, int64 result. Builds the
 * callwire_value_t array internally — no manual tagged-union construction.
 * Returns 0 on success, -1 on failure (see callwire_error()) or if the
 * server's result isn't an int64.
 *   int64_t result;
 *   callwire_call_ints(client, "add", (int64_t[]){10, 20}, 2, &result);
 */
int callwire_call_ints(callwire_client_t *client, const char *func,
                        const int64_t *args, size_t argc, int64_t *result_out);

/**
 * Convenience: unary call with a single string arg (or none, pass NULL),
 * string result copied into result_buf (null-terminated, truncated if it
 * doesn't fit). Returns 0 on success, -1 on failure or if the server's
 * result isn't a string.
 */
int callwire_call_str(callwire_client_t *client, const char *func, const char *arg,
                       char *result_buf, size_t result_buf_len);

/**
 * Begin server-streaming. Returns a stream ID for use with callwire_stream_recv().
 */
uint64_t callwire_client_stream_begin(callwire_client_t *client, const char *func,
                                       callwire_value_t *args, size_t args_count);

/**
 * Receive next chunk from a server stream. Returns 0 if stream continues,
 * 1 if stream_end received, -1 on error.
 * Chunk is written to *chunk_out (caller must free via callwire_value_free).
 */
int callwire_client_stream_recv(callwire_client_t *client, uint64_t stream_id,
                                 callwire_value_t *chunk_out);

/**
 * Begin client-streaming. Returns a stream ID for use with callwire_client_stream_send()
 * and callwire_client_stream_close().
 */
uint64_t callwire_client_export_stream_begin(callwire_client_t *client, const char *func);

/**
 * Send a chunk in client-streaming. stream_id was returned by callwire_client_export_stream_begin().
 */
int callwire_client_export_stream_send(callwire_client_t *client, uint64_t stream_id,
                                        callwire_value_t *chunk);

/**
 * Close client-streaming and wait for server response.
 * Result is written to *result_out.
 */
int callwire_client_export_stream_close(callwire_client_t *client, uint64_t stream_id,
                                         callwire_value_t *result_out);

/**
 * Begin bidirectional-streaming. Returns a stream ID.
 */
uint64_t callwire_client_bidi_stream_begin(callwire_client_t *client, const char *func);

/**
 * Send a chunk in bidi streaming.
 */
int callwire_client_bidi_stream_send(callwire_client_t *client, uint64_t stream_id,
                                      callwire_value_t *chunk);

/**
 * Receive next chunk in bidi streaming. Returns 0 if chunk received, 1 if stream_end, -1 on error.
 */
int callwire_client_bidi_stream_recv(callwire_client_t *client, uint64_t stream_id,
                                      callwire_value_t *chunk_out);

/**
 * Close client side of bidi stream.
 */
int callwire_client_bidi_stream_close_send(callwire_client_t *client, uint64_t stream_id);

/* Server API */

/**
 * Create a server listening on addr:port.
 * Returns NULL on error.
 */
callwire_server_t *callwire_server_new(const char *addr, int port);

/**
 * Register a unary RPC handler.
 * fn_ptr is called with (callwire_value_t *args, size_t args_count, callwire_value_t *result_out).
 * Must return 0 on success, non-zero on error.
 */
int callwire_server_export(callwire_server_t *server, const char *func,
                           int (*fn_ptr)(callwire_value_t *, size_t, callwire_value_t *));

/**
 * Register a unary RPC handler with an opaque userdata pointer, passed back
 * on every invocation. Lets language bindings (C++, Swift, etc.) route a
 * single C-linkage trampoline to per-registration closures/objects instead
 * of requiring one distinct function pointer per exported name.
 * fn_ptr is called with (userdata, args, args_count, result_out).
 * Must return 0 on success, non-zero on error.
 */
int callwire_server_export_ctx(callwire_server_t *server, const char *func, void *userdata,
                               int (*fn_ptr)(void *, callwire_value_t *, size_t, callwire_value_t *));

/**
 * Convenience macros: define a unary int64-arg handler without hand-writing
 * a (callwire_value_t*, size_t, callwire_value_t*) -> int function and
 * manually unpacking args[i].val.int_val / packing the result.
 *
 *   CALLWIRE_EXPORT_INT2(add, a, b) { return a + b; }
 *   callwire_server_export(server, "add", add);
 *
 * Expands to a `static int add(callwire_value_t*, size_t, callwire_value_t*)`
 * function — the name is usable directly as the fn_ptr argument to
 * callwire_server_export(). No bounds/type checking on args beyond arity
 * (matches the rest of this convenience layer's scope: common case only,
 * drop to the full ABI for anything more defensive).
 */
#define CALLWIRE_EXPORT_INT1(NAME, A) \
    static int64_t NAME##_body(int64_t A); \
    static int NAME(callwire_value_t *cw_args, size_t cw_argc, callwire_value_t *cw_out) { \
        (void)cw_argc; \
        cw_out->type = CALLWIRE_INT64; \
        cw_out->val.int_val = NAME##_body(cw_args[0].val.int_val); \
        return 0; \
    } \
    static int64_t NAME##_body(int64_t A)

#define CALLWIRE_EXPORT_INT2(NAME, A, B) \
    static int64_t NAME##_body(int64_t A, int64_t B); \
    static int NAME(callwire_value_t *cw_args, size_t cw_argc, callwire_value_t *cw_out) { \
        (void)cw_argc; \
        cw_out->type = CALLWIRE_INT64; \
        cw_out->val.int_val = NAME##_body(cw_args[0].val.int_val, cw_args[1].val.int_val); \
        return 0; \
    } \
    static int64_t NAME##_body(int64_t A, int64_t B)

#define CALLWIRE_EXPORT_INT3(NAME, A, B, C) \
    static int64_t NAME##_body(int64_t A, int64_t B, int64_t C); \
    static int NAME(callwire_value_t *cw_args, size_t cw_argc, callwire_value_t *cw_out) { \
        (void)cw_argc; \
        cw_out->type = CALLWIRE_INT64; \
        cw_out->val.int_val = NAME##_body(cw_args[0].val.int_val, cw_args[1].val.int_val, cw_args[2].val.int_val); \
        return 0; \
    } \
    static int64_t NAME##_body(int64_t A, int64_t B, int64_t C)

/**
 * Start accepting connections (blocks until error or close).
 */
int callwire_server_serve(callwire_server_t *server);

/**
 * Close and free a server.
 */
void callwire_server_close(callwire_server_t *server);

/* Utility */

/**
 * Free a callwire_value_t and its contents.
 */
void callwire_value_free(callwire_value_t *value);

/**
 * Get the last error message (thread-local).
 */
const char *callwire_error(void);

#ifdef __cplusplus
}
#endif

#endif /* CALLWIRE_H */
