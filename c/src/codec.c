#include "callwire.h"
#include <stdlib.h>
#include <string.h>

/* Stub msgpack codec. Real implementation uses mpack (single-file msgpack lib).
 * For now, this is a placeholder showing the API surface. */

int callwire_codec_encode_request(uint64_t id, const char *func,
                                   callwire_value_t *args, size_t args_count,
                                   uint8_t **payload_out, size_t *len_out) {
    /* TODO: use mpack to encode a msgpack map with:
     *   id, type="request", func, args (array of values), stream=false
     */
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
