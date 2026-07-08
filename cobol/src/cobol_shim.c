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
#include <libcob.h>
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

/* ---------------------------------------------------------------------- */
/* Server-side export: register a COBOL subprogram as an RPC handler.     */
/*                                                                         */
/* Design: rather than passing a raw ADDRESS OF <paragraph> function       */
/* pointer from COBOL (untested territory — GnuCOBOL entry-point calling  */
/* convention details, stack setup expectations, etc.), the handler is a  */
/* SEPARATE COBOL subprogram compiled as its own dynamically-loadable      */
/* module (PROGRAM-ID X, `cobc -m`), and this shim registers it by NAME,  */
/* dispatching through libcob's own cob_call() — the same mechanism        */
/* GnuCOBOL itself uses for dynamic CALLs. Verified working end-to-end     */
/* (real TCP round trip, C++ client -> COBOL server, see                  */
/* cobol/tests/test_server.cob) before committing to this shape over the  */
/* address-of approach originally sketched in planning.                   */
/*                                                                         */
/* GOTCHA (cost real debugging time — worth flagging): libcob's dynamic    */
/* module loader matches the compiled .dylib's BASENAME against the       */
/* CALLed name, lowercased, with dashes PRESERVED (not converted to        */
/* underscores). PROGRAM-ID ADD-HANDLER compiled via `cobc -m` must be     */
/* output as `add-handler.dylib` (dash), not `add_handler.dylib`           */
/* (underscore) — the latter fails with "module 'ADD-HANDLER' not found"   */
/* even though the file exists and cob_init() succeeded. The module must   */
/* also be on COB_LIBRARY_PATH at runtime (or preloaded via COB_PRELOAD).  */
/* ---------------------------------------------------------------------- */

static int cobol_runtime_initialized = 0;
static void ensure_cob_init(void) {
    if (!cobol_runtime_initialized) {
        cob_init(0, NULL);
        cobol_runtime_initialized = 1;
    }
}

typedef struct {
    char program_name[64];
} cobol_handler_ctx_t;

/* Handler: COBOL subprogram signature `PROCEDURE DIVISION USING A B RESULT`
 * with A, B, RESULT all PIC S9(18) COMP-5 (int64). */
static int cobol_int2_trampoline(void *userdata, callwire_value_t *args, size_t argc, callwire_value_t *result_out) {
    cobol_handler_ctx_t *ctx = (cobol_handler_ctx_t *)userdata;
    if (argc != 2 || args[0].type != CALLWIRE_INT64 || args[1].type != CALLWIRE_INT64) {
        return -1;
    }

    int64_t a = args[0].val.int_val;
    int64_t b = args[1].val.int_val;
    int64_t result = 0;
    void *cob_args[3] = { &a, &b, &result };

    int rc = cob_call(ctx->program_name, 3, cob_args);
    if (rc != 0) {
        return -1;
    }

    result_out->type = CALLWIRE_INT64;
    result_out->val.int_val = result;
    return 0;
}

/* Handler: COBOL subprogram signature `PROCEDURE DIVISION USING NAME RESULT`
 * with NAME/RESULT both PIC X(N) — fixed-size, space-padded (not null-
 * terminated; COBOL native string convention). Fixed at 256 bytes each,
 * matching the buffer sizes documented in cobol/README.md. */
static int cobol_str1_trampoline(void *userdata, callwire_value_t *args, size_t argc, callwire_value_t *result_out) {
    cobol_handler_ctx_t *ctx = (cobol_handler_ctx_t *)userdata;
    if (argc != 1 || args[0].type != CALLWIRE_STRING) {
        return -1;
    }

    char in_buf[256];
    memset(in_buf, ' ', sizeof(in_buf));
    size_t copy_len = args[0].val.str_val.len;
    if (copy_len > sizeof(in_buf)) copy_len = sizeof(in_buf);
    memcpy(in_buf, args[0].val.str_val.data, copy_len);

    char out_buf[256];
    memset(out_buf, ' ', sizeof(out_buf));
    void *cob_args[2] = { in_buf, out_buf };

    int rc = cob_call(ctx->program_name, 2, cob_args);
    if (rc != 0) {
        return -1;
    }

    /* Trim trailing spaces (COBOL PIC X convention) before returning. */
    size_t out_len = sizeof(out_buf);
    while (out_len > 0 && out_buf[out_len - 1] == ' ') out_len--;

    char *heap_str = malloc(out_len + 1);
    memcpy(heap_str, out_buf, out_len);
    heap_str[out_len] = '\0';

    result_out->type = CALLWIRE_STRING;
    result_out->val.str_val.data = heap_str;
    result_out->val.str_val.len = out_len;
    return 0;
}

int callwire_cobol_export_int2(void *server, const char *func, const char *cobol_program_name) {
    if (!server || !func || !cobol_program_name) {
        return -1;
    }
    ensure_cob_init();

    cobol_handler_ctx_t *ctx = malloc(sizeof(cobol_handler_ctx_t));
    if (!ctx) return -1;
    strncpy(ctx->program_name, cobol_program_name, sizeof(ctx->program_name) - 1);
    ctx->program_name[sizeof(ctx->program_name) - 1] = '\0';

    return callwire_server_export_ctx((callwire_server_t *)server, func, ctx, cobol_int2_trampoline);
}

int callwire_cobol_export_str1(void *server, const char *func, const char *cobol_program_name) {
    if (!server || !func || !cobol_program_name) {
        return -1;
    }
    ensure_cob_init();

    cobol_handler_ctx_t *ctx = malloc(sizeof(cobol_handler_ctx_t));
    if (!ctx) return -1;
    strncpy(ctx->program_name, cobol_program_name, sizeof(ctx->program_name) - 1);
    ctx->program_name[sizeof(ctx->program_name) - 1] = '\0';

    return callwire_server_export_ctx((callwire_server_t *)server, func, ctx, cobol_str1_trampoline);
}

void *callwire_cobol_server_new(const char *addr, int port) {
    return callwire_server_new(addr, port);
}

int callwire_cobol_server_serve(void *server) {
    return callwire_server_serve((callwire_server_t *)server);
}

void callwire_cobol_server_close(void *server) {
    if (server) callwire_server_close((callwire_server_t *)server);
}
