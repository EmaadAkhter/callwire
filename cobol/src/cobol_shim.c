/* COBOL-friendly shim over the Callwire C core ABI.
 *
 * Rationale: callwire_value_t is a tagged union with C struct padding that
 * would need to be hand-replicated field-for-field (including alignment) in
 * COBOL's DATA DIVISION to call the C core directly — a fragile, silently
 * dangerous approach if the layouts ever drift out of sync (wrong padding
 * reads garbage, not a crash). Instead, this shim exposes flat, COBOL-native
 * parameter shapes (fixed-size int64 arrays, C strings) and builds the real
 * callwire_value_t structs on the C side, where the layout is guaranteed
 * correct by construction.
 *
 * Scope: unary calls only, integer and string values only. This matches
 * COBOL's typical legacy-integration use case (call a modern service, pass
 * numbers/strings, get numbers/strings back) rather than the full streaming
 * + arbitrary-nested-value ABI the other SDKs expose.
 */
#include "../../c/include/callwire.h"
#include <stdlib.h>
#include <string.h>

#define COBOL_MAX_ARGS 16

/* Connect to a callwire server. addr/func strings must be null-terminated
 * (COBOL callers: null-terminate PIC X fields before calling, e.g. via
 * STRING ... LOW-VALUE DELIMITED BY SIZE INTO ...). Returns an opaque
 * client handle (NULL on failure — check callwire_cobol_last_error()). */
void *callwire_cobol_connect(const char *addr, int port) {
    return callwire_client_connect(addr, port);
}

void callwire_cobol_close(void *client) {
    if (client) callwire_client_close((callwire_client_t *)client);
}

/* Unary call with integer args, integer result.
 * args: array of argc int64 values (argc capped at COBOL_MAX_ARGS).
 * result_out: written with the int64 result on success.
 * Returns 0 on success, -1 on failure (see callwire_cobol_last_error()). */
int callwire_cobol_call_ints(void *client, const char *func,
                              const int64_t *args, int argc,
                              int64_t *result_out) {
    if (!client || !func || argc < 0 || argc > COBOL_MAX_ARGS) {
        return -1;
    }

    callwire_value_t cargs[COBOL_MAX_ARGS];
    for (int i = 0; i < argc; i++) {
        cargs[i].type = CALLWIRE_INT64;
        cargs[i].val.int_val = args[i];
    }

    callwire_value_t result;
    int rc = callwire_client_call((callwire_client_t *)client, func,
                                   argc > 0 ? cargs : NULL, (size_t)argc, &result);
    if (rc != 0) {
        return -1;
    }

    if (result.type != CALLWIRE_INT64) {
        callwire_value_free(&result);
        return -1; /* server returned a non-integer result */
    }

    *result_out = result.val.int_val;
    callwire_value_free(&result);
    return 0;
}

/* Unary call with a single string arg, string result.
 * arg: null-terminated input string (pass NULL/empty for no-arg calls).
 * result_buf: caller-provided buffer, result_buf_len bytes.
 * Returns 0 on success (result_buf is null-terminated, truncated if it
 * doesn't fit), -1 on failure. */
int callwire_cobol_call_str(void *client, const char *func, const char *arg,
                             char *result_buf, int result_buf_len) {
    if (!client || !func || !result_buf || result_buf_len <= 0) {
        return -1;
    }

    callwire_value_t cargs[1];
    size_t argc = 0;
    if (arg && arg[0] != '\0') {
        cargs[0].type = CALLWIRE_STRING;
        cargs[0].val.str_val.data = arg;
        cargs[0].val.str_val.len = strlen(arg);
        argc = 1;
    }

    callwire_value_t result;
    int rc = callwire_client_call((callwire_client_t *)client, func,
                                   argc > 0 ? cargs : NULL, argc, &result);
    if (rc != 0) {
        return -1;
    }

    if (result.type != CALLWIRE_STRING) {
        callwire_value_free(&result);
        return -1;
    }

    size_t copy_len = result.val.str_val.len;
    if (copy_len >= (size_t)result_buf_len) {
        copy_len = (size_t)result_buf_len - 1;
    }
    memcpy(result_buf, result.val.str_val.data, copy_len);
    result_buf[copy_len] = '\0';

    callwire_value_free(&result);
    return 0;
}

const char *callwire_cobol_last_error(void) {
    return callwire_error();
}
