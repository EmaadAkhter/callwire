/* End-to-end loopback test: real server thread + real client connection over
 * TCP on 127.0.0.1, exercising callwire_server_export + callwire_client_call.
 * Build: cc -std=c99 -pthread -Isrc -Iinclude src/codec.c src/framing.c
 *   src/client.c src/server.c src/errors.c tests/test_loopback.c -o test_loopback
 */
#include "../include/callwire.h"
#include <assert.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define TEST_PORT 19099

static int add_handler(callwire_value_t *args, size_t args_count, callwire_value_t *result_out) {
    if (args_count != 2 || args[0].type != CALLWIRE_INT64 || args[1].type != CALLWIRE_INT64) {
        return -1;
    }
    result_out->type = CALLWIRE_INT64;
    result_out->val.int_val = args[0].val.int_val + args[1].val.int_val;
    return 0;
}

static int greet_handler(callwire_value_t *args, size_t args_count, callwire_value_t *result_out) {
    if (args_count != 1 || args[0].type != CALLWIRE_STRING) {
        return -1;
    }
    /* Handler results are always freed by the server via callwire_value_free()
     * after encoding, so string/binary data must be heap-allocated. */
    char *buf = malloc(128);
    int n = snprintf(buf, 128, "Hello, %.*s!", (int)args[0].val.str_val.len, args[0].val.str_val.data);
    result_out->type = CALLWIRE_STRING;
    result_out->val.str_val.data = buf;
    result_out->val.str_val.len = (size_t)n;
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

    assert(callwire_server_export(g_server, "add", add_handler) == 0);
    assert(callwire_server_export(g_server, "greet", greet_handler) == 0);

    pthread_t server_thread;
    pthread_create(&server_thread, NULL, server_thread_fn, NULL);
    usleep(100 * 1000); /* let the listener bind before the client connects */

    callwire_client_t *client = callwire_client_connect("127.0.0.1", TEST_PORT);
    assert(client != NULL);

    /* Unary call: add(10, 20) == 30 */
    {
        callwire_value_t args[2];
        args[0].type = CALLWIRE_INT64;
        args[0].val.int_val = 10;
        args[1].type = CALLWIRE_INT64;
        args[1].val.int_val = 20;

        callwire_value_t result;
        int rc = callwire_client_call(client, "add", args, 2, &result);
        assert(rc == 0);
        assert(result.type == CALLWIRE_INT64);
        assert(result.val.int_val == 30);
        callwire_value_free(&result);
        printf("test_unary_add: OK (10 + 20 = %lld)\n", (long long)30);
    }

    /* Unary call with string args/result: greet("World") == "Hello, World!" */
    {
        callwire_value_t args[1];
        args[0].type = CALLWIRE_STRING;
        args[0].val.str_val.data = "World";
        args[0].val.str_val.len = 5;

        callwire_value_t result;
        int rc = callwire_client_call(client, "greet", args, 1, &result);
        assert(rc == 0);
        assert(result.type == CALLWIRE_STRING);
        assert(memcmp(result.val.str_val.data, "Hello, World!", 13) == 0);
        callwire_value_free(&result);
        printf("test_unary_greet: OK\n");
    }

    /* Error path: calling an unexported function. */
    {
        callwire_value_t result;
        int rc = callwire_client_call(client, "nonexistent", NULL, 0, &result);
        assert(rc == -1);
        printf("test_unary_not_found: OK (error: %s)\n", callwire_error());
    }

    callwire_client_close(client);
    callwire_server_close(g_server);
    pthread_join(server_thread, NULL);

    printf("All loopback tests passed.\n");
    return 0;
}
