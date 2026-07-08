#include "callwire.h"
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <unistd.h>
#include <pthread.h>

struct callwire_server {
    int listen_sock;
    int running;
    pthread_t accept_thread;
    /* TODO: registry of exported functions */
    /* TODO: thread pool for handling connections */
};

callwire_server_t *callwire_server_new(const char *addr, int port) {
    callwire_server_t *s = malloc(sizeof(callwire_server_t));
    if (!s) return NULL;

    int sock = socket(AF_INET, SOCK_STREAM, 0);
    if (sock < 0) {
        free(s);
        return NULL;
    }

    int opt = 1;
    if (setsockopt(sock, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt)) < 0) {
        close(sock);
        free(s);
        return NULL;
    }

    struct sockaddr_in sa;
    memset(&sa, 0, sizeof(sa));
    sa.sin_family = AF_INET;
    sa.sin_port = htons(port);
    sa.sin_addr.s_addr = htonl(INADDR_ANY);

    if (bind(sock, (struct sockaddr *)&sa, sizeof(sa)) < 0) {
        close(sock);
        free(s);
        return NULL;
    }

    if (listen(sock, 128) < 0) {
        close(sock);
        free(s);
        return NULL;
    }

    s->listen_sock = sock;
    s->running = 1;
    return s;
}

int callwire_server_export(callwire_server_t *server, const char *func,
                           int (*fn_ptr)(callwire_value_t *, size_t, callwire_value_t *)) {
    /* TODO: store fn_ptr in registry under func name */
    return 0;
}

int callwire_server_serve(callwire_server_t *server) {
    /* TODO: accept loop, handle connections, dispatch to handlers */
    return -1; /* stub */
}

void callwire_server_close(callwire_server_t *server) {
    if (!server) return;
    server->running = 0;
    if (server->listen_sock >= 0) {
        close(server->listen_sock);
    }
    /* TODO: wait for accept thread, clean up registry */
    free(server);
}
