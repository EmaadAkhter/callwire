// TLS integration tests for callwire Rust.
// Tests: one-way TLS, mTLS, TLS reconnect.

/// Generate a self-signed TLS cert for testing.
/// Returns (cert_pem, key_pem, ca_pem) — ca_pem == cert_pem (self-signed).
fn gen_self_signed(san: &str) -> (Vec<u8>, Vec<u8>, Vec<u8>) {
    use rcgen::{generate_simple_self_signed, CertifiedKey};
    let CertifiedKey { cert, key_pair } =
        generate_simple_self_signed(vec![san.to_owned()]).unwrap();
    let cert_pem = cert.pem().into_bytes();
    let key_pem = key_pair.serialize_pem().into_bytes();
    let ca_pem = cert_pem.clone();
    (cert_pem, key_pem, ca_pem)
}

/// Helper: one-way TLS configs (server-auth only).
fn make_tls_oneway() -> (callwire::TlsConfig, callwire::TlsConfig) {
    let (cert_pem, key_pem, ca_pem) = gen_self_signed("127.0.0.1");
    let server_cfg = callwire::TlsConfig { cert_pem, key_pem, ca_pem: None };
    let client_cfg = callwire::TlsConfig { cert_pem: vec![], key_pem: vec![], ca_pem: Some(ca_pem) };
    (server_cfg, client_cfg)
}

/// Helper: mTLS configs (both sides present the same self-signed cert).
fn make_tls_mtls() -> (callwire::TlsConfig, callwire::TlsConfig) {
    let (cert_pem, key_pem, ca_pem) = gen_self_signed("127.0.0.1");
    let server_cfg = callwire::TlsConfig {
        cert_pem: cert_pem.clone(),
        key_pem: key_pem.clone(),
        ca_pem: Some(ca_pem.clone()),
    };
    let client_cfg = callwire::TlsConfig {
        cert_pem: cert_pem.clone(),
        key_pem: key_pem.clone(),
        ca_pem: Some(ca_pem.clone()),
    };
    (server_cfg, client_cfg)
}

#[tokio::test]
async fn test_tls_oneway() {
    unsafe { std::env::set_var("CALLWIRE_AUTO", "0"); }
    callwire::register_unary("add_tls_ow", |(a, b): (i64, i64)| -> Result<i64, String> { Ok(a + b) });

    let (server_cfg, client_cfg) = make_tls_oneway();
    let handle = callwire::serve_on_tls("127.0.0.1:19700", server_cfg).await
        .expect("serve_on_tls failed");
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;

    let client = client_cfg.connect("127.0.0.1:19700").await.expect("TLS connect failed");
    let result: i64 = client.import("add_tls_ow", &(10i64, 32i64)).await.expect("call failed");
    assert_eq!(result, 42);
    handle.close();
}

#[tokio::test]
async fn test_tls_mtls() {
    unsafe { std::env::set_var("CALLWIRE_AUTO", "0"); }
    callwire::register_unary("add_tls_mtls", |(a, b): (i64, i64)| -> Result<i64, String> { Ok(a + b) });

    let (server_cfg, client_cfg) = make_tls_mtls();
    let handle = callwire::serve_on_tls("127.0.0.1:19701", server_cfg).await
        .expect("serve_on_tls mTLS failed");
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;

    let client = client_cfg.connect("127.0.0.1:19701").await.expect("mTLS connect failed");
    let result: i64 = client.import("add_tls_mtls", &(20i64, 22i64)).await.expect("call failed");
    assert_eq!(result, 42);
    handle.close();
}

#[tokio::test]
async fn test_tls_reconnect() {
    unsafe { std::env::set_var("CALLWIRE_AUTO", "0"); }
    callwire::register_unary("add_tls_rc", |(a, b): (i64, i64)| -> Result<i64, String> { Ok(a + b) });

    // Generate ONE cert/key pair shared by both server instances.
    let (cert_pem, key_pem, ca_pem) = gen_self_signed("127.0.0.1");
    let make_server_cfg = || callwire::TlsConfig {
        cert_pem: cert_pem.clone(),
        key_pem: key_pem.clone(),
        ca_pem: None,
    };
    let client_cfg = callwire::TlsConfig {
        cert_pem: vec![],
        key_pem: vec![],
        ca_pem: Some(ca_pem),
    };

    let handle1 = callwire::serve_on_tls("127.0.0.1:19702", make_server_cfg()).await
        .expect("first TLS server failed");
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;

    let client = client_cfg.clone().connect_with_reconnect("127.0.0.1:19702").await
        .expect("TLS reconnect connect failed");
    let r1: i64 = client.import("add_tls_rc", &(1i64, 1i64)).await.unwrap();
    assert_eq!(r1, 2);

    // Bounce the server — same cert, so client can reconnect.
    handle1.close();
    tokio::time::sleep(std::time::Duration::from_millis(200)).await;

    let handle2 = callwire::serve_on_tls("127.0.0.1:19702", make_server_cfg()).await
        .expect("second TLS server failed");
    // Give the client's reconnect loop time to re-establish the TLS connection.
    tokio::time::sleep(std::time::Duration::from_millis(800)).await;

    let r2: i64 = client.import("add_tls_rc", &(2i64, 2i64)).await.unwrap();
    assert_eq!(r2, 4);
    handle2.close();
}
