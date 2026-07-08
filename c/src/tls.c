#include "callwire.h"

#ifdef CALLWIRE_WITH_TLS

#include <openssl/ssl.h>
#include <openssl/err.h>

/* TLS/mTLS support via OpenSSL. */

typedef struct {
    SSL_CTX *ctx;
    int verify_peer;
} callwire_tls_config_t;

callwire_tls_config_t *callwire_tls_config_new(void) {
    /* TODO: create and configure SSL_CTX for TLS connections */
    return NULL; /* stub */
}

void callwire_tls_config_set_ca(callwire_tls_config_t *config, const char *ca_pem) {
    /* TODO: load CA certificate */
}

void callwire_tls_config_set_cert(callwire_tls_config_t *config, const char *cert_pem, const char *key_pem) {
    /* TODO: load client certificate and key for mTLS */
}

#endif /* CALLWIRE_WITH_TLS */
