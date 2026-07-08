#include "callwire.h"
#include <string.h>
#include <pthread.h>

#define CALLWIRE_ERROR_MAX 256

static __thread char callwire_error_buf[CALLWIRE_ERROR_MAX] = {0};

const char *callwire_error(void) {
    return callwire_error_buf[0] ? callwire_error_buf : "unknown error";
}

void callwire_error_set(const char *fmt, ...) {
    /* TODO: implement va_list formatting into callwire_error_buf */
}

void callwire_value_free(callwire_value_t *value) {
    if (!value) return;
    switch (value->type) {
    case CALLWIRE_STRING:
        free((void *)value->val.str_val.data);
        break;
    case CALLWIRE_BINARY:
        free((void *)value->val.bin_val.data);
        break;
    case CALLWIRE_ARRAY:
        for (size_t i = 0; i < value->val.array_val.count; i++) {
            callwire_value_free(&value->val.array_val.items[i]);
        }
        free(value->val.array_val.items);
        break;
    case CALLWIRE_MAP:
        for (size_t i = 0; i < value->val.map_val.count; i++) {
            callwire_value_free(&value->val.map_val.keys[i]);
            callwire_value_free(&value->val.map_val.values[i]);
        }
        free(value->val.map_val.keys);
        free(value->val.map_val.values);
        break;
    default:
        break;
    }
    memset(value, 0, sizeof(*value));
}
