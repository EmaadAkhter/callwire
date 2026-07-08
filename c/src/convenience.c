/* Convenience layer over the C core ABI: reduces the common "few int/string
 * args, single result" call shape to one function call instead of manually
 * building a callwire_value_t[] array. Same principle as cobol/src/cobol_shim.c
 * (flat parameter shapes, no tagged union exposed to the caller) — this is
 * that shim's counterpart for plain C callers, not just COBOL.
 *
 * Scope: unary calls, up to CALLWIRE_MAX_CONVENIENCE_ARGS int64 args, or a
 * single string arg. Anything beyond that (mixed types, arrays, >8 args)
 * uses the full callwire_client_call() ABI directly — already exists, no
 * regression.
 */
#include "internal.h"
#include <string.h>

#define CALLWIRE_MAX_CONVENIENCE_ARGS 8

int callwire_call_ints(callwire_client_t *client, const char *func,
                        const int64_t *args, size_t argc, int64_t *result_out) {
    if (!client || !func || argc > CALLWIRE_MAX_CONVENIENCE_ARGS) {
        return -1;
    }

    callwire_value_t cargs[CALLWIRE_MAX_CONVENIENCE_ARGS];
    for (size_t i = 0; i < argc; i++) {
        cargs[i].type = CALLWIRE_INT64;
        cargs[i].val.int_val = args[i];
    }

    callwire_value_t result;
    int rc = callwire_client_call(client, func, argc > 0 ? cargs : NULL, argc, &result);
    if (rc != 0) {
        return -1;
    }

    if (result.type != CALLWIRE_INT64) {
        callwire_value_free(&result);
        return -1;
    }

    *result_out = result.val.int_val;
    callwire_value_free(&result);
    return 0;
}

int callwire_call_str(callwire_client_t *client, const char *func, const char *arg,
                       char *result_buf, size_t result_buf_len) {
    if (!client || !func || !result_buf || result_buf_len == 0) {
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
    int rc = callwire_client_call(client, func, argc > 0 ? cargs : NULL, argc, &result);
    if (rc != 0) {
        return -1;
    }

    if (result.type != CALLWIRE_STRING) {
        callwire_value_free(&result);
        return -1;
    }

    size_t copy_len = result.val.str_val.len;
    if (copy_len >= result_buf_len) {
        copy_len = result_buf_len - 1;
    }
    memcpy(result_buf, result.val.str_val.data, copy_len);
    result_buf[copy_len] = '\0';

    callwire_value_free(&result);
    return 0;
}
