use callwire::{Client, DiscoverPool, serve_registry, serve_on, register_unary};

#[tokio::test]
async fn test_rust_registry_discovery() {
    unsafe { std::env::set_var("CALLWIRE_AUTO", "0"); }

    // Start registry
    let reg_handle = serve_registry("127.0.0.1:29090").await.expect("failed to start registry");
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;

    // Export function on worker
    register_unary("say_hello", |(name,): (String,)| -> Result<String, String> {
        Ok(format!("Hello {}", name))
    });

    // Start worker
    let worker_handle = serve_on("127.0.0.1:29091").await.expect("failed to start worker");
    tokio::time::sleep(std::time::Duration::from_millis(50)).await;

    // Register worker
    let reg_client = Client::connect("127.0.0.1:29090").await.expect("failed to connect to registry");
    let _: () = reg_client.import("callwire.register", &("test-service".to_string(), "127.0.0.1:29091".to_string())).await.unwrap();

    // Discover and call
    let pool = DiscoverPool::new("127.0.0.1:29090", "test-service").await.expect("failed to create pool");
    let client = pool.get().expect("failed to get client");
    
    let res: String = client.import("say_hello", &("World".to_string(),)).await.expect("call failed");
    assert_eq!(res, "Hello World");

    reg_handle.close();
    worker_handle.close();
}
