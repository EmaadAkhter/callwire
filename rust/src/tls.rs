//! TLS support for callwire.
//!
//! Mirrors Go's `TLSConfig` / `ServeWithTLS` / `ConnectWithTLS`.
//!
//! # One-way TLS (server-auth only)
//! ```ignore
//! // Server
//! let handle = callwire::tls::serve_on_tls("0.0.0.0:9443", callwire::tls::TlsConfig {
//!     cert_pem: cert_bytes,
//!     key_pem:  key_bytes,
//!     ca_pem:   None, // no mTLS
//! }).await?;
//!
//! // Client
//! let client = callwire::tls::TlsConfig {
//!     cert_pem: vec![],
//!     key_pem:  vec![],
//!     ca_pem:   Some(ca_bytes), // trust the CA that signed the server cert
//! }.connect("127.0.0.1:9443").await?;
//! ```
//!
//! # mTLS (mutual)
//! Pass a `ca_pem` on the server (requires client cert) AND provide a
//! `cert_pem` + `key_pem` on the client.

use std::io;
use std::sync::Arc;
use rustls::pki_types::{CertificateDer, PrivateKeyDer, ServerName};
use rustls::{ClientConfig, RootCertStore, ServerConfig};
use tokio::net::{TcpListener, TcpStream};
use tokio_rustls::{TlsAcceptor, TlsConnector};
use tokio::io::{AsyncWriteExt, split};

use crate::errors::{Error, Result};
use crate::server::{ServerHandle, RegistryEntry};

/// Ensure the ring CryptoProvider is ready.
/// In rustls 0.22, ring is the default provider — no explicit installation needed.
fn ensure_provider() {
    // rustls 0.22 uses ring by default; nothing to install.
}


// ──────────────────────────────────────────────────────────────────────────────
// TlsConfig
// ──────────────────────────────────────────────────────────────────────────────

/// TLS credentials, held as raw PEM bytes.
///
/// To generate self-signed certs for testing see [`gen_self_signed`].
#[derive(Clone, Default)]
pub struct TlsConfig {
    /// PEM-encoded certificate chain (required for servers; optional for mTLS clients).
    pub cert_pem: Vec<u8>,
    /// PEM-encoded private key (required for servers; optional for mTLS clients).
    pub key_pem: Vec<u8>,
    /// PEM-encoded CA certificate. When set on a **server** it enables mTLS
    /// (client certificate required). When set on a **client** it is used to
    /// verify the server certificate.
    pub ca_pem: Option<Vec<u8>>,
}

impl TlsConfig {
    // ─── server side ────────────────────────────────────────────────────────

    pub(crate) fn build_server_config(&self) -> io::Result<Arc<ServerConfig>> {
        let certs = load_certs(&self.cert_pem)?;
        let key = load_private_key(&self.key_pem)?;

        if let Some(ca) = &self.ca_pem {
            // mTLS: require a client certificate verified against this CA.
            let ca_certs = load_certs(ca)?;
            let mut roots = RootCertStore::empty();
            for cert in ca_certs {
                roots.add(cert)
                    .map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, e))?;
            }
            let verifier = rustls::server::WebPkiClientVerifier::builder(Arc::new(roots))
                .build()
                .map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, e))?;
            let cfg = ServerConfig::builder()
                .with_client_cert_verifier(verifier)
                .with_single_cert(certs, key)
                .map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, e))?;
            Ok(Arc::new(cfg))
        } else {
            let cfg = ServerConfig::builder()
                .with_no_client_auth()
                .with_single_cert(certs, key)
                .map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, e))?;
            Ok(Arc::new(cfg))
        }
    }

    // ─── client side ────────────────────────────────────────────────────────

    pub(crate) fn build_client_config(&self) -> io::Result<Arc<ClientConfig>> {
        let mut roots = RootCertStore::empty();
        if let Some(ca) = &self.ca_pem {
            for cert in load_certs(ca)? {
                roots.add(cert)
                    .map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, e))?;
            }
        }

        let builder = ClientConfig::builder().with_root_certificates(roots);

        let cfg = if !self.cert_pem.is_empty() && !self.key_pem.is_empty() {
            // mTLS: present our own certificate.
            let certs = load_certs(&self.cert_pem)?;
            let key = load_private_key(&self.key_pem)?;
            builder
                .with_client_auth_cert(certs, key)
                .map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, e))?
        } else {
            builder.with_no_client_auth()
        };

        Ok(Arc::new(cfg))
    }

    /// Dial `addr` over TLS and return a [`crate::Client`].
    pub async fn connect(self, addr: &str) -> Result<crate::Client> {
        crate::client::Client::connect_tls_impl(addr.to_string(), self, false).await
    }

    /// Dial `addr` over TLS with auto-reconnect and return a [`crate::Client`].
    pub async fn connect_with_reconnect(self, addr: &str) -> Result<crate::Client> {
        crate::client::Client::connect_tls_impl(addr.to_string(), self, true).await
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Server
// ──────────────────────────────────────────────────────────────────────────────

/// Start a TLS server on `addr`.  Returns a [`ServerHandle`] to shut it down.
pub async fn serve_on_tls(addr: &str, cfg: TlsConfig) -> Result<ServerHandle> {
    ensure_provider();
    let server_cfg = cfg.build_server_config()
        .map_err(|e| Error::Internal(e.to_string()))?;
    let acceptor = TlsAcceptor::from(server_cfg);

    let listener = TcpListener::bind(addr).await?;
    let (tx, rx) = tokio::sync::watch::channel(false);

    tokio::spawn(run_tls_accept_loop(listener, acceptor, rx));

    Ok(ServerHandle { tx })
}

async fn run_tls_accept_loop(
    listener: TcpListener,
    acceptor: TlsAcceptor,
    mut rx: tokio::sync::watch::Receiver<bool>,
) {
    loop {
        tokio::select! {
            res = listener.accept() => {
                match res {
                    Ok((tcp, _)) => {
                        let acceptor = acceptor.clone();
                        let shutdown = rx.clone();
                        tokio::spawn(async move {
                            match acceptor.accept(tcp).await {
                                Ok(tls_stream) => {
                                    handle_tls_connection(tls_stream, shutdown).await;
                                }
                                Err(e) => {
                                    eprintln!("callwire/tls: TLS handshake error: {e}");
                                }
                            }
                        });
                    }
                    Err(_) => break,
                }
            }
            _ = rx.changed() => {
                if *rx.borrow() { break; }
            }
        }
    }
}

async fn handle_tls_connection(
    stream: tokio_rustls::server::TlsStream<TcpStream>,
    mut shutdown_rx: tokio::sync::watch::Receiver<bool>,
) {
    let (mut reader, writer) = split(stream);
    let writer = Arc::new(tokio::sync::Mutex::new(writer));

    loop {
        tokio::select! {
            res = crate::framing::read_frame(&mut reader) => {
                match res {
                    Ok(payload) => {
                        match crate::codec::unpack(&payload) {
                            Ok(msg) => {
                                let writer_clone = writer.clone();
                                tokio::spawn(async move {
                                    dispatch_tls(writer_clone, msg).await;
                                });
                            }
                            Err(_) => { /* ignore malformed */ }
                        }
                    }
                    Err(_) => break,
                }
            }
            _ = shutdown_rx.changed() => {
                if *shutdown_rx.borrow() { break; }
            }
        }
    }
    // Graceful shutdown: flush writer.
    let _ = writer.lock().await.shutdown().await;
}

// ──────────────────────────────────────────────────────────────────────────────
// Dispatch (same logic as server::dispatch but for TLS WriteHalf)
// ──────────────────────────────────────────────────────────────────────────────

type TlsWriteHalf = tokio::io::WriteHalf<tokio_rustls::server::TlsStream<TcpStream>>;

async fn dispatch_tls(writer: Arc<tokio::sync::Mutex<TlsWriteHalf>>, msg: crate::codec::WireMessage) {
    let func_name = match &msg.func {
        Some(f) => f.clone(),
        None => {
            if let Ok(payload) = crate::codec::pack_error(msg.id, "TypeError", "missing func field") {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
            return;
        }
    };

    let entry = {
        let reg = crate::server::REGISTRY.lock().unwrap();
        reg.get(&func_name).cloned()
    };

    let Some(entry) = entry else {
        if let Ok(payload) = crate::codec::pack_error(
            msg.id,
            "NotFoundError",
            &format!("function '{}' not exported", func_name),
        ) {
            let mut w = writer.lock().await;
            let _ = crate::framing::write_frame(&mut *w, &payload).await;
        }
        return;
    };

    let args = msg.args.unwrap_or(rmpv::Value::Nil);

    match entry {
        RegistryEntry::Unary(handler) => {
            match handler(args).await {
                Ok(res) => {
                    if let Ok(payload) = crate::codec::pack_response(msg.id, &res) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
                Err(err) => {
                    if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
            }
        }
        RegistryEntry::Stream(handler) => {
            match handler(args).await {
                Ok(mut stream) => {
                    use futures_util::StreamExt;
                    while let Some(res) = stream.next().await {
                        match res {
                            Ok(val) => {
                                if let Ok(payload) = crate::codec::pack_stream_chunk(msg.id, &val) {
                                    let mut w = writer.lock().await;
                                    if crate::framing::write_frame(&mut *w, &payload).await.is_err() {
                                        return;
                                    }
                                }
                            }
                            Err(err) => {
                                if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                                    let mut w = writer.lock().await;
                                    let _ = crate::framing::write_frame(&mut *w, &payload).await;
                                }
                                return;
                            }
                        }
                    }
                    if let Ok(payload) = crate::codec::pack_stream_end(msg.id) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
                Err(err) => {
                    if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
            }
        }
        RegistryEntry::ClientStream(_) | RegistryEntry::Bidi(_) => {
            // TLS server dispatch does not yet route streaming-input frames
            // (stream_chunk/stream_close/stream_end) to an in-progress call —
            // that routing only exists in the plaintext handle_connection in server.rs.
            // Client-streaming/bidi over TLS is not yet supported.
            if let Ok(payload) = crate::codec::pack_error(
                msg.id,
                "UnsupportedError",
                "client-streaming and bidi-streaming are not yet supported over TLS",
            ) {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────────
// Client-side TLS dial helpers (called by client::Client::connect_tls_impl)
// ──────────────────────────────────────────────────────────────────────────────

/// Dial `addr` over TLS and return the raw TLS stream.
pub(crate) async fn dial_tls(addr: &str, cfg: &TlsConfig) -> io::Result<tokio_rustls::client::TlsStream<TcpStream>> {
    ensure_provider();
    let client_cfg = cfg.build_client_config()?;
    let connector = TlsConnector::from(client_cfg);

    // Extract the hostname for SNI (use IP literal as fallback).
    let host = addr.split(':').next().unwrap_or("localhost");
    let server_name: ServerName<'static> = host.to_string().try_into()
        .map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, format!("invalid server name: {e}")))?;

    let tcp = TcpStream::connect(addr).await?;
    let tls = connector.connect(server_name, tcp).await?;
    Ok(tls)
}

// ──────────────────────────────────────────────────────────────────────────────
// PEM helpers
// ──────────────────────────────────────────────────────────────────────────────

fn load_certs(pem: &[u8]) -> io::Result<Vec<CertificateDer<'static>>> {
    rustls_pemfile::certs(&mut io::BufReader::new(pem))
        .collect::<io::Result<Vec<_>>>()
}

fn load_private_key(pem: &[u8]) -> io::Result<PrivateKeyDer<'static>> {
    rustls_pemfile::private_key(&mut io::BufReader::new(pem))?
        .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidInput, "no private key found in PEM"))
}

// ──────────────────────────────────────────────────────────────────────────────
// Test helper: generate a self-signed cert + CA
// ──────────────────────────────────────────────────────────────────────────────

/// Generate a self-signed certificate for testing.
///
/// Returns `(cert_pem, key_pem, ca_pem)`.  The CA is the self-signed cert
/// itself — pass it as `ca_pem` on the client to trust the server.
#[cfg(test)]
pub fn gen_self_signed(san: &str) -> (Vec<u8>, Vec<u8>, Vec<u8>) {
    use rcgen::{generate_simple_self_signed, CertifiedKey};
    let CertifiedKey { cert, signing_key } =
        generate_simple_self_signed(vec![san.to_owned()]).unwrap();
    let cert_pem = cert.pem().into_bytes();
    let key_pem = signing_key.serialize_pem().into_bytes();
    let ca_pem = cert_pem.clone();
    (cert_pem, key_pem, ca_pem)
}
