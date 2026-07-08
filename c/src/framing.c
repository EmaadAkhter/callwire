#include "callwire.h"
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <arpa/inet.h>
#include <unistd.h>

/* Read a frame from socket: [4-byte big-endian length][payload]. */
int callwire_framing_read(int sock, uint8_t **payload_out, size_t *len_out) {
    uint8_t header[4];
    ssize_t n = recv(sock, header, 4, 0);
    if (n != 4) {
        return -1; /* error or disconnect */
    }

    /* Parse big-endian uint32. */
    uint32_t len = ((uint32_t)header[0] << 24) |
                   ((uint32_t)header[1] << 16) |
                   ((uint32_t)header[2] << 8) |
                   ((uint32_t)header[3]);

    if (len > 16 * 1024 * 1024) {
        return -1; /* payload too large (16MB limit) */
    }

    uint8_t *payload = malloc(len);
    if (!payload) {
        return -1;
    }

    size_t read_so_far = 0;
    while (read_so_far < len) {
        ssize_t n = recv(sock, payload + read_so_far, len - read_so_far, 0);
        if (n <= 0) {
            free(payload);
            return -1;
        }
        read_so_far += n;
    }

    *payload_out = payload;
    *len_out = len;
    return 0;
}

/* Write a frame to socket. */
int callwire_framing_write(int sock, const uint8_t *payload, size_t len) {
    uint8_t header[4];
    header[0] = (len >> 24) & 0xFF;
    header[1] = (len >> 16) & 0xFF;
    header[2] = (len >> 8) & 0xFF;
    header[3] = len & 0xFF;

    if (send(sock, header, 4, 0) != 4) {
        return -1;
    }
    if (send(sock, payload, len, 0) != (ssize_t)len) {
        return -1;
    }
    return 0;
}
