/* Minimal self-check for the C core codec: encode a request, decode it back,
 * assert round-trip fidelity. Build: cc -std=c99 -I../include ../src/codec.c test_codec.c -o test_codec */
#include "../include/callwire.h"
#include "../src/internal.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void test_request_roundtrip(void) {
    callwire_value_t args[2];
    args[0].type = CALLWIRE_INT64;
    args[0].val.int_val = 10;
    args[1].type = CALLWIRE_INT64;
    args[1].val.int_val = 20;

    uint8_t *payload;
    size_t len;
    int rc = callwire_codec_encode_request(42, "add", args, 2, &payload, &len);
    assert(rc == 0);

    callwire_wire_message_t msg;
    rc = callwire_codec_decode(payload, len, &msg);
    assert(rc == 0);
    assert(msg.id == 42);
    assert(strcmp(msg.type, "request") == 0);
    assert(strcmp(msg.func, "add") == 0);
    assert(msg.args_count == 2);
    assert(msg.args[0].type == CALLWIRE_INT64 && msg.args[0].val.int_val == 10);
    assert(msg.args[1].type == CALLWIRE_INT64 && msg.args[1].val.int_val == 20);
    assert(msg.stream == 0);

    callwire_wire_message_free(&msg);
    free(payload);
    printf("test_request_roundtrip: OK\n");
}

static void test_bidi_request_roundtrip(void) {
    uint8_t *payload;
    size_t len;
    int rc = callwire_codec_encode_bidi_request(7, "chat", NULL, 0, &payload, &len);
    assert(rc == 0);

    callwire_wire_message_t msg;
    rc = callwire_codec_decode(payload, len, &msg);
    assert(rc == 0);
    assert(msg.id == 7);
    assert(strcmp(msg.func, "chat") == 0);
    assert(msg.stream == 1);

    callwire_wire_message_free(&msg);
    free(payload);
    printf("test_bidi_request_roundtrip: OK\n");
}

static void test_response_with_string_result(void) {
    callwire_value_t result;
    result.type = CALLWIRE_STRING;
    result.val.str_val.data = "hello world";
    result.val.str_val.len = 11;

    uint8_t *payload;
    size_t len;
    int rc = callwire_codec_encode_response(99, &result, &payload, &len);
    assert(rc == 0);

    callwire_wire_message_t msg;
    rc = callwire_codec_decode(payload, len, &msg);
    assert(rc == 0);
    assert(msg.id == 99);
    assert(strcmp(msg.type, "response") == 0);
    assert(msg.result.type == CALLWIRE_STRING);
    assert(msg.result.val.str_val.len == 11);
    assert(memcmp(msg.result.val.str_val.data, "hello world", 11) == 0);

    callwire_wire_message_free(&msg);
    free(payload);
    printf("test_response_with_string_result: OK\n");
}

static void test_nested_array_and_map(void) {
    /* args[0] = [1, 2, 3] (array), args[1] = {"k": true} (map) */
    callwire_value_t inner_items[3];
    for (int i = 0; i < 3; i++) {
        inner_items[i].type = CALLWIRE_INT64;
        inner_items[i].val.int_val = i + 1;
    }

    callwire_value_t map_key, map_val;
    map_key.type = CALLWIRE_STRING;
    map_key.val.str_val.data = "k";
    map_key.val.str_val.len = 1;
    map_val.type = CALLWIRE_BOOL;
    map_val.val.is_true = 1;

    callwire_value_t args[2];
    args[0].type = CALLWIRE_ARRAY;
    args[0].val.array_val.items = (struct callwire_value *)inner_items;
    args[0].val.array_val.count = 3;

    args[1].type = CALLWIRE_MAP;
    args[1].val.map_val.keys = (struct callwire_value *)&map_key;
    args[1].val.map_val.values = (struct callwire_value *)&map_val;
    args[1].val.map_val.count = 1;

    uint8_t *payload;
    size_t len;
    int rc = callwire_codec_encode_request(1, "f", args, 2, &payload, &len);
    assert(rc == 0);

    callwire_wire_message_t msg;
    rc = callwire_codec_decode(payload, len, &msg);
    assert(rc == 0);
    assert(msg.args[0].type == CALLWIRE_ARRAY);
    assert(msg.args[0].val.array_val.count == 3);
    callwire_value_t *decoded_items = (callwire_value_t *)msg.args[0].val.array_val.items;
    assert(decoded_items[0].val.int_val == 1);
    assert(decoded_items[2].val.int_val == 3);

    assert(msg.args[1].type == CALLWIRE_MAP);
    assert(msg.args[1].val.map_val.count == 1);
    callwire_value_t *decoded_keys = (callwire_value_t *)msg.args[1].val.map_val.keys;
    callwire_value_t *decoded_vals = (callwire_value_t *)msg.args[1].val.map_val.values;
    assert(decoded_keys[0].type == CALLWIRE_STRING);
    assert(decoded_vals[0].type == CALLWIRE_BOOL && decoded_vals[0].val.is_true == 1);

    callwire_wire_message_free(&msg);
    free(payload);
    printf("test_nested_array_and_map: OK\n");
}

static void test_error_and_stream_frames(void) {
    uint8_t *payload;
    size_t len;

    assert(callwire_codec_encode_error(5, "NotFoundError", "no such func", &payload, &len) == 0);
    callwire_wire_message_t msg;
    assert(callwire_codec_decode(payload, len, &msg) == 0);
    assert(strcmp(msg.type, "error") == 0);
    assert(strcmp(msg.error_type, "NotFoundError") == 0);
    assert(strcmp(msg.message, "no such func") == 0);
    callwire_wire_message_free(&msg);
    free(payload);

    assert(callwire_codec_encode_stream_end(3, &payload, &len) == 0);
    assert(callwire_codec_decode(payload, len, &msg) == 0);
    assert(strcmp(msg.type, "stream_end") == 0);
    callwire_wire_message_free(&msg);
    free(payload);

    assert(callwire_codec_encode_stream_close(3, &payload, &len) == 0);
    assert(callwire_codec_decode(payload, len, &msg) == 0);
    assert(strcmp(msg.type, "stream_close") == 0);
    callwire_wire_message_free(&msg);
    free(payload);

    printf("test_error_and_stream_frames: OK\n");
}

int main(void) {
    test_request_roundtrip();
    test_bidi_request_roundtrip();
    test_response_with_string_result();
    test_nested_array_and_map();
    test_error_and_stream_frames();
    printf("All codec tests passed.\n");
    return 0;
}
