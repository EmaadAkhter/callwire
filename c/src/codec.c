#include "callwire.h"
#include "../third_party/mpack_stub.h"
#include <stdlib.h>
#include <string.h>

#define MSGPACK_BUF_SIZE 65536

int callwire_codec_encode_request(uint64_t id, const char *func,
                                   callwire_value_t *args, size_t args_count,
                                   uint8_t **payload_out, size_t *len_out) {
    uint8_t *buf = malloc(MSGPACK_BUF_SIZE);
    if (!buf) return -1;

    mpack_writer_t w = {buf, 0, MSGPACK_BUF_SIZE};

    /* Encode msgpack map: {id, type, func, args} */
    mpack_write_map_header(&w, 4);

    mpack_write_str(&w, "id", 2);
    mpack_write_u64(&w, id);

    mpack_write_str(&w, "type", 4);
    mpack_write_str(&w, "request", 7);

    mpack_write_str(&w, "func", 4);
    mpack_write_str(&w, func, strlen(func));

    mpack_write_str(&w, "args", 4);
    mpack_write_map_header(&w, args_count);  /* TODO: encode as array, not map */
    for (size_t i = 0; i < args_count; i++) {
        /* TODO: encode each arg value */
        mpack_write_nil(&w);
    }

    *payload_out = buf;
    *len_out = w.pos;
    return 0;
}

int callwire_codec_encode_response(uint64_t id, callwire_value_t *result,
                                    uint8_t **payload_out, size_t *len_out) {
    /* TODO: encode msgpack map with: id, type="response", result */
    return 0;
}

int callwire_codec_encode_error(uint64_t id, const char *error_type, const char *message,
                                 uint8_t **payload_out, size_t *len_out) {
    /* TODO: encode msgpack map with: id, type="error", error_type, message */
    return 0;
}

int callwire_codec_encode_stream_chunk(uint64_t id, callwire_value_t *result,
                                        uint8_t **payload_out, size_t *len_out) {
    /* TODO: encode msgpack map with: id, type="stream_chunk", result */
    return 0;
}

int callwire_codec_encode_stream_end(uint64_t id,
                                      uint8_t **payload_out, size_t *len_out) {
    /* TODO: encode msgpack map with: id, type="stream_end" */
    return 0;
}

int callwire_codec_encode_stream_close(uint64_t id,
                                        uint8_t **payload_out, size_t *len_out) {
    /* TODO: encode msgpack map with: id, type="stream_close" */
    return 0;
}

int callwire_codec_encode_bidi_request(uint64_t id, const char *func,
                                        callwire_value_t *args, size_t args_count,
                                        uint8_t **payload_out, size_t *len_out) {
    /* TODO: encode msgpack map with: id, type="request", func, args, stream=true */
    return 0;
}

int callwire_codec_decode(const uint8_t *payload, size_t len,
                          callwire_wire_message_t *msg_out) {
    /* TODO: decode msgpack payload into wireMessage struct */
    return 0;
}
