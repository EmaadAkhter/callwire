#include "internal.h"
#include "third_party/mpack_stub.h"
#include <stdlib.h>
#include <string.h>

#define CALLWIRE_BUF_INITIAL_SIZE 4096

/* ---------------------------------------------------------------------- */
/* Value encode/decode                                                    */
/* ---------------------------------------------------------------------- */

static void encode_value(mpack_writer_t *w, const callwire_value_t *v) {
    if (!v) {
        mpack_write_nil(w);
        return;
    }
    switch (v->type) {
        case CALLWIRE_NULL:
            mpack_write_nil(w);
            break;
        case CALLWIRE_BOOL:
            mpack_write_bool(w, v->val.is_true);
            break;
        case CALLWIRE_INT64:
            mpack_write_int64(w, v->val.int_val);
            break;
        case CALLWIRE_FLOAT64:
            mpack_write_double(w, v->val.float_val);
            break;
        case CALLWIRE_STRING:
            mpack_write_str(w, v->val.str_val.data, v->val.str_val.len);
            break;
        case CALLWIRE_BINARY:
            mpack_write_bin(w, v->val.bin_val.data, v->val.bin_val.len);
            break;
        case CALLWIRE_ARRAY:
            mpack_write_array_header(w, (uint32_t)v->val.array_val.count);
            for (size_t i = 0; i < v->val.array_val.count; i++) {
                encode_value(w, &v->val.array_val.items[i]);
            }
            break;
        case CALLWIRE_MAP:
            mpack_write_map_header(w, (uint32_t)v->val.map_val.count);
            for (size_t i = 0; i < v->val.map_val.count; i++) {
                encode_value(w, &v->val.map_val.keys[i]);
                encode_value(w, &v->val.map_val.values[i]);
            }
            break;
        default:
            mpack_write_nil(w);
            break;
    }
}

/* Decodes one msgpack value from `r` into a freshly allocated callwire_value_t.
 * Caller owns the result and must free it via callwire_value_free(). */
static int decode_value(mpack_reader_t *r, callwire_value_t *out) {
    if (r->error) return -1;

    if (mpack_try_read_nil(r)) {
        out->type = CALLWIRE_NULL;
        return 0;
    }

    int bool_val;
    if (mpack_try_read_bool(r, &bool_val)) {
        out->type = CALLWIRE_BOOL;
        out->val.is_true = bool_val;
        return 0;
    }

    int64_t int_val;
    if (mpack_try_read_int64(r, &int_val)) {
        out->type = CALLWIRE_INT64;
        out->val.int_val = int_val;
        return 0;
    }

    double float_val;
    if (mpack_try_read_double(r, &float_val)) {
        out->type = CALLWIRE_FLOAT64;
        out->val.float_val = float_val;
        return 0;
    }

    const char *str_data;
    size_t str_len;
    if (mpack_try_read_str(r, &str_data, &str_len)) {
        char *copy = malloc(str_len + 1);
        if (!copy) return -1;
        memcpy(copy, str_data, str_len);
        copy[str_len] = '\0';
        out->type = CALLWIRE_STRING;
        out->val.str_val.data = copy;
        out->val.str_val.len = str_len;
        return 0;
    }

    const uint8_t *bin_data;
    size_t bin_len;
    if (mpack_try_read_bin(r, &bin_data, &bin_len)) {
        uint8_t *copy = malloc(bin_len);
        if (!copy && bin_len > 0) return -1;
        if (bin_len > 0) memcpy(copy, bin_data, bin_len);
        out->type = CALLWIRE_BINARY;
        out->val.bin_val.data = copy;
        out->val.bin_val.len = bin_len;
        return 0;
    }

    uint32_t arr_count;
    if (mpack_try_read_array_header(r, &arr_count)) {
        callwire_value_t *items = NULL;
        if (arr_count > 0) {
            items = calloc(arr_count, sizeof(callwire_value_t));
            if (!items) return -1;
        }
        for (uint32_t i = 0; i < arr_count; i++) {
            if (decode_value(r, &items[i]) != 0) {
                free(items);
                return -1;
            }
        }
        out->type = CALLWIRE_ARRAY;
        out->val.array_val.items = (struct callwire_value *)items;
        out->val.array_val.count = arr_count;
        return 0;
    }

    uint32_t map_count;
    if (mpack_try_read_map_header(r, &map_count)) {
        callwire_value_t *keys = NULL;
        callwire_value_t *values = NULL;
        if (map_count > 0) {
            keys = calloc(map_count, sizeof(callwire_value_t));
            values = calloc(map_count, sizeof(callwire_value_t));
            if (!keys || !values) {
                free(keys);
                free(values);
                return -1;
            }
        }
        for (uint32_t i = 0; i < map_count; i++) {
            if (decode_value(r, &keys[i]) != 0 || decode_value(r, &values[i]) != 0) {
                free(keys);
                free(values);
                return -1;
            }
        }
        out->type = CALLWIRE_MAP;
        out->val.map_val.keys = (struct callwire_value *)keys;
        out->val.map_val.values = (struct callwire_value *)values;
        out->val.map_val.count = map_count;
        return 0;
    }

    /* Unknown/unsupported tag. */
    r->error = 1;
    return -1;
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
        case CALLWIRE_ARRAY: {
            callwire_value_t *items = (callwire_value_t *)value->val.array_val.items;
            for (size_t i = 0; i < value->val.array_val.count; i++) {
                callwire_value_free(&items[i]);
            }
            free(items);
            break;
        }
        case CALLWIRE_MAP: {
            callwire_value_t *keys = (callwire_value_t *)value->val.map_val.keys;
            callwire_value_t *values = (callwire_value_t *)value->val.map_val.values;
            for (size_t i = 0; i < value->val.map_val.count; i++) {
                callwire_value_free(&keys[i]);
                callwire_value_free(&values[i]);
            }
            free(keys);
            free(values);
            break;
        }
        default:
            break;
    }
    value->type = CALLWIRE_NULL;
}

/* ---------------------------------------------------------------------- */
/* Growable-buffer helper: encode_value can exceed a fixed-size buffer for
 * large payloads, so each public encode_* function retries with a bigger
 * buffer on overflow rather than truncating silently.                    */
/* ---------------------------------------------------------------------- */

typedef void (*encode_fn_t)(mpack_writer_t *w, void *ctx);

static int encode_with_growth(encode_fn_t fn, void *ctx, uint8_t **payload_out, size_t *len_out) {
    size_t cap = CALLWIRE_BUF_INITIAL_SIZE;
    for (;;) {
        uint8_t *buf = malloc(cap);
        if (!buf) return -1;
        mpack_writer_t w;
        mpack_writer_init(&w, buf, cap);
        fn(&w, ctx);
        if (!w.overflow) {
            *payload_out = buf;
            *len_out = w.pos;
            return 0;
        }
        free(buf);
        cap *= 2;
        if (cap > (64u * 1024u * 1024u)) return -1; /* 64MB safety cap */
    }
}

/* ---------------------------------------------------------------------- */
/* Wire message encoders                                                  */
/* ---------------------------------------------------------------------- */

struct request_ctx {
    uint64_t id;
    const char *func;
    callwire_value_t *args;
    size_t args_count;
    int stream;
};

/* NOTE: ids are always non-negative; encode as msgpack uint via write_int64
 * (write_int64 already picks the correct uint tag for non-negative values). */
static void encode_request_fn2(mpack_writer_t *w, void *ctx_) {
    struct request_ctx *ctx = (struct request_ctx *)ctx_;
    mpack_write_map_header(w, ctx->stream ? 5 : 4);

    mpack_write_str(w, "id", 2);
    mpack_write_int64(w, (int64_t)ctx->id);

    mpack_write_str(w, "type", 4);
    mpack_write_str(w, "request", 7);

    mpack_write_str(w, "func", 4);
    mpack_write_str(w, ctx->func ? ctx->func : "", ctx->func ? strlen(ctx->func) : 0);

    mpack_write_str(w, "args", 4);
    mpack_write_array_header(w, (uint32_t)ctx->args_count);
    for (size_t i = 0; i < ctx->args_count; i++) {
        encode_value(w, &ctx->args[i]);
    }

    if (ctx->stream) {
        mpack_write_str(w, "stream", 6);
        mpack_write_bool(w, 1);
    }
}

int callwire_codec_encode_request(uint64_t id, const char *func,
                                   callwire_value_t *args, size_t args_count,
                                   uint8_t **payload_out, size_t *len_out) {
    struct request_ctx ctx = { id, func, args, args_count, 0 };
    return encode_with_growth(encode_request_fn2, &ctx, payload_out, len_out);
}

int callwire_codec_encode_bidi_request(uint64_t id, const char *func,
                                        callwire_value_t *args, size_t args_count,
                                        uint8_t **payload_out, size_t *len_out) {
    struct request_ctx ctx = { id, func, args, args_count, 1 };
    return encode_with_growth(encode_request_fn2, &ctx, payload_out, len_out);
}

struct response_ctx {
    uint64_t id;
    callwire_value_t *result;
};

static void encode_response_fn(mpack_writer_t *w, void *ctx_) {
    struct response_ctx *ctx = (struct response_ctx *)ctx_;
    mpack_write_map_header(w, 3);
    mpack_write_str(w, "id", 2);
    mpack_write_int64(w, (int64_t)ctx->id);
    mpack_write_str(w, "type", 4);
    mpack_write_str(w, "response", 8);
    mpack_write_str(w, "result", 6);
    encode_value(w, ctx->result);
}

int callwire_codec_encode_response(uint64_t id, callwire_value_t *result,
                                    uint8_t **payload_out, size_t *len_out) {
    struct response_ctx ctx = { id, result };
    return encode_with_growth(encode_response_fn, &ctx, payload_out, len_out);
}

struct error_ctx {
    uint64_t id;
    const char *error_type;
    const char *message;
};

static void encode_error_fn(mpack_writer_t *w, void *ctx_) {
    struct error_ctx *ctx = (struct error_ctx *)ctx_;
    mpack_write_map_header(w, 4);
    mpack_write_str(w, "id", 2);
    mpack_write_int64(w, (int64_t)ctx->id);
    mpack_write_str(w, "type", 4);
    mpack_write_str(w, "error", 5);
    mpack_write_str(w, "error_type", 10);
    mpack_write_str(w, ctx->error_type ? ctx->error_type : "Error",
                     ctx->error_type ? strlen(ctx->error_type) : 5);
    mpack_write_str(w, "message", 7);
    mpack_write_str(w, ctx->message ? ctx->message : "", ctx->message ? strlen(ctx->message) : 0);
}

int callwire_codec_encode_error(uint64_t id, const char *error_type, const char *message,
                                 uint8_t **payload_out, size_t *len_out) {
    struct error_ctx ctx = { id, error_type, message };
    return encode_with_growth(encode_error_fn, &ctx, payload_out, len_out);
}

static void encode_stream_chunk_fn(mpack_writer_t *w, void *ctx_) {
    struct response_ctx *ctx = (struct response_ctx *)ctx_;
    mpack_write_map_header(w, 3);
    mpack_write_str(w, "id", 2);
    mpack_write_int64(w, (int64_t)ctx->id);
    mpack_write_str(w, "type", 4);
    mpack_write_str(w, "stream_chunk", 12);
    mpack_write_str(w, "result", 6);
    encode_value(w, ctx->result);
}

int callwire_codec_encode_stream_chunk(uint64_t id, callwire_value_t *result,
                                        uint8_t **payload_out, size_t *len_out) {
    struct response_ctx ctx = { id, result };
    return encode_with_growth(encode_stream_chunk_fn, &ctx, payload_out, len_out);
}

struct id_only_ctx {
    uint64_t id;
    const char *type_str;
    size_t type_len;
};

static void encode_id_only_fn(mpack_writer_t *w, void *ctx_) {
    struct id_only_ctx *ctx = (struct id_only_ctx *)ctx_;
    mpack_write_map_header(w, 2);
    mpack_write_str(w, "id", 2);
    mpack_write_int64(w, (int64_t)ctx->id);
    mpack_write_str(w, "type", 4);
    mpack_write_str(w, ctx->type_str, ctx->type_len);
}

int callwire_codec_encode_stream_end(uint64_t id,
                                      uint8_t **payload_out, size_t *len_out) {
    struct id_only_ctx ctx = { id, "stream_end", 10 };
    return encode_with_growth(encode_id_only_fn, &ctx, payload_out, len_out);
}

int callwire_codec_encode_stream_close(uint64_t id,
                                        uint8_t **payload_out, size_t *len_out) {
    struct id_only_ctx ctx = { id, "stream_close", 12 };
    return encode_with_growth(encode_id_only_fn, &ctx, payload_out, len_out);
}

/* ---------------------------------------------------------------------- */
/* Decode: msgpack payload -> callwire_wire_message_t                     */
/* ---------------------------------------------------------------------- */

static char *dup_str(const char *data, size_t len) {
    char *copy = malloc(len + 1);
    if (!copy) return NULL;
    memcpy(copy, data, len);
    copy[len] = '\0';
    return copy;
}

int callwire_codec_decode(const uint8_t *payload, size_t len,
                          callwire_wire_message_t *msg_out) {
    mpack_reader_t r;
    mpack_reader_init(&r, payload, len);

    memset(msg_out, 0, sizeof(*msg_out));
    msg_out->result.type = CALLWIRE_NULL;

    uint32_t map_count;
    if (!mpack_try_read_map_header(&r, &map_count)) {
        return -1;
    }

    for (uint32_t i = 0; i < map_count; i++) {
        const char *key_data;
        size_t key_len;
        if (!mpack_try_read_str(&r, &key_data, &key_len)) {
            /* Non-string key: skip key and value, keep going. */
            mpack_skip_value(&r);
            mpack_skip_value(&r);
            continue;
        }

        if (key_len == 2 && memcmp(key_data, "id", 2) == 0) {
            int64_t id_val;
            if (mpack_try_read_int64(&r, &id_val)) {
                msg_out->id = (uint64_t)id_val;
            } else {
                mpack_skip_value(&r);
            }
        } else if (key_len == 4 && memcmp(key_data, "type", 4) == 0) {
            const char *s; size_t sl;
            if (mpack_try_read_str(&r, &s, &sl)) {
                msg_out->type = dup_str(s, sl);
            } else {
                mpack_skip_value(&r);
            }
        } else if (key_len == 4 && memcmp(key_data, "func", 4) == 0) {
            const char *s; size_t sl;
            if (mpack_try_read_str(&r, &s, &sl)) {
                msg_out->func = dup_str(s, sl);
            } else {
                mpack_skip_value(&r);
            }
        } else if (key_len == 4 && memcmp(key_data, "args", 4) == 0) {
            uint32_t count;
            if (mpack_try_read_array_header(&r, &count)) {
                callwire_value_t *items = NULL;
                if (count > 0) items = calloc(count, sizeof(callwire_value_t));
                for (uint32_t j = 0; j < count; j++) {
                    decode_value(&r, &items[j]);
                }
                msg_out->args = items;
                msg_out->args_count = count;
            } else {
                mpack_skip_value(&r);
            }
        } else if (key_len == 6 && memcmp(key_data, "stream", 6) == 0) {
            int b;
            if (mpack_try_read_bool(&r, &b)) {
                msg_out->stream = b;
            } else {
                mpack_skip_value(&r);
            }
        } else if (key_len == 6 && memcmp(key_data, "result", 6) == 0) {
            decode_value(&r, &msg_out->result);
        } else if (key_len == 10 && memcmp(key_data, "error_type", 10) == 0) {
            const char *s; size_t sl;
            if (mpack_try_read_str(&r, &s, &sl)) {
                msg_out->error_type = dup_str(s, sl);
            } else {
                mpack_skip_value(&r);
            }
        } else if (key_len == 7 && memcmp(key_data, "message", 7) == 0) {
            const char *s; size_t sl;
            if (mpack_try_read_str(&r, &s, &sl)) {
                msg_out->message = dup_str(s, sl);
            } else {
                mpack_skip_value(&r);
            }
        } else {
            mpack_skip_value(&r);
        }

        if (r.error) return -1;
    }

    return r.error ? -1 : 0;
}

void callwire_wire_message_free(callwire_wire_message_t *msg) {
    if (!msg) return;
    free((void *)msg->type);
    free((void *)msg->func);
    free((void *)msg->error_type);
    free((void *)msg->message);
    if (msg->args) {
        for (size_t i = 0; i < msg->args_count; i++) {
            callwire_value_free(&msg->args[i]);
        }
        free(msg->args);
    }
    callwire_value_free(&msg->result);
    memset(msg, 0, sizeof(*msg));
}
