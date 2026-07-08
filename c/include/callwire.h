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

typedef struct {
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
