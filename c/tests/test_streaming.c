/* Verifies the C ABI's streaming server registration (added to close the
 * Tier-1 gap: server-streaming, client-streaming, bidi — unary already
 * worked). Real TCP round-trip for all 3 patterns. */
#include "../include/callwire.h"
#include <assert.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define TEST_PORT 19599

/* Server-streaming: count_to(n) emits 1..n then ends. */
static int count_to_handler(void *userdata, callwire_value_t *args, size_t argc,
                             callwire_stream_emit_fn emit, void *emit_ctx) {
    (void)userdata;
    if (argc != 1 || args[0].type != CALLWIRE_INT64) return -1;
    int64_t n = args[0].val.int_val;
    for (int64_t i = 1; i <= n; i++) {
        callwire_value_t chunk;
        chunk.type = CALLWIRE_INT64;
        chunk.val.int_val = i;
        if (emit(emit_ctx, &chunk) != 0) return -1;
    }
    return 0;
}

/* Client-streaming: sum_stream receives chunks, returns their sum. */
static int sum_stream_handler(void *userdata, callwire_stream_recv_fn recv, void *recv_ctx,
                               callwire_value_t *result_out) {
    (void)userdata;
    int64_t sum = 0;
    for (;;) {
        callwire_value_t chunk;
        int rc = recv(recv_ctx, &chunk);
        if (rc == 1) break; /* client done sending */
        if (rc != 0) return -1;
        if (chunk.type == CALLWIRE_INT64) sum += chunk.val.int_val;
        callwire_value_free(&chunk);
    }
    result_out->type = CALLWIRE_INT64;
    result_out->val.int_val = sum;
    return 0;
}

/* Bidi: echo each received chunk back doubled, until client closes. */
static int echo_double_handler(void *userdata, callwire_stream_recv_fn recv, void *recv_ctx,
                                callwire_stream_emit_fn emit, void *emit_ctx) {
    (void)userdata;
    for (;;) {
        callwire_value_t chunk;
        int rc = recv(recv_ctx, &chunk);
        if (rc == 1) break;
        if (rc != 0) return -1;
        if (chunk.type == CALLWIRE_INT64) {
            callwire_value_t out;
            out.type = CALLWIRE_INT64;
            out.val.int_val = chunk.val.int_val * 2;
            emit(emit_ctx, &out);
        }
        callwire_value_free(&chunk);
    }
    return 0;
}

static callwire_server_t *g_server;

static void *server_thread_fn(void *arg) {
    (void)arg;
    callwire_server_serve(g_server);
    return NULL;
}

int main(void) {
    g_server = callwire_server_new("0.0.0.0", TEST_PORT);
    assert(g_server != NULL);

    assert(callwire_server_export_stream_ctx(g_server, "count_to", NULL, count_to_handler) == 0);
    assert(callwire_server_export_client_stream_ctx(g_server, "sum_stream", NULL, sum_stream_handler) == 0);
    assert(callwire_server_export_bidi_ctx(g_server, "echo_double", NULL, echo_double_handler) == 0);

    pthread_t server_thread;
    pthread_create(&server_thread, NULL, server_thread_fn, NULL);
    usleep(200 * 1000);

    callwire_client_t *client = callwire_client_connect("127.0.0.1", TEST_PORT);
    assert(client != NULL);

    /* Server-streaming: count_to(5) -> 1,2,3,4,5 */
    {
        callwire_value_t arg;
        arg.type = CALLWIRE_INT64;
        arg.val.int_val = 5;
        uint64_t stream_id = callwire_client_stream_begin(client, "count_to", &arg, 1);
        assert(stream_id != 0);

        int64_t expected = 1;
        int rc;
        callwire_value_t chunk;
        while ((rc = callwire_client_stream_recv(client, stream_id, &chunk)) == 0) {
            assert(chunk.type == CALLWIRE_INT64);
            assert(chunk.val.int_val == expected);
            expected++;
            callwire_value_free(&chunk);
        }
        assert(rc == 1); /* clean end */
        assert(expected == 6); /* saw 1..5 */
        printf("test_server_streaming_count_to: OK (1..5)\n");
    }

    /* Client-streaming: sum_stream(1,2,3,4) -> 10 */
    {
        uint64_t stream_id = callwire_client_export_stream_begin(client, "sum_stream");
        assert(stream_id != 0);
        for (int64_t i = 1; i <= 4; i++) {
            callwire_value_t chunk;
            chunk.type = CALLWIRE_INT64;
            chunk.val.int_val = i;
            assert(callwire_client_export_stream_send(client, stream_id, &chunk) == 0);
        }
        callwire_value_t result;
        assert(callwire_client_export_stream_close(client, stream_id, &result) == 0);
        assert(result.type == CALLWIRE_INT64);
        assert(result.val.int_val == 10);
        callwire_value_free(&result);
        printf("test_client_streaming_sum: OK (1+2+3+4 = 10)\n");
    }

    /* Bidi: echo_double sends 3, expects 6 back; sends 5, expects 10 back. */
    {
        uint64_t stream_id = callwire_client_bidi_stream_begin(client, "echo_double");
        assert(stream_id != 0);

        callwire_value_t send1;
        send1.type = CALLWIRE_INT64;
        send1.val.int_val = 3;
        assert(callwire_client_bidi_stream_send(client, stream_id, &send1) == 0);

        callwire_value_t recv1;
        assert(callwire_client_bidi_stream_recv(client, stream_id, &recv1) == 0);
        assert(recv1.type == CALLWIRE_INT64 && recv1.val.int_val == 6);
        callwire_value_free(&recv1);

        callwire_value_t send2;
        send2.type = CALLWIRE_INT64;
        send2.val.int_val = 5;
        assert(callwire_client_bidi_stream_send(client, stream_id, &send2) == 0);

        callwire_value_t recv2;
        assert(callwire_client_bidi_stream_recv(client, stream_id, &recv2) == 0);
        assert(recv2.type == CALLWIRE_INT64 && recv2.val.int_val == 10);
        callwire_value_free(&recv2);

        assert(callwire_client_bidi_stream_close_send(client, stream_id) == 0);
        printf("test_bidi_echo_double: OK (3->6, 5->10)\n");
    }

    callwire_client_close(client);
    callwire_server_close(g_server);
    pthread_join(server_thread, NULL);

    printf("All streaming tests passed.\n");
    return 0;
}
