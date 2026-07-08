/* mpack minimal stub for msgpack encoding/decoding
   Real: https://github.com/ludocode/mpack (single header, MIT)
   For now: basic msgpack packing/unpacking functions
*/

#ifndef MPACK_STUB_H
#define MPACK_STUB_H

#include <stdint.h>
#include <stddef.h>
#include <string.h>

/* Minimal msgpack encoder */
typedef struct {
    uint8_t *buf;
    size_t pos;
    size_t cap;
} mpack_writer_t;

static inline void mpack_write_u8(mpack_writer_t *w, uint8_t v) {
    if (w->pos < w->cap) w->buf[w->pos++] = v;
}

static inline void mpack_write_u32(mpack_writer_t *w, uint32_t v) {
    mpack_write_u8(w, (v >> 24) & 0xFF);
    mpack_write_u8(w, (v >> 16) & 0xFF);
    mpack_write_u8(w, (v >> 8) & 0xFF);
    mpack_write_u8(w, v & 0xFF);
}

static inline void mpack_write_u64(mpack_writer_t *w, uint64_t v) {
    mpack_write_u32(w, (v >> 32) & 0xFFFFFFFF);
    mpack_write_u32(w, v & 0xFFFFFFFF);
}

static inline void mpack_write_bin(mpack_writer_t *w, const uint8_t *data, size_t len) {
    memcpy(&w->buf[w->pos], data, len);
    w->pos += len;
}

/* Map header: fixmap or map16/map32 */
static inline void mpack_write_map_header(mpack_writer_t *w, uint32_t count) {
    if (count < 16) {
        mpack_write_u8(w, 0x80 | count);
    } else if (count < 65536) {
        mpack_write_u8(w, 0xDE);
        mpack_write_u8(w, (count >> 8) & 0xFF);
        mpack_write_u8(w, count & 0xFF);
    } else {
        mpack_write_u8(w, 0xDF);
        mpack_write_u32(w, count);
    }
}

/* Nil, boolean, int, string helpers */
static inline void mpack_write_nil(mpack_writer_t *w) {
    mpack_write_u8(w, 0xC0);
}

static inline void mpack_write_true(mpack_writer_t *w) {
    mpack_write_u8(w, 0xC3);
}

static inline void mpack_write_false(mpack_writer_t *w) {
    mpack_write_u8(w, 0xC2);
}

static inline void mpack_write_int64(mpack_writer_t *w, int64_t v) {
    if (v >= 0) {
        mpack_write_u8(w, 0xCF);
        mpack_write_u64(w, (uint64_t)v);
    } else {
        mpack_write_u8(w, 0xD3);
        mpack_write_u64(w, (uint64_t)v);
    }
}

static inline void mpack_write_str(mpack_writer_t *w, const char *str, size_t len) {
    if (len < 32) {
        mpack_write_u8(w, 0xA0 | len);
    } else if (len < 65536) {
        mpack_write_u8(w, 0xD9);
        mpack_write_u8(w, (len >> 8) & 0xFF);
        mpack_write_u8(w, len & 0xFF);
    } else {
        mpack_write_u8(w, 0xDA);
        mpack_write_u32(w, len);
    }
    mpack_write_bin(w, (const uint8_t *)str, len);
}

#endif
