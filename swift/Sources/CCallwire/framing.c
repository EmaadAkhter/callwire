#include "internal.h"
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <arpa/inet.h>
#include <unistd.h>

#define CALLWIRE_MAX_FRAME_SIZE (16u * 1024u * 1024u)

/* recv() is only guaranteed to return "up to" the requested length — a short
 * read on a partially-arrived TCP segment is normal, not an error. Loop
 * until the full length is read or the connection genuinely fails. */
static int recv_all(int sockfd, uint8_t *buf, size_t len) {
    size_t read_so_far = 0;
    while (read_so_far < len) {
        ssize_t n = recv(sockfd, buf + read_so_far, len - read_so_far, 0);
        if (n <= 0) {
            return -1; /* 0 = orderly disconnect, <0 = error */
        }
        read_so_far += (size_t)n;
    }
    return 0;
}

/* send() likewise may only write part of the buffer in one call. */
static int send_all(int sockfd, const uint8_t *buf, size_t len) {
    size_t sent_so_far = 0;
    while (sent_so_far < len) {
        ssize_t n = send(sockfd, buf + sent_so_far, len - sent_so_far, 0);
        if (n <= 0) {
            return -1;
        }
        sent_so_far += (size_t)n;
    }
    return 0;
}

/* Read a frame from socket: [4-byte big-endian length][payload]. */
int callwire_framing_read_frame(int sockfd, uint8_t **payload_out, size_t *len_out) {
    uint8_t header[4];
    if (recv_all(sockfd, header, 4) != 0) {
        return -1;
    }

    uint32_t len = ((uint32_t)header[0] << 24) |
                   ((uint32_t)header[1] << 16) |
                   ((uint32_t)header[2] << 8) |
                   ((uint32_t)header[3]);

    if (len > CALLWIRE_MAX_FRAME_SIZE) {
        return -1;
    }

    if (len == 0) {
        *payload_out = NULL;
        *len_out = 0;
        return 0;
    }

    uint8_t *payload = malloc(len);
    if (!payload) {
        return -1;
    }

    if (recv_all(sockfd, payload, len) != 0) {
        free(payload);
        return -1;
    }

    *payload_out = payload;
    *len_out = len;
    return 0;
}

/* Write a frame to socket: [4-byte big-endian length][payload]. */
int callwire_framing_write_frame(int sockfd, const uint8_t *payload, size_t len) {
    if (len > CALLWIRE_MAX_FRAME_SIZE) {
        return -1;
    }

    uint8_t header[4];
    header[0] = (uint8_t)((len >> 24) & 0xFF);
    header[1] = (uint8_t)((len >> 16) & 0xFF);
    header[2] = (uint8_t)((len >> 8) & 0xFF);
    header[3] = (uint8_t)(len & 0xFF);

    if (send_all(sockfd, header, 4) != 0) {
        return -1;
    }
    if (len > 0 && send_all(sockfd, payload, len) != 0) {
        return -1;
    }
    return 0;
}
