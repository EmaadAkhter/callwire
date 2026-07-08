#include "internal.h"
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <unistd.h>
#include <pthread.h>

typedef int (*callwire_handler_fn)(callwire_value_t *, size_t, callwire_value_t *);
typedef int (*callwire_handler_ctx_fn)(void *, callwire_value_t *, size_t, callwire_value_t *);
typedef int (*callwire_stream_fn_t)(void *, callwire_value_t *, size_t, callwire_stream_emit_fn, void *);
typedef int (*callwire_client_stream_fn_t)(void *, callwire_stream_recv_fn, void *, callwire_value_t *);
typedef int (*callwire_bidi_fn_t)(void *, callwire_stream_recv_fn, void *, callwire_stream_emit_fn, void *);

typedef enum {
    HANDLER_UNARY,
    HANDLER_UNARY_CTX,
    HANDLER_STREAM_CTX,
    HANDLER_CLIENT_STREAM_CTX,
    HANDLER_BIDI_CTX
} handler_kind_t;

typedef struct {
    char *name;
    handler_kind_t kind;
    void *fn;       /* cast to the matching typedef above per `kind` */
    void *userdata; /* unused for HANDLER_UNARY (no ctx) */
} registry_entry_t;

struct callwire_server {
    int listen_sock;
    volatile int running;
    pthread_mutex_t registry_mu;
    registry_entry_t *entries;
    size_t entry_count;
    size_t entry_cap;
};

/* ---------------------------------------------------------------------- */
/* Per-connection state: write mutex (multiple call threads may write to    */
/* the same socket concurrently) + a registry of in-progress streaming-     */
/* input calls (client-stream/bidi), keyed by request id, each backed by    */
/* a small blocking queue that the connection's read loop pushes into and  */
/* the call's dispatch thread pops from via recv().                        */
/* ---------------------------------------------------------------------- */

typedef struct chunk_node {
    callwire_value_t value;
    int is_end;
    struct chunk_node *next;
} chunk_node_t;

typedef struct {
    uint64_t id;
    pthread_mutex_t mu;
    pthread_cond_t cond;
    chunk_node_t *head;
    chunk_node_t *tail;
} stream_input_t;

typedef struct {
    int sockfd;
    pthread_mutex_t write_mu;
    pthread_mutex_t inputs_mu;
    stream_input_t **inputs;
    size_t inputs_count;
    size_t inputs_cap;
} conn_state_t;

static stream_input_t *stream_input_new(uint64_t id) {
    stream_input_t *in = malloc(sizeof(stream_input_t));
    in->id = id;
    pthread_mutex_init(&in->mu, NULL);
    pthread_cond_init(&in->cond, NULL);
    in->head = in->tail = NULL;
    return in;
}

static void stream_input_push(stream_input_t *in, callwire_value_t value, int is_end) {
    chunk_node_t *node = malloc(sizeof(chunk_node_t));
    node->value = value;
    node->is_end = is_end;
    node->next = NULL;
    pthread_mutex_lock(&in->mu);
    if (in->tail) {
        in->tail->next = node;
    } else {
        in->head = node;
    }
    in->tail = node;
    pthread_cond_signal(&in->cond);
    pthread_mutex_unlock(&in->mu);
}

/* Blocks until a chunk or end marker arrives. Returns 0 (chunk_out filled),
 * 1 (clean end), matching callwire_stream_recv_fn's contract. */
static int stream_input_recv(void *recv_ctx, callwire_value_t *chunk_out) {
    stream_input_t *in = (stream_input_t *)recv_ctx;
    pthread_mutex_lock(&in->mu);
    while (!in->head) {
        pthread_cond_wait(&in->cond, &in->mu);
    }
    chunk_node_t *node = in->head;
    in->head = node->next;
    if (!in->head) in->tail = NULL;
    pthread_mutex_unlock(&in->mu);

    int result = node->is_end ? 1 : 0;
    if (!node->is_end) {
        *chunk_out = node->value; /* transfer ownership to caller */
    }
    free(node);
    return result;
}

static void stream_input_free(stream_input_t *in) {
    pthread_mutex_lock(&in->mu);
    chunk_node_t *node = in->head;
    while (node) {
        chunk_node_t *next = node->next;
        if (!node->is_end) callwire_value_free(&node->value);
        free(node);
        node = next;
    }
    pthread_mutex_unlock(&in->mu);
    pthread_mutex_destroy(&in->mu);
    pthread_cond_destroy(&in->cond);
    free(in);
}

static stream_input_t *conn_register_input(conn_state_t *conn, uint64_t id) {
    stream_input_t *in = stream_input_new(id);
    pthread_mutex_lock(&conn->inputs_mu);
    if (conn->inputs_count == conn->inputs_cap) {
        size_t new_cap = conn->inputs_cap == 0 ? 8 : conn->inputs_cap * 2;
        conn->inputs = realloc(conn->inputs, new_cap * sizeof(stream_input_t *));
        conn->inputs_cap = new_cap;
    }
    conn->inputs[conn->inputs_count++] = in;
    pthread_mutex_unlock(&conn->inputs_mu);
    return in;
}

static stream_input_t *conn_find_input(conn_state_t *conn, uint64_t id) {
    stream_input_t *found = NULL;
    pthread_mutex_lock(&conn->inputs_mu);
    for (size_t i = 0; i < conn->inputs_count; i++) {
        if (conn->inputs[i]->id == id) {
            found = conn->inputs[i];
            break;
        }
    }
    pthread_mutex_unlock(&conn->inputs_mu);
    return found;
}

static void conn_remove_input(conn_state_t *conn, uint64_t id) {
    pthread_mutex_lock(&conn->inputs_mu);
    for (size_t i = 0; i < conn->inputs_count; i++) {
        if (conn->inputs[i]->id == id) {
            stream_input_t *in = conn->inputs[i];
            conn->inputs[i] = conn->inputs[conn->inputs_count - 1];
            conn->inputs_count--;
            pthread_mutex_unlock(&conn->inputs_mu);
            stream_input_free(in);
            return;
        }
    }
    pthread_mutex_unlock(&conn->inputs_mu);
}

/* ---------------------------------------------------------------------- */

static int conn_write_frame(conn_state_t *conn, const uint8_t *payload, size_t len) {
    pthread_mutex_lock(&conn->write_mu);
    int rc = callwire_framing_write_frame(conn->sockfd, payload, len);
    pthread_mutex_unlock(&conn->write_mu);
    return rc;
}

static void write_error(conn_state_t *conn, uint64_t id, const char *error_type, const char *message) {
    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_error(id, error_type, message, &payload, &len) == 0) {
        conn_write_frame(conn, payload, len);
        free(payload);
    }
}

/* emit() callback handed to server-streaming/bidi handlers. */
typedef struct {
    conn_state_t *conn;
    uint64_t id;
} emit_ctx_t;

static int emit_chunk(void *emit_ctx, callwire_value_t *chunk) {
    emit_ctx_t *ctx = (emit_ctx_t *)emit_ctx;
    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_stream_chunk(ctx->id, chunk, &payload, &len) != 0) {
        return -1;
    }
    int rc = conn_write_frame(ctx->conn, payload, len);
    free(payload);
    return rc;
}

/* ---------------------------------------------------------------------- */

callwire_server_t *callwire_server_new(const char *addr, int port) {
    (void)addr; /* binds to INADDR_ANY; addr reserved for future interface binding */
    callwire_server_t *s = malloc(sizeof(callwire_server_t));
    if (!s) {
        callwire_error_set("out of memory");
        return NULL;
    }

    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) {
        callwire_error_set("socket() failed");
        free(s);
        return NULL;
    }

    int opt = 1;
    if (setsockopt(sock, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt)) < 0) {
        callwire_error_set("setsockopt(SO_REUSEADDR) failed");
        close(sock);
        free(s);
        return NULL;
    }

    struct sockaddr_in sa;
    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_port = htons((uint16_t)port);
    sa.sin_addr.s_addr = htonl(INADDR_ANY);

    if (bind(sock, (struct sockaddr *)&sa, sizeof(sa)) < 0) {
        callwire_error_set("bind() to port %d failed", port);
        close(sock);
        free(s);
        return NULL;
    }

    if (listen(sock, 128) < 0) {
        callwire_error_set("listen() failed");
        close(sock);
        free(s);
        return NULL;
    }

    s->listen_sock = sock;
    s->running = 1;
    pthread_mutex_init(&s->registry_mu, NULL);
    s->entries = NULL;
    s->entry_count = 0;
    s->entry_cap = 0;
    return s;
}

static int register_entry(callwire_server_t *server, const char *func, registry_entry_t entry) {
    if (!server || !func) {
        callwire_error_set("invalid argument");
        return -1;
    }

    char *name_copy = malloc(strlen(func) + 1);
    if (!name_copy) {
        callwire_error_set("out of memory");
        return -1;
    }
    strcpy(name_copy, func);
    entry.name = name_copy;

    pthread_mutex_lock(&server->registry_mu);
    if (server->entry_count == server->entry_cap) {
        size_t new_cap = server->entry_cap == 0 ? 8 : server->entry_cap * 2;
        registry_entry_t *new_entries = realloc(server->entries, new_cap * sizeof(registry_entry_t));
        if (!new_entries) {
            pthread_mutex_unlock(&server->registry_mu);
            free(name_copy);
            callwire_error_set("out of memory");
            return -1;
        }
        server->entries = new_entries;
        server->entry_cap = new_cap;
    }
    server->entries[server->entry_count] = entry;
    server->entry_count++;
    pthread_mutex_unlock(&server->registry_mu);

    return 0;
}

int callwire_server_export(callwire_server_t *server, const char *func,
                           int (*fn_ptr)(callwire_value_t *, size_t, callwire_value_t *)) {
    if (!fn_ptr) {
        callwire_error_set("invalid argument");
        return -1;
    }
    registry_entry_t entry = {0};
    entry.kind = HANDLER_UNARY;
    entry.fn = (void *)fn_ptr;
    return register_entry(server, func, entry);
}

int callwire_server_export_ctx(callwire_server_t *server, const char *func, void *userdata,
                               int (*fn_ptr)(void *, callwire_value_t *, size_t, callwire_value_t *)) {
    if (!fn_ptr) {
        callwire_error_set("invalid argument");
        return -1;
    }
    registry_entry_t entry = {0};
    entry.kind = HANDLER_UNARY_CTX;
    entry.fn = (void *)fn_ptr;
    entry.userdata = userdata;
    return register_entry(server, func, entry);
}

int callwire_server_export_stream_ctx(callwire_server_t *server, const char *func, void *userdata,
                                      int (*fn_ptr)(void *, callwire_value_t *, size_t,
                                                     callwire_stream_emit_fn, void *)) {
    if (!fn_ptr) {
        callwire_error_set("invalid argument");
        return -1;
    }
    registry_entry_t entry = {0};
    entry.kind = HANDLER_STREAM_CTX;
    entry.fn = (void *)fn_ptr;
    entry.userdata = userdata;
    return register_entry(server, func, entry);
}

int callwire_server_export_client_stream_ctx(callwire_server_t *server, const char *func, void *userdata,
                                             int (*fn_ptr)(void *, callwire_stream_recv_fn, void *,
                                                            callwire_value_t *)) {
    if (!fn_ptr) {
        callwire_error_set("invalid argument");
        return -1;
    }
    registry_entry_t entry = {0};
    entry.kind = HANDLER_CLIENT_STREAM_CTX;
    entry.fn = (void *)fn_ptr;
    entry.userdata = userdata;
    return register_entry(server, func, entry);
}

int callwire_server_export_bidi_ctx(callwire_server_t *server, const char *func, void *userdata,
                                    int (*fn_ptr)(void *, callwire_stream_recv_fn, void *,
                                                   callwire_stream_emit_fn, void *)) {
    if (!fn_ptr) {
        callwire_error_set("invalid argument");
        return -1;
    }
    registry_entry_t entry = {0};
    entry.kind = HANDLER_BIDI_CTX;
    entry.fn = (void *)fn_ptr;
    entry.userdata = userdata;
    return register_entry(server, func, entry);
}

static int find_handler(callwire_server_t *server, const char *func, registry_entry_t *out) {
    int found = 0;
    pthread_mutex_lock(&server->registry_mu);
    for (size_t i = 0; i < server->entry_count; i++) {
        if (strcmp(server->entries[i].name, func) == 0) {
            *out = server->entries[i];
            found = 1;
            break;
        }
    }
    pthread_mutex_unlock(&server->registry_mu);
    return found;
}

/* ---------------------------------------------------------------------- */
/* Per-call dispatch: each incoming request is handled on its own thread    */
/* so the connection's read loop stays free to route follow-up frames       */
/* (stream_chunk/close/end) to in-progress client-stream/bidi calls.        */
/* ---------------------------------------------------------------------- */

typedef struct {
    conn_state_t *conn;
    registry_entry_t entry;
    uint64_t id;
    callwire_value_t *args; /* owned; moved from the decoded wire message */
    size_t args_count;
    int is_bidi_input; /* stream flag on the request, distinguishes ClientStream vs Bidi framing */
} dispatch_ctx_t;

static void free_args(callwire_value_t *args, size_t count) {
    if (!args) return;
    for (size_t i = 0; i < count; i++) callwire_value_free(&args[i]);
    free(args);
}

static void *dispatch_unary_thread(void *arg) {
    dispatch_ctx_t *ctx = (dispatch_ctx_t *)arg;
    callwire_value_t result;
    result.type = CALLWIRE_NULL;

    int rc = ctx->entry.kind == HANDLER_UNARY_CTX
                 ? ((callwire_handler_ctx_fn)ctx->entry.fn)(ctx->entry.userdata, ctx->args, ctx->args_count, &result)
                 : ((callwire_handler_fn)ctx->entry.fn)(ctx->args, ctx->args_count, &result);

    if (rc != 0) {
        write_error(ctx->conn, ctx->id, "Error", "handler returned an error");
        callwire_value_free(&result);
    } else {
        uint8_t *payload;
        size_t len;
        if (callwire_codec_encode_response(ctx->id, &result, &payload, &len) == 0) {
            conn_write_frame(ctx->conn, payload, len);
            free(payload);
        }
        callwire_value_free(&result);
    }

    free_args(ctx->args, ctx->args_count);
    free(ctx->entry.name);
    free(ctx);
    return NULL;
}

static void *dispatch_stream_thread(void *arg) {
    dispatch_ctx_t *ctx = (dispatch_ctx_t *)arg;
    emit_ctx_t emit_ctx = { ctx->conn, ctx->id };

    int rc = ((callwire_stream_fn_t)ctx->entry.fn)(ctx->entry.userdata, ctx->args, ctx->args_count,
                                                     emit_chunk, &emit_ctx);
    if (rc != 0) {
        write_error(ctx->conn, ctx->id, "Error", "handler returned an error");
    } else {
        uint8_t *payload;
        size_t len;
        if (callwire_codec_encode_stream_end(ctx->id, &payload, &len) == 0) {
            conn_write_frame(ctx->conn, payload, len);
            free(payload);
        }
    }

    free_args(ctx->args, ctx->args_count);
    free(ctx->entry.name);
    free(ctx);
    return NULL;
}

static void *dispatch_client_stream_thread(void *arg) {
    dispatch_ctx_t *ctx = (dispatch_ctx_t *)arg;
    stream_input_t *in = conn_find_input(ctx->conn, ctx->id);

    callwire_value_t result;
    result.type = CALLWIRE_NULL;
    int rc = ((callwire_client_stream_fn_t)ctx->entry.fn)(ctx->entry.userdata, stream_input_recv, in, &result);

    if (rc != 0) {
        write_error(ctx->conn, ctx->id, "Error", "handler returned an error");
        callwire_value_free(&result);
    } else {
        uint8_t *payload;
        size_t len;
        if (callwire_codec_encode_response(ctx->id, &result, &payload, &len) == 0) {
            conn_write_frame(ctx->conn, payload, len);
            free(payload);
        }
        callwire_value_free(&result);
    }

    conn_remove_input(ctx->conn, ctx->id);
    free(ctx->entry.name);
    free(ctx);
    return NULL;
}

static void *dispatch_bidi_thread(void *arg) {
    dispatch_ctx_t *ctx = (dispatch_ctx_t *)arg;
    stream_input_t *in = conn_find_input(ctx->conn, ctx->id);
    emit_ctx_t emit_ctx = { ctx->conn, ctx->id };

    int rc = ((callwire_bidi_fn_t)ctx->entry.fn)(ctx->entry.userdata, stream_input_recv, in, emit_chunk, &emit_ctx);

    if (rc != 0) {
        write_error(ctx->conn, ctx->id, "Error", "handler returned an error");
    } else {
        uint8_t *payload;
        size_t len;
        if (callwire_codec_encode_stream_end(ctx->id, &payload, &len) == 0) {
            conn_write_frame(ctx->conn, payload, len);
            free(payload);
        }
    }

    conn_remove_input(ctx->conn, ctx->id);
    free(ctx->entry.name);
    free(ctx);
    return NULL;
}

static void dispatch_request(callwire_server_t *server, conn_state_t *conn, callwire_wire_message_t *msg) {
    if (!msg->func) {
        write_error(conn, msg->id, "TypeError", "missing func field");
        return;
    }

    registry_entry_t entry;
    if (!find_handler(server, msg->func, &entry)) {
        write_error(conn, msg->id, "NotFoundError", "function not exported");
        return;
    }

    /* Streaming-input calls (client-stream, bidi) need their input queue
     * registered BEFORE we return to the read loop, so follow-up
     * stream_chunk frames (which can arrive immediately) have somewhere to go. */
    int is_bidi = msg->stream;
    if (entry.kind == HANDLER_BIDI_CTX && !is_bidi) {
        write_error(conn, msg->id, "TypeError", "function requires stream:true (bidi call)");
        return;
    }
    if (entry.kind == HANDLER_CLIENT_STREAM_CTX && is_bidi) {
        write_error(conn, msg->id, "TypeError", "function is client-streaming, not bidi");
        return;
    }

    dispatch_ctx_t *ctx = malloc(sizeof(dispatch_ctx_t));
    ctx->conn = conn;
    ctx->entry = entry; /* entry.name is a fresh copy owned by ctx now (freed in each thread) */
    ctx->entry.name = malloc(strlen(entry.name) + 1);
    strcpy(ctx->entry.name, entry.name);
    ctx->id = msg->id;
    ctx->args = msg->args;
    ctx->args_count = msg->args_count;
    msg->args = NULL; /* ownership moved to ctx; wire_message_free won't double-free */
    msg->args_count = 0;

    void *(*thread_fn)(void *) = NULL;
    switch (entry.kind) {
        case HANDLER_UNARY:
        case HANDLER_UNARY_CTX:
            thread_fn = dispatch_unary_thread;
            break;
        case HANDLER_STREAM_CTX:
            thread_fn = dispatch_stream_thread;
            break;
        case HANDLER_CLIENT_STREAM_CTX:
            conn_register_input(conn, msg->id);
            thread_fn = dispatch_client_stream_thread;
            break;
        case HANDLER_BIDI_CTX:
            conn_register_input(conn, msg->id);
            thread_fn = dispatch_bidi_thread;
            break;
    }

    pthread_t thread;
    if (pthread_create(&thread, NULL, thread_fn, ctx) != 0) {
        free_args(ctx->args, ctx->args_count);
        free(ctx->entry.name);
        free(ctx);
        return;
    }
    pthread_detach(thread);
}

static void *handle_connection(void *arg) {
    void **args = (void **)arg;
    callwire_server_t *server = (callwire_server_t *)args[0];
    int sockfd = (int)(intptr_t)args[1];
    free(arg);

    conn_state_t conn;
    conn.sockfd = sockfd;
    pthread_mutex_init(&conn.write_mu, NULL);
    pthread_mutex_init(&conn.inputs_mu, NULL);
    conn.inputs = NULL;
    conn.inputs_count = 0;
    conn.inputs_cap = 0;

    for (;;) {
        uint8_t *payload;
        size_t len;
        if (callwire_framing_read_frame(sockfd, &payload, &len) != 0) {
            break; /* connection closed or error */
        }

        callwire_wire_message_t msg;
        int decoded_ok = (callwire_codec_decode(payload, len, &msg) == 0);
        free(payload);
        if (!decoded_ok) {
            continue; /* ignore malformed frame */
        }

        if (msg.type && (strcmp(msg.type, "stream_chunk") == 0 ||
                          strcmp(msg.type, "stream_close") == 0 ||
                          strcmp(msg.type, "stream_end") == 0)) {
            stream_input_t *in = conn_find_input(&conn, msg.id);
            if (in) {
                if (strcmp(msg.type, "stream_chunk") == 0) {
                    /* Move ownership of the decoded result value into the queue. */
                    callwire_value_t moved = msg.result;
                    msg.result.type = CALLWIRE_NULL;
                    stream_input_push(in, moved, 0);
                } else {
                    callwire_value_t nil_val;
                    nil_val.type = CALLWIRE_NULL;
                    stream_input_push(in, nil_val, 1);
                }
            }
            callwire_wire_message_free(&msg);
            continue;
        }

        if (msg.type && strcmp(msg.type, "request") == 0) {
            dispatch_request(server, &conn, &msg);
        }

        callwire_wire_message_free(&msg);
    }

    /* Unblock any handler thread still waiting on input from a call that
     * never sent stream_close/stream_end before the connection dropped. */
    pthread_mutex_lock(&conn.inputs_mu);
    for (size_t i = 0; i < conn.inputs_count; i++) {
        callwire_value_t nil_val;
        nil_val.type = CALLWIRE_NULL;
        stream_input_push(conn.inputs[i], nil_val, 1);
    }
    pthread_mutex_unlock(&conn.inputs_mu);

    /* Note: in-flight dispatch threads still hold conn state by pointer;
     * this simplified implementation leaks the conn_state_t/mutexes rather
     * than solving the shutdown race of "free conn while a detached thread
     * might still be writing to it" — acceptable for a first working
     * streaming server (each conn is bounded by one client connection's
     * lifetime, not a long-running leak across many connections a la a
     * server that never closes any connection... this IS a real leak under
     * sustained load; flagged here rather than silently ignored). */

    close(sockfd);
    return NULL;
}

int callwire_server_serve(callwire_server_t *server) {
    if (!server) {
        callwire_error_set("invalid argument");
        return -1;
    }

    while (server->running) {
        int client_sock = accept(server->listen_sock, NULL, NULL);
        if (client_sock < 0) {
            if (!server->running) {
                break; /* listen_sock was closed by callwire_server_close() */
            }
            continue; /* transient accept error, keep serving */
        }

        void **args = malloc(2 * sizeof(void *));
        args[0] = server;
        args[1] = (void *)(intptr_t)client_sock;

        pthread_t thread;
        if (pthread_create(&thread, NULL, handle_connection, args) != 0) {
            free(args);
            close(client_sock);
            continue;
        }
        pthread_detach(thread);
    }

    return 0;
}

void callwire_server_close(callwire_server_t *server) {
    if (!server) return;
    server->running = 0;
    if (server->listen_sock >= 0) {
        close(server->listen_sock); /* unblocks accept() in callwire_server_serve() */
    }
    pthread_mutex_lock(&server->registry_mu);
    for (size_t i = 0; i < server->entry_count; i++) {
        free(server->entries[i].name);
    }
    free(server->entries);
    pthread_mutex_unlock(&server->registry_mu);
    pthread_mutex_destroy(&server->registry_mu);
    free(server);
}
