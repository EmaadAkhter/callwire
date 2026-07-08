// Rust import script: calls "add"(10,20) on every OTHER language's matrix
// export server (best-effort — SKIP if a port isn't reachable).
use callwire::Client;

#[tokio::main]
async fn main() {
    let targets: Vec<(&str, u16)> = vec![
        ("go", 9101),
        ("python", 9102),
        ("ts", 9104),
        ("java", 9105),
        ("c", 9106),
        ("cpp", 9107),
        ("swift", 9108),
        ("cobol", 9109),
    ];

    for (name, port) in targets {
        let addr = format!("127.0.0.1:{}", port);
        match Client::connect(&addr).await {
            Ok(client) => match client.import::<i64, _>("add", &(10i64, 20i64)).await {
                Ok(result) => println!("{:8} OK  add(10,20) = {}", name, result),
                Err(e) => println!("{:8} SKIP (call failed: {})", name, e),
            },
            Err(e) => println!("{:8} SKIP (not running: {})", name, e),
        }
    }
}
