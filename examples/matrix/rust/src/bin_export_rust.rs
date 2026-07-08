// Rust export script: exports "add" on a fixed port. init() performs the
// setup and is called first thing in main() — register_unary auto-starts
// the background server (same auto-serve mechanism Python/Go use).
use callwire::register_unary;
use std::env;

const MATRIX_PORT: u16 = 9103;

fn init() {
    env::set_var("CALLWIRE_PORT", MATRIX_PORT.to_string());
    register_unary("add", |(a, b): (i64, i64)| Ok::<i64, String>(a + b));
}

fn main() {
    init();
    println!("Rust matrix export listening on :{}", MATRIX_PORT);
    loop {
        std::thread::sleep(std::time::Duration::from_secs(3600));
    }
}
