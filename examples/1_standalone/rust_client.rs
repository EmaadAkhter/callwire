// Standalone Rust Client Example
// ================================
// Connects directly to a running Go server at localhost:9090.
//
// Prerequisites:
//   1. Start the Go server:
//        cd examples/1_standalone
//        go run go_server.go
//
//   2. Paste this code into your project's src/main.rs and add to Cargo.toml:
//        [dependencies]
//        callwire = "2.0"
//        tokio = { version = "1", features = ["full"] }
//
//   3. Run:
//        cargo run
//
// Also works with the orchestrator — if CALLWIRE_REGISTRY is set,
// the client automatically routes through the registry instead.

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = if let Ok(reg_addr) = std::env::var("CALLWIRE_REGISTRY") {
        println!("Connecting to registry at {} (auto-routing enabled)...", reg_addr);
        callwire::Client::connect_registry(&reg_addr).await?
    } else {
        let addr = "localhost:9090";
        println!("Connecting directly to Go server at {}...", addr);
        callwire::Client::connect(addr).await.map_err(|_| {
            eprintln!("[error] Could not connect — is the Go server running? (go run go_server.go)");
            std::process::exit(1);
        })?
    };

    println!();

    // Call 'add'
    let sum: i32 = client.import("add", &(15, 27)).await?;
    println!("  add(15, 27)        = {}", sum);

    // Call 'greet'
    let greeting: String = client.import("greet", &("Rust Developer",)).await?;
    println!("  greet('Developer') = {:?}", greeting);

    Ok(())
}
