/* Minimal msgpack encoder/decoder for the Callwire C core.
 * Not a general-purpose library — implements only what the wire protocol needs:
 * nil, bool, int64 (via fixint/uint8/uint16/uint32/uint64/int8/int16/int32/int64),
 * float64, str8/16/32, bin8/16/32, fixarray/array16/array32, fixmap/map16/map32.
 * For a production-grade dependency, swap this for github.com/ludocode/mpack.
 */

#ifndef MPACK_STUB_H
#define MPACK_STUB_H

#include <stdint.h>
#include <stddef.h>
#include <string.h>
#include <stdlib.h>

/* ---------------------------------------------------------------------- */
/* Writer                                                                  */
/* ---------------------------------------------------------------------- */

typedef struct {
    uint8_t *buf;
    size_t pos;
    size_t cap;
    int overflow; /* set to 1 if a write attempted to exceed cap */
} mpack_writer_t;

static inline void mpack_writer_init(mpack_writer_t *w, uint8_t *buf, size_t cap) {
    w->buf = buf;
    w->pos = 0;
    w->cap = cap;
    w->overflow = 0;
}

static inline void mpack_write_raw(mpack_writer_t *w, const void *data, size_t len) {
    if (w->pos + len > w->cap) {
        w->overflow = 1;
        return;
    }
    memcpy(w->buf + w->pos, data, len);
    w->pos += len;
}

static inline void mpack_write_u8(mpack_writer_t *w, uint8_t v) {
    mpack_write_raw(w, &v, 1);
}

static inline void mpack_write_u16(mpack_writer_t *w, uint16_t v) {
    uint8_t b[2] = { (uint8_t)(v >> 8), (uint8_t)v };
    mpack_write_raw(w, b, 2);
}

static inline void mpack_write_u32(mpack_writer_t *w, uint32_t v) {
    uint8_t b[4] = { (uint8_t)(v >> 24), (uint8_t)(v >> 16), (uint8_t)(v >> 8), (uint8_t)v };
    mpack_write_raw(w, b, 4);
}

static inline void mpack_write_u64(mpack_writer_t *w, uint64_t v) {
    mpack_write_u32(w, (uint32_t)(v >> 32));
    mpack_write_u32(w, (uint32_t)v);
}

static inline void mpack_write_nil(mpack_writer_t *w) {
    mpack_write_u8(w, 0xC0);
}

static inline void mpack_write_bool(mpack_writer_t *w, int val) {
    mpack_write_u8(w, val ? 0xC3 : 0xC2);
}

static inline void mpack_write_int64(mpack_writer_t *w, int64_t v) {
    if (v >= 0 && v <= 0x7F) {
        mpack_write_u8(w, (uint8_t)v); /* positive fixint */
    } else if (v < 0 && v >= -32) {
        mpack_write_u8(w, (uint8_t)(0xE0 | (v & 0x1F))); /* negative fixint */
    } else if (v >= 0) {
        if (v <= 0xFF) {
            mpack_write_u8(w, 0xCC);
            mpack_write_u8(w, (uint8_t)v);
        } else if (v <= 0xFFFF) {
            mpack_write_u8(w, 0xCD);
            mpack_write_u16(w, (uint16_t)v);
        } else if (v <= 0xFFFFFFFFLL) {
            mpack_write_u8(w, 0xCE);
            mpack_write_u32(w, (uint32_t)v);
        } else {
            mpack_write_u8(w, 0xCF);
            mpack_write_u64(w, (uint64_t)v);
        }
    } else {
        if (v >= -128) {
            mpack_write_u8(w, 0xD0);
            mpack_write_u8(w, (uint8_t)v);
        } else if (v >= -32768) {
            mpack_write_u8(w, 0xD1);
            mpack_write_u16(w, (uint16_t)v);
        } else if (v >= -2147483648LL) {
            mpack_write_u8(w, 0xD2);
            mpack_write_u32(w, (uint32_t)v);
        } else {
            mpack_write_u8(w, 0xD3);
            mpack_write_u64(w, (uint64_t)v);
        }
    }
}

static inline void mpack_write_double(mpack_writer_t *w, double v) {
    union { double d; uint64_t u; } conv;
    conv.d = v;
    mpack_write_u8(w, 0xCB);
    mpack_write_u64(w, conv.u);
}

static inline void mpack_write_str(mpack_writer_t *w, const char *str, size_t len) {
    if (len < 32) {
        mpack_write_u8(w, (uint8_t)(0xA0 | len));
    } else if (len <= 0xFF) {
        mpack_write_u8(w, 0xD9);
        mpack_write_u8(w, (uint8_t)len);
    } else if (len <= 0xFFFF) {
        mpack_write_u8(w, 0xDA);
        mpack_write_u16(w, (uint16_t)len);
    } else {
        mpack_write_u8(w, 0xDB);
        mpack_write_u32(w, (uint32_t)len);
    }
    mpack_write_raw(w, str, len);
}

static inline void mpack_write_bin(mpack_writer_t *w, const uint8_t *data, size_t len) {
    if (len <= 0xFF) {
        mpack_write_u8(w, 0xC4);
        mpack_write_u8(w, (uint8_t)len);
    } else if (len <= 0xFFFF) {
        mpack_write_u8(w, 0xC5);
        mpack_write_u16(w, (uint16_t)len);
    } else {
        mpack_write_u8(w, 0xC6);
        mpack_write_u32(w, (uint32_t)len);
    }
    mpack_write_raw(w, data, len);
}

static inline void mpack_write_array_header(mpack_writer_t *w, uint32_t count) {
    if (count < 16) {
        mpack_write_u8(w, (uint8_t)(0x90 | count));
    } else if (count <= 0xFFFF) {
        mpack_write_u8(w, 0xDC);
        mpack_write_u16(w, (uint16_t)count);
    } else {
        mpack_write_u8(w, 0xDD);
        mpack_write_u32(w, count);
    }
}

static inline void mpack_write_map_header(mpack_writer_t *w, uint32_t count) {
    if (count < 16) {
        mpack_write_u8(w, (uint8_t)(0x80 | count));
    } else if (count <= 0xFFFF) {
        mpack_write_u8(w, 0xDE);
        mpack_write_u16(w, (uint16_t)count);
    } else {
        mpack_write_u8(w, 0xDF);
        mpack_write_u32(w, count);
    }
}

/* ---------------------------------------------------------------------- */
/* Reader                                                                  */
/* ---------------------------------------------------------------------- */

typedef struct {
    const uint8_t *buf;
    size_t pos;
    size_t len;
    int error; /* set to 1 on any out-of-bounds read or unsupported tag */
} mpack_reader_t;

static inline void mpack_reader_init(mpack_reader_t *r, const uint8_t *buf, size_t len) {
    r->buf = buf;
    r->pos = 0;
    r->len = len;
    r->error = 0;
}

static inline int mpack_reader_remaining(mpack_reader_t *r) {
    return (int)(r->len - r->pos);
}

static inline uint8_t mpack_read_u8(mpack_reader_t *r) {
    if (r->pos + 1 > r->len) { r->error = 1; return 0; }
    return r->buf[r->pos++];
}

static inline uint16_t mpack_read_u16(mpack_reader_t *r) {
    if (r->pos + 2 > r->len) { r->error = 1; return 0; }
    uint16_t v = ((uint16_t)r->buf[r->pos] << 8) | r->buf[r->pos + 1];
    r->pos += 2;
    return v;
}

static inline uint32_t mpack_read_u32(mpack_reader_t *r) {
    if (r->pos + 4 > r->len) { r->error = 1; return 0; }
    uint32_t v = ((uint32_t)r->buf[r->pos] << 24) | ((uint32_t)r->buf[r->pos + 1] << 16) |
                 ((uint32_t)r->buf[r->pos + 2] << 8) | r->buf[r->pos + 3];
    r->pos += 4;
    return v;
}

static inline uint64_t mpack_read_u64(mpack_reader_t *r) {
    uint64_t hi = mpack_read_u32(r);
    uint64_t lo = mpack_read_u32(r);
    return (hi << 32) | lo;
}

static inline const uint8_t *mpack_read_raw(mpack_reader_t *r, size_t len) {
    if (r->pos + len > r->len) { r->error = 1; return NULL; }
    const uint8_t *p = r->buf + r->pos;
    r->pos += len;
    return p;
}

/* Peeks the next tag byte without consuming it. */
static inline uint8_t mpack_peek_tag(mpack_reader_t *r) {
    if (r->pos >= r->len) { r->error = 1; return 0; }
    return r->buf[r->pos];
}

/* Reads a nil tag (0xC0). Returns 1 if it was nil, 0 otherwise (does not consume on mismatch). */
static inline int mpack_try_read_nil(mpack_reader_t *r) {
    if (mpack_peek_tag(r) == 0xC0) { r->pos++; return 1; }
    return 0;
}

/* Reads a bool tag. Sets *out. Returns 1 on success, 0 if not a bool tag. */
static inline int mpack_try_read_bool(mpack_reader_t *r, int *out) {
    uint8_t tag = mpack_peek_tag(r);
    if (tag == 0xC2) { r->pos++; *out = 0; return 1; }
    if (tag == 0xC3) { r->pos++; *out = 1; return 1; }
    return 0;
}

/* Reads any msgpack integer tag into an int64_t. Returns 1 on success. */
static inline int mpack_try_read_int64(mpack_reader_t *r, int64_t *out) {
    uint8_t tag = mpack_peek_tag(r);
    if (tag <= 0x7F) { r->pos++; *out = tag; return 1; }
    if (tag >= 0xE0) { r->pos++; *out = (int8_t)tag; return 1; }
    switch (tag) {
        case 0xCC: r->pos++; *out = mpack_read_u8(r); return 1;
        case 0xCD: r->pos++; *out = mpack_read_u16(r); return 1;
        case 0xCE: r->pos++; *out = mpack_read_u32(r); return 1;
        case 0xCF: r->pos++; *out = (int64_t)mpack_read_u64(r); return 1;
        case 0xD0: r->pos++; *out = (int8_t)mpack_read_u8(r); return 1;
        case 0xD1: r->pos++; *out = (int16_t)mpack_read_u16(r); return 1;
        case 0xD2: r->pos++; *out = (int32_t)mpack_read_u32(r); return 1;
        case 0xD3: r->pos++; *out = (int64_t)mpack_read_u64(r); return 1;
        default: return 0;
    }
}

static inline int mpack_try_read_double(mpack_reader_t *r, double *out) {
    uint8_t tag = mpack_peek_tag(r);
    if (tag == 0xCB) {
        r->pos++;
        union { double d; uint64_t u; } conv;
        conv.u = mpack_read_u64(r);
        *out = conv.d;
        return 1;
    }
    if (tag == 0xCA) {
        r->pos++;
        union { float f; uint32_t u; } conv;
        conv.u = mpack_read_u32(r);
        *out = (double)conv.f;
        return 1;
    }
    return 0;
}

/* Reads a str tag; returns pointer into the buffer (not null-terminated) and length.
 * Returns 1 on success, 0 if not a str tag. */
static inline int mpack_try_read_str(mpack_reader_t *r, const char **data_out, size_t *len_out) {
    uint8_t tag = mpack_peek_tag(r);
    size_t len;
    if ((tag & 0xE0) == 0xA0) {
        r->pos++;
        len = tag & 0x1F;
    } else if (tag == 0xD9) {
        r->pos++;
        len = mpack_read_u8(r);
    } else if (tag == 0xDA) {
        r->pos++;
        len = mpack_read_u16(r);
    } else if (tag == 0xDB) {
        r->pos++;
        len = mpack_read_u32(r);
    } else {
        return 0;
    }
    const uint8_t *p = mpack_read_raw(r, len);
    if (!p) return 0;
    *data_out = (const char *)p;
    *len_out = len;
    return 1;
}

static inline int mpack_try_read_bin(mpack_reader_t *r, const uint8_t **data_out, size_t *len_out) {
    uint8_t tag = mpack_peek_tag(r);
    size_t len;
    if (tag == 0xC4) {
        r->pos++;
        len = mpack_read_u8(r);
    } else if (tag == 0xC5) {
        r->pos++;
        len = mpack_read_u16(r);
    } else if (tag == 0xC6) {
        r->pos++;
        len = mpack_read_u32(r);
    } else {
        return 0;
    }
    const uint8_t *p = mpack_read_raw(r, len);
    if (!p) return 0;
    *data_out = p;
    *len_out = len;
    return 1;
}

static inline int mpack_try_read_array_header(mpack_reader_t *r, uint32_t *count_out) {
    uint8_t tag = mpack_peek_tag(r);
    if ((tag & 0xF0) == 0x90) {
        r->pos++;
        *count_out = tag & 0x0F;
        return 1;
    }
    if (tag == 0xDC) {
        r->pos++;
        *count_out = mpack_read_u16(r);
        return 1;
    }
    if (tag == 0xDD) {
        r->pos++;
        *count_out = mpack_read_u32(r);
        return 1;
    }
    return 0;
}

static inline int mpack_try_read_map_header(mpack_reader_t *r, uint32_t *count_out) {
    uint8_t tag = mpack_peek_tag(r);
    if ((tag & 0xF0) == 0x80) {
        r->pos++;
        *count_out = tag & 0x0F;
        return 1;
    }
    if (tag == 0xDE) {
        r->pos++;
        *count_out = mpack_read_u16(r);
        return 1;
    }
    if (tag == 0xDF) {
        r->pos++;
        *count_out = mpack_read_u32(r);
        return 1;
    }
    return 0;
}

/* Skips over one complete msgpack value (used to ignore unknown map keys). */
static inline void mpack_skip_value(mpack_reader_t *r) {
    if (r->error || r->pos >= r->len) { r->error = 1; return; }
    uint8_t tag = r->buf[r->pos];

    if (tag <= 0x7F || tag >= 0xE0) { r->pos++; return; }               /* fixint */
    if ((tag & 0xE0) == 0xA0) { const char *d; size_t l; mpack_try_read_str(r, &d, &l); return; }
    if ((tag & 0xF0) == 0x90) {                                          /* fixarray */
        uint32_t count; mpack_try_read_array_header(r, &count);
        for (uint32_t i = 0; i < count && !r->error; i++) mpack_skip_value(r);
        return;
    }
    if ((tag & 0xF0) == 0x80) {                                          /* fixmap */
        uint32_t count; mpack_try_read_map_header(r, &count);
        for (uint32_t i = 0; i < count && !r->error; i++) { mpack_skip_value(r); mpack_skip_value(r); }
        return;
    }
    switch (tag) {
        case 0xC0: case 0xC2: case 0xC3: r->pos++; return;
        case 0xCC: r->pos++; mpack_read_u8(r); return;
        case 0xCD: r->pos++; mpack_read_u16(r); return;
        case 0xCE: r->pos++; mpack_read_u32(r); return;
        case 0xCF: r->pos++; mpack_read_u64(r); return;
        case 0xD0: r->pos++; mpack_read_u8(r); return;
        case 0xD1: r->pos++; mpack_read_u16(r); return;
        case 0xD2: r->pos++; mpack_read_u32(r); return;
        case 0xD3: r->pos++; mpack_read_u64(r); return;
        case 0xCA: r->pos++; mpack_read_u32(r); return;
        case 0xCB: r->pos++; mpack_read_u64(r); return;
        case 0xC4: case 0xC5: case 0xC6: { const uint8_t *d; size_t l; mpack_try_read_bin(r, &d, &l); return; }
        case 0xD9: case 0xDA: case 0xDB: { const char *d; size_t l; mpack_try_read_str(r, &d, &l); return; }
        case 0xDC: case 0xDD: {
            uint32_t count; mpack_try_read_array_header(r, &count);
            for (uint32_t i = 0; i < count && !r->error; i++) mpack_skip_value(r);
            return;
        }
        case 0xDE: case 0xDF: {
            uint32_t count; mpack_try_read_map_header(r, &count);
            for (uint32_t i = 0; i < count && !r->error; i++) { mpack_skip_value(r); mpack_skip_value(r); }
            return;
        }
        default:
            r->error = 1;
            return;
    }
}

#endif
