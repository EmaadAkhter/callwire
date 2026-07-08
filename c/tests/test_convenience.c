/* Verifies the convenience layer (c/src/convenience.c + CALLWIRE_EXPORT_INTn
 * macros in callwire.h): real TCP round-trip, server registered via macro,
 * client calling via callwire_call_ints/callwire_call_str. */
#include "../include/callwire.h"
#include <assert.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define TEST_PORT 19099

CALLWIRE_EXPORT_INT2(add, a, b) {
    return a + b;
}

CALLWIRE_EXPORT_INT1(negate, a) {
    return -a;
}

CALLWIRE_EXPORT_INT3(sum3, a, b, c) {
    return a + b + c;
}

static int greet_handler(callwire_value_t *args, size_t argc, callwire_value_t *result_out) {
    (void)argc;
    /* Handler results are always freed by the server via callwire_value_free()
     * after encoding, so string data must be heap-allocated (same contract
     * as the C++/COBOL loopback tests this session). */
    char *buf = malloc(128);
    int n = snprintf(buf, 128, "Hello, %.*s!",
                      (int)args[0].val.str_val.len, args[0].val.str_val.data);
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

    assert(callwire_server_export(g_server, "add", add) == 0);
    assert(callwire_server_export(g_server, "negate", negate) == 0);
    assert(callwire_server_export(g_server, "sum3", sum3) == 0);
    assert(callwire_server_export(g_server, "greet", greet_handler) == 0);

    pthread_t server_thread;
    pthread_create(&server_thread, NULL, server_thread_fn, NULL);
    usleep(200 * 1000);

    callwire_client_t *client = callwire_client_connect("127.0.0.1", TEST_PORT);
    assert(client != NULL);

    /* callwire_call_ints: 2-arg macro-exported handler */
    {
        int64_t result;
        int rc = callwire_call_ints(client, "add", (int64_t[]){10, 20}, 2, &result);
        assert(rc == 0);
        assert(result == 30);
        printf("test_export_int2_add: OK (10 + 20 = %lld)\n", (long long)result);
    }

    /* 1-arg macro-exported handler */
    {
        int64_t result;
        int rc = callwire_call_ints(client, "negate", (int64_t[]){42}, 1, &result);
        assert(rc == 0);
        assert(result == -42);
        printf("test_export_int1_negate: OK (-42 = %lld)\n", (long long)result);
    }

    /* 3-arg macro-exported handler */
    {
        int64_t result;
        int rc = callwire_call_ints(client, "sum3", (int64_t[]){1, 2, 3}, 3, &result);
        assert(rc == 0);
        assert(result == 6);
        printf("test_export_int3_sum3: OK (1+2+3 = %lld)\n", (long long)result);
    }

    /* callwire_call_str */
    {
        char result_buf[64];
        int rc = callwire_call_str(client, "greet", "World", result_buf, sizeof(result_buf));
        assert(rc == 0);
        assert(strcmp(result_buf, "Hello, World!") == 0);
        printf("test_call_str_greet: OK (%s)\n", result_buf);
    }

    /* Error path: wrong result type expectation */
    {
        int64_t result;
        int rc = callwire_call_ints(client, "greet", (int64_t[]){1}, 1, &result);
        assert(rc == -1);
        printf("test_call_ints_type_mismatch: OK (correctly rejected)\n");
    }

    callwire_client_close(client);
    callwire_server_close(g_server);
    pthread_join(server_thread, NULL);

    printf("All convenience-layer tests passed.\n");
    return 0;
}
