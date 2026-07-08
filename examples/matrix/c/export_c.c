/* C export script: exports "add" on a fixed port. init() performs setup
 * and is called first thing in main(). */
#include "callwire.h"
#include <stdio.h>

#define MATRIX_PORT 9106

CALLWIRE_EXPORT_INT2(add, a, b) { return a + b; }

static callwire_server_t *g_server;

static void init(void) {
    g_server = callwire_server_new("0.0.0.0", MATRIX_PORT);
    callwire_server_export(g_server, "add", add);
}

int main(void) {
    init();
    printf("C matrix export listening on :%d\n", MATRIX_PORT);
    callwire_server_serve(g_server);
    return 0;
}
