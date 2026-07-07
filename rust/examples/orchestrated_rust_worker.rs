// This file is a Callwire Rust WORKER.
//
// When spawned by callwire::init() (Python/Go/Rust/TS orchestrator) it detects
// CALLWIRE_SPAWNED=1, binds to a random port, and auto-registers these
// functions with the parent's dynamic registry.

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Register the local functions
    callwire::register_unary("predict", |(val,): (String,)| -> Result<String, String> {
        Ok(format!("Prediction for '{}': [0.99, 0.01] (from Rust worker)", val))
    });

    callwire::register_unary("embed", |(val,): (String,)| -> Result<Vec<f32>, String> {
        // Return dummy embeddings
        Ok(vec![0.1, 0.2, 0.3, 0.4, val.len() as f32])
    });

    println!("[rust-worker] Functions registered: predict, embed");

    // callwire::init() will:
    // 1. Detect CALLWIRE_SPAWNED=1 and run as worker.
    // 2. Start worker TCP server on a random port.
    // 3. Register "predict" and "embed" to parent registry.
    // 4. Start parent orphan watcher.
    let guard = callwire::init().await?;

    if guard.is_some() {
        println!("[rust-worker] Orchestrator guard started. Waiting...");
    } else {
        println!("[rust-worker] Worker mode active. Ready to process calls!");
    }

    // Keep the worker process alive to receive and handle incoming calls.
    // We block forever using a pending future.
    std::future::pending::<()>().await;

    Ok(())
}
