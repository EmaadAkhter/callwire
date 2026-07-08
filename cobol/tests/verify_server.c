/* Minimal C client used by cobol/build.sh to automatically verify the
 * COBOL-hosted server round trip (no g++/C++ dependency needed just to
 * confirm the build works end-to-end). */
#include "../../c/include/callwire.h"
#include <stdio.h>
#include <string.h>

int main(void) {
    callwire_client_t *client = callwire_client_connect("127.0.0.1", 19499);
    if (!client) {
        fprintf(stderr, "FAIL: connect: %s\n", callwire_error());
        return 1;
    }

    int64_t result;
    if (callwire_call_ints(client, "add", (int64_t[]){10, 20}, 2, &result) != 0) {
        fprintf(stderr, "FAIL: add: %s\n", callwire_error());
        return 1;
    }
    if (result != 30) {
        fprintf(stderr, "FAIL: add(10,20) = %lld, expected 30\n", (long long)result);
        return 1;
    }
    printf("add(10, 20) = %lld\n", (long long)result);

    char greet_result[64];
    if (callwire_call_str(client, "greet", "World", greet_result, sizeof(greet_result)) != 0) {
        fprintf(stderr, "FAIL: greet: %s\n", callwire_error());
        return 1;
    }
    if (strcmp(greet_result, "Hello, World!") != 0) {
        fprintf(stderr, "FAIL: greet(World) = '%s', expected 'Hello, World!'\n", greet_result);
        return 1;
    }
    printf("greet(\"World\") = %s\n", greet_result);

    callwire_client_close(client);
    printf("COBOL server round-trip: PASSED\n");
    return 0;
}
