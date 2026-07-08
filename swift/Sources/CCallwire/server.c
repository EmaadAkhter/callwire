#include "internal.h"
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <unistd.h>
#include <pthread.h>

typedef int (*callwire_handler_fn)(callwire_value_t *, size_t, callwire_value_t *);
typedef int (*callwire_handler_ctx_fn)(void *, callwire_value_t *, size_t, callwire_value_t *);

typedef struct {
    char *name;
    /* Exactly one of fn/fn_ctx is set, distinguished by has_userdata. */
    callwire_handler_fn fn;
    callwire_handler_ctx_fn fn_ctx;
    void *userdata;
    int has_userdata;
} registry_entry_t;

struct callwire_server {
    int listen_sock;
    volatile int running;
    pthread_mutex_t registry_mu;
    registry_entry_t *entries;
    size_t entry_count;
    size_t entry_cap;
};

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
    entry.fn = fn_ptr;
    entry.has_userdata = 0;
    return register_entry(server, func, entry);
}

int callwire_server_export_ctx(callwire_server_t *server, const char *func, void *userdata,
                               int (*fn_ptr)(void *, callwire_value_t *, size_t, callwire_value_t *)) {
    if (!fn_ptr) {
        callwire_error_set("invalid argument");
        return -1;
    }
    registry_entry_t entry = {0};
    entry.fn_ctx = fn_ptr;
    entry.userdata = userdata;
    entry.has_userdata = 1;
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

static void write_error(int sockfd, uint64_t id, const char *error_type, const char *message) {
    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_error(id, error_type, message, &payload, &len) == 0) {
        callwire_framing_write_frame(sockfd, payload, len);
        free(payload);
    }
}

static void dispatch_request(callwire_server_t *server, int sockfd, callwire_wire_message_t *msg) {
    if (!msg->func) {
        write_error(sockfd, msg->id, "TypeError", "missing func field");
        return;
    }

    registry_entry_t entry;
    if (!find_handler(server, msg->func, &entry)) {
        write_error(sockfd, msg->id, "NotFoundError", "function not exported");
        return;
    }

    callwire_value_t result;
    result.type = CALLWIRE_NULL;
    int rc = entry.has_userdata
                 ? entry.fn_ctx(entry.userdata, msg->args, msg->args_count, &result)
                 : entry.fn(msg->args, msg->args_count, &result);

    if (rc != 0) {
        write_error(sockfd, msg->id, "Error", "handler returned an error");
        callwire_value_free(&result);
        return;
    }

    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_response(msg->id, &result, &payload, &len) == 0) {
        callwire_framing_write_frame(sockfd, payload, len);
        free(payload);
    }
    callwire_value_free(&result);
}

typedef struct {
    callwire_server_t *server;
    int sockfd;
} conn_ctx_t;

static void *handle_connection(void *arg) {
    conn_ctx_t *ctx = (conn_ctx_t *)arg;
    callwire_server_t *server = ctx->server;
    int sockfd = ctx->sockfd;
    free(ctx);

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

        if (msg.type && strcmp(msg.type, "request") == 0) {
            /* The public C ABI only exposes unary registration
             * (callwire_server_export); streaming-input dispatch
             * (client-streaming/bidi) is not yet part of the C server ABI. */
            dispatch_request(server, sockfd, &msg);
        }
        /* stream_chunk/stream_close/stream_end frames from a client are
         * silently ignored server-side until streaming-input dispatch lands. */

        callwire_wire_message_free(&msg);
    }

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

        conn_ctx_t *ctx = malloc(sizeof(conn_ctx_t));
        if (!ctx) {
            close(client_sock);
            continue;
        }
        ctx->server = server;
        ctx->sockfd = client_sock;

        pthread_t thread;
        if (pthread_create(&thread, NULL, handle_connection, ctx) != 0) {
            free(ctx);
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
