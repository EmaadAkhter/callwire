#include "callwire.h"
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <netdb.h>

struct callwire_client {
    int sock;
    /* TODO: thread-safe pending map for request tracking */
    /* TODO: read loop thread */
};

callwire_client_t *callwire_client_connect(const char *addr, int port) {
    callwire_client_t *c = malloc(sizeof(callwire_client_t));
    if (!c) return NULL;

    struct hostent *he = gethostbyname(addr);
    if (!he) {
        free(c);
        return NULL;
    }

    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) {
        free(c);
        return NULL;
    }

    struct sockaddr_in sa;
    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_port = htons(port);
    memcpy(&sa.sin_addr, he->h_addr, he->h_length);

    if (connect(sock, (struct sockaddr *)&sa, sizeof(sa)) < 0) {
        close(sock);
        free(c);
        return NULL;
    }

    c->sock = sock;
    /* TODO: start read loop thread */

    return c;
}

void callwire_client_close(callwire_client_t *client) {
    if (!client) return;
    if (client->sock >= 0) {
        close(client->sock);
    }
    /* TODO: stop read loop thread, drain pending */
    free(client);
}

int callwire_client_call(callwire_client_t *client, const char *func,
                         callwire_value_t *args, size_t args_count,
                         callwire_value_t *result_out) {
    /* TODO: encode request, send, wait for response */
    return -1; /* stub */
}

uint64_t callwire_client_stream_begin(callwire_client_t *client, const char *func,
                                       callwire_value_t *args, size_t args_count) {
    /* TODO: encode request, send, return stream ID */
    return 0;
}

int callwire_client_stream_recv(callwire_client_t *client, uint64_t stream_id,
                                 callwire_value_t *chunk_out) {
    /* TODO: wait for stream_chunk or stream_end with matching ID */
    return -1; /* stub */
}

uint64_t callwire_client_export_stream_begin(callwire_client_t *client, const char *func) {
    /* TODO: send initial request, return stream ID for sending chunks */
    return 0;
}

int callwire_client_export_stream_send(callwire_client_t *client, uint64_t stream_id,
                                        callwire_value_t *chunk) {
    /* TODO: encode and send stream_chunk */
    return -1; /* stub */
}

int callwire_client_export_stream_close(callwire_client_t *client, uint64_t stream_id,
                                         callwire_value_t *result_out) {
    /* TODO: send stream_close, wait for response */
    return -1; /* stub */
}

uint64_t callwire_client_bidi_stream_begin(callwire_client_t *client, const char *func) {
    /* TODO: send request with stream=true, return stream ID */
    return 0;
}

int callwire_client_bidi_stream_send(callwire_client_t *client, uint64_t stream_id,
                                      callwire_value_t *chunk) {
    /* TODO: send stream_chunk */
    return -1; /* stub */
}

int callwire_client_bidi_stream_recv(callwire_client_t *client, uint64_t stream_id,
                                      callwire_value_t *chunk_out) {
    /* TODO: receive stream_chunk or stream_end */
    return -1; /* stub */
}

int callwire_client_bidi_stream_close_send(callwire_client_t *client, uint64_t stream_id) {
    /* TODO: send stream_end */
    return -1; /* stub */
}
