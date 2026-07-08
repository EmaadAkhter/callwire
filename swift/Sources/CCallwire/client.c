#include "internal.h"
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <netdb.h>
#include <netinet/in.h>
#include <unistd.h>

/* ponytail: one RPC call in flight per connection at a time — no reader
 * thread, no pending-map demux. A caller wanting concurrent calls opens
 * multiple callwire_client_t connections. This matches the simplest correct
 * semantics for a C ABI consumer and is what C++/COBOL/Swift will initially
 * build on; upgrade to a background reader thread + id-keyed pending map
 * (mirroring the Go/Rust/Java client internals) if true per-connection
 * concurrency is needed later. */
struct callwire_client {
    int sock;
    uint64_t next_id;
};

callwire_client_t *callwire_client_connect(const char *addr, int port) {
    callwire_client_t *c = malloc(sizeof(callwire_client_t));
    if (!c) {
        callwire_error_set("out of memory");
        return NULL;
    }

    struct hostent *he = gethostbyname(addr);
    if (!he) {
        callwire_error_set("could not resolve host '%s'", addr);
        free(c);
        return NULL;
    }

    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) {
        callwire_error_set("socket() failed");
        free(c);
        return NULL;
    }

    struct sockaddr_in sa;
    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_port = htons((uint16_t)port);
    memcpy(&sa.sin_addr, he->h_addr, (size_t)he->h_length);

    if (connect(sock, (struct sockaddr *)&sa, sizeof(sa)) < 0) {
        callwire_error_set("connect() to %s:%d failed", addr, port);
        close(sock);
        free(c);
        return NULL;
    }

    c->sock = sock;
    c->next_id = 0;

    return c;
}

void callwire_client_close(callwire_client_t *client) {
    if (!client) return;
    if (client->sock >= 0) {
        close(client->sock);
    }
    free(client);
}

static uint64_t next_id(callwire_client_t *client) {
    return ++client->next_id;
}

/* Reads the next frame and decodes it. Returns 0 on success, -1 on I/O or
 * decode error (with callwire_error() set). */
static int read_wire_message(callwire_client_t *client, callwire_wire_message_t *msg_out) {
    uint8_t *payload;
    size_t len;
    if (callwire_framing_read_frame(client->sock, &payload, &len) != 0) {
        callwire_error_set("connection read failed or closed");
        return -1;
    }
    int rc = callwire_codec_decode(payload, len, msg_out);
    free(payload);
    if (rc != 0) {
        callwire_error_set("failed to decode server response");
        return -1;
    }
    return 0;
}

static int write_payload(callwire_client_t *client, uint8_t *payload, size_t len) {
    int rc = callwire_framing_write_frame(client->sock, payload, len);
    free(payload);
    if (rc != 0) {
        callwire_error_set("connection write failed");
    }
    return rc;
}

int callwire_client_call(callwire_client_t *client, const char *func,
                         callwire_value_t *args, size_t args_count,
                         callwire_value_t *result_out) {
    if (!client || !func) {
        callwire_error_set("invalid argument");
        return -1;
    }

    uint64_t id = next_id(client);
    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_request(id, func, args, args_count, &payload, &len) != 0) {
        callwire_error_set("failed to encode request");
        return -1;
    }
    if (write_payload(client, payload, len) != 0) {
        return -1;
    }

    callwire_wire_message_t msg;
    if (read_wire_message(client, &msg) != 0) {
        return -1;
    }

    int rc;
    if (msg.type && strcmp(msg.type, "error") == 0) {
        callwire_error_set("%s: %s",
                            msg.error_type ? msg.error_type : "Error",
                            msg.message ? msg.message : "unknown error");
        rc = -1;
    } else {
        *result_out = msg.result;
        msg.result.type = CALLWIRE_NULL; /* transfer ownership to result_out */
        rc = 0;
    }

    callwire_wire_message_free(&msg);
    return rc;
}

uint64_t callwire_client_stream_begin(callwire_client_t *client, const char *func,
                                       callwire_value_t *args, size_t args_count) {
    if (!client || !func) {
        callwire_error_set("invalid argument");
        return 0;
    }

    uint64_t id = next_id(client);
    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_request(id, func, args, args_count, &payload, &len) != 0) {
        callwire_error_set("failed to encode request");
        return 0;
    }
    if (write_payload(client, payload, len) != 0) {
        return 0;
    }
    return id;
}

int callwire_client_stream_recv(callwire_client_t *client, uint64_t stream_id,
                                 callwire_value_t *chunk_out) {
    (void)stream_id; /* single call in flight per connection; id is implicit */
    if (!client) {
        callwire_error_set("invalid argument");
        return -1;
    }

    callwire_wire_message_t msg;
    if (read_wire_message(client, &msg) != 0) {
        return -1;
    }

    int rc;
    if (msg.type && strcmp(msg.type, "stream_chunk") == 0) {
        *chunk_out = msg.result;
        msg.result.type = CALLWIRE_NULL;
        rc = 0;
    } else if (msg.type && strcmp(msg.type, "stream_end") == 0) {
        rc = 1;
    } else if (msg.type && strcmp(msg.type, "error") == 0) {
        callwire_error_set("%s: %s",
                            msg.error_type ? msg.error_type : "Error",
                            msg.message ? msg.message : "unknown error");
        rc = -1;
    } else {
        callwire_error_set("unexpected message type during stream recv");
        rc = -1;
    }

    callwire_wire_message_free(&msg);
    return rc;
}

uint64_t callwire_client_export_stream_begin(callwire_client_t *client, const char *func) {
    if (!client || !func) {
        callwire_error_set("invalid argument");
        return 0;
    }

    uint64_t id = next_id(client);
    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_request(id, func, NULL, 0, &payload, &len) != 0) {
        callwire_error_set("failed to encode request");
        return 0;
    }
    if (write_payload(client, payload, len) != 0) {
        return 0;
    }
    return id;
}

int callwire_client_export_stream_send(callwire_client_t *client, uint64_t stream_id,
                                        callwire_value_t *chunk) {
    if (!client) {
        callwire_error_set("invalid argument");
        return -1;
    }

    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_stream_chunk(stream_id, chunk, &payload, &len) != 0) {
        callwire_error_set("failed to encode stream chunk");
        return -1;
    }
    return write_payload(client, payload, len);
}

int callwire_client_export_stream_close(callwire_client_t *client, uint64_t stream_id,
                                         callwire_value_t *result_out) {
    if (!client) {
        callwire_error_set("invalid argument");
        return -1;
    }

    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_stream_close(stream_id, &payload, &len) != 0) {
        callwire_error_set("failed to encode stream close");
        return -1;
    }
    if (write_payload(client, payload, len) != 0) {
        return -1;
    }

    callwire_wire_message_t msg;
    if (read_wire_message(client, &msg) != 0) {
        return -1;
    }

    int rc;
    if (msg.type && strcmp(msg.type, "error") == 0) {
        callwire_error_set("%s: %s",
                            msg.error_type ? msg.error_type : "Error",
                            msg.message ? msg.message : "unknown error");
        rc = -1;
    } else {
        *result_out = msg.result;
        msg.result.type = CALLWIRE_NULL;
        rc = 0;
    }

    callwire_wire_message_free(&msg);
    return rc;
}

uint64_t callwire_client_bidi_stream_begin(callwire_client_t *client, const char *func) {
    if (!client || !func) {
        callwire_error_set("invalid argument");
        return 0;
    }

    uint64_t id = next_id(client);
    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_bidi_request(id, func, NULL, 0, &payload, &len) != 0) {
        callwire_error_set("failed to encode bidi request");
        return 0;
    }
    if (write_payload(client, payload, len) != 0) {
        return 0;
    }
    return id;
}

int callwire_client_bidi_stream_send(callwire_client_t *client, uint64_t stream_id,
                                      callwire_value_t *chunk) {
    if (!client) {
        callwire_error_set("invalid argument");
        return -1;
    }

    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_stream_chunk(stream_id, chunk, &payload, &len) != 0) {
        callwire_error_set("failed to encode stream chunk");
        return -1;
    }
    return write_payload(client, payload, len);
}

int callwire_client_bidi_stream_recv(callwire_client_t *client, uint64_t stream_id,
                                      callwire_value_t *chunk_out) {
    (void)stream_id;
    return callwire_client_stream_recv(client, stream_id, chunk_out);
}

int callwire_client_bidi_stream_close_send(callwire_client_t *client, uint64_t stream_id) {
    if (!client) {
        callwire_error_set("invalid argument");
        return -1;
    }

    uint8_t *payload;
    size_t len;
    if (callwire_codec_encode_stream_end(stream_id, &payload, &len) != 0) {
        callwire_error_set("failed to encode stream end");
        return -1;
    }
    return write_payload(client, payload, len);
}
