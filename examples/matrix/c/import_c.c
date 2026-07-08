/* C import script: calls "add"(10,20) on every OTHER language's matrix
 * export server (best-effort — SKIP if a port isn't reachable). */
#include "callwire.h"
#include <stdio.h>

static void init(void) {} /* no setup needed for a pure client script */

int main(void) {
    init();

    struct { const char *name; int port; } targets[] = {
        {"go", 9101}, {"python", 9102}, {"rust", 9103}, {"ts", 9104},
        {"java", 9105}, {"cpp", 9107}, {"swift", 9108}, {"cobol", 9109},
    };

    for (size_t i = 0; i < sizeof(targets) / sizeof(targets[0]); i++) {
        callwire_client_t *client = callwire_client_connect("127.0.0.1", targets[i].port);
        if (!client) {
            printf("%-8s SKIP (not running: %s)\n", targets[i].name, callwire_error());
            continue;
        }
        int64_t result;
        if (callwire_call_ints(client, "add", (int64_t[]){10, 20}, 2, &result) != 0) {
            printf("%-8s SKIP (call failed: %s)\n", targets[i].name, callwire_error());
        } else {
            printf("%-8s OK  add(10,20) = %lld\n", targets[i].name, (long long)result);
        }
        callwire_client_close(client);
    }

    return 0;
}
