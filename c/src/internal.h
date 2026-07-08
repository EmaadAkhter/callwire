#ifndef CALLWIRE_INTERNAL_H
#define CALLWIRE_INTERNAL_H

#include "../include/callwire.h"

/* Codec: encode wire messages to msgpack payloads. Caller frees *payload_out with free(). */
int callwire_codec_encode_request(uint64_t id, const char *func,
                                   callwire_value_t *args, size_t args_count,
                                   uint8_t **payload_out, size_t *len_out);
int callwire_codec_encode_bidi_request(uint64_t id, const char *func,
                                        callwire_value_t *args, size_t args_count,
                                        uint8_t **payload_out, size_t *len_out);
int callwire_codec_encode_response(uint64_t id, callwire_value_t *result,
                                    uint8_t **payload_out, size_t *len_out);
int callwire_codec_encode_error(uint64_t id, const char *error_type, const char *message,
                                 uint8_t **payload_out, size_t *len_out);
int callwire_codec_encode_stream_chunk(uint64_t id, callwire_value_t *result,
                                        uint8_t **payload_out, size_t *len_out);
int callwire_codec_encode_stream_end(uint64_t id,
                                      uint8_t **payload_out, size_t *len_out);
int callwire_codec_encode_stream_close(uint64_t id,
                                        uint8_t **payload_out, size_t *len_out);

/* Codec: decode a msgpack payload into a wire message. Caller must free via
 * callwire_wire_message_free() (frees func/type/error_type/message/args/result). */
int callwire_codec_decode(const uint8_t *payload, size_t len,
                          callwire_wire_message_t *msg_out);
void callwire_wire_message_free(callwire_wire_message_t *msg);

/* Framing: length-prefixed frame I/O over a connected socket fd. */
int callwire_framing_read_frame(int sockfd, uint8_t **payload_out, size_t *len_out);
int callwire_framing_write_frame(int sockfd, const uint8_t *payload, size_t len);

/* Errors: thread-local last-error message (printf-style). */
void callwire_error_set(const char *fmt, ...);

#endif /* CALLWIRE_INTERNAL_H */
