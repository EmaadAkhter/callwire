#include "internal.h"
#include <stdarg.h>
#include <stdio.h>
#include <string.h>

#define CALLWIRE_ERROR_MAX 256

static __thread char callwire_error_buf[CALLWIRE_ERROR_MAX] = {0};

const char *callwire_error(void) {
    return callwire_error_buf[0] ? callwire_error_buf : "unknown error";
}

/* Internal: set the thread-local last-error message. Not part of the public
 * ABI (not declared in callwire.h) — used by client.c/server.c to record
 * failures before returning an error code to the caller. */
void callwire_error_set(const char *fmt, ...) {
    va_list args;
    va_start(args, fmt);
    vsnprintf(callwire_error_buf, CALLWIRE_ERROR_MAX, fmt, args);
    va_end(args);
}
