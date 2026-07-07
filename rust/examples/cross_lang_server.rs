use serde::{Deserialize, Serialize};
use std::env;
use futures_util::stream::iter;

#[derive(Serialize, Deserialize, Debug, Clone)]
struct InferRequest {
    name: String,
    scale: f64,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
struct InferResponse {
    name: String,
    score: f64,
    count: i64,
}

#[tokio::main]
async fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: cross_lang_server <port>");
        std::process::exit(1);
    }
    let port = &args[1];
    let addr = format!("127.0.0.1:{}", port);

    callwire::export!("add", |(a, b): (i64, i64)| -> Result<i64, String> {
        Ok(a + b)
    });

    callwire::export!("crash", |(): ()| -> Result<i64, String> {
        Err("boom".to_string())
    });

    callwire::export!("infer_score", |(req, tensor): (InferRequest, Vec<f64>)| -> Result<InferResponse, String> {
        let sum: f64 = tensor.iter().sum();
        Ok(InferResponse {
            name: req.name,
            score: sum * req.scale,
            count: tensor.len() as i64,
        })
    });

    callwire::export_stream!("count_up", |(n,): (i64,)| -> Result<_, String> {
        let s = iter((1..=n).map(|i| Ok::<i64, std::convert::Infallible>(i)));
        Ok(s)
    });

    println!("Rust server starting on {}", addr);
    let handle = callwire::serve_on(&addr).await.expect("Failed to start server");
    
    // Sleep forever, let process signal kill us
    tokio::signal::ctrl_c().await.ok();
    handle.close();
}
