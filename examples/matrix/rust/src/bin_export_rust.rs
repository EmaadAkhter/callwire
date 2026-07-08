// Rust export script: exports "add" on a fixed port. init() performs the
// setup and is called first thing in main().
//
// Calls callwire::serve() explicitly rather than relying on
// register_unary()'s auto-serve — auto-serve is gated on CALLWIRE_SPAWNED
// (unset = auto-start, "1" = don't), and the orchestrator (callwire.toml +
// init()) sets CALLWIRE_SPAWNED=1 on every spawned worker for its own
// registry-worker bookkeeping. Under the orchestrator, auto-serve would
// silently no-op and this server would never bind. CALLWIRE_AUTO=0 forces
// auto-serve off unconditionally (standalone or orchestrated) so the
// explicit serve() call below is always the one true listener.
use callwire::register_unary;
use std::env;

const MATRIX_PORT: u16 = 9103;

fn init() {
    env::set_var("CALLWIRE_AUTO", "0");
    register_unary("add", |(a, b): (i64, i64)| Ok::<i64, String>(a + b));
}

#[tokio::main]
async fn main() {
    init();
    println!("Rust matrix export listening on :{}", MATRIX_PORT);
    callwire::serve(("0.0.0.0", MATRIX_PORT)).await.unwrap();
}
