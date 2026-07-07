use serde::{Deserialize, Serialize};
use std::env;
use futures_util::StreamExt;
use callwire::{Client, Error};

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
    let addr = if args.len() < 2 {
        "localhost:9090"
    } else {
        &args[1]
    };

    let client = Client::connect(addr).await.expect("Failed to connect");

    // Try "add"
    match client.import::<i64, _>("add", &(10i64, 20i64)).await {
        Ok(res) => println!("ADD:{}", res),
        Err(Error::Remote(e)) if e.error_type == "NotFoundError" => {
            println!("NOTFOUND:not exported");
        }
        Err(e) => println!("ADD_ERROR:{}", e),
    }

    // Try a non-existent method to test NotFoundError
    match client.import::<i64, _>("does_not_exist", &()).await {
        Ok(res) => println!("NOTFOUND_SUCCESS:{}", res),
        Err(Error::Remote(e)) if e.error_type == "NotFoundError" => {
            println!("NOTFOUND:not exported");
        }
        Err(e) => println!("NOTFOUND_ERROR:{}", e),
    }



    // Try "double" (for Go double test)
    match client.import::<i64, _>("double", &(9i64,)).await {
        Ok(res) => println!("DOUBLE:{}", res),
        Err(Error::Remote(e)) if e.error_type == "NotFoundError" => {}
        Err(e) => println!("DOUBLE_ERROR:{}", e),
    }

    // Try "crash"
    match client.import::<i64, _>("crash", &()).await {
        Ok(res) => println!("CRASH_SUCCESS:{}", res),
        Err(Error::Remote(e)) => println!("ERROR:{}", e.message),
        Err(e) => println!("CRASH_ERROR:{}", e),
    }

    // Try "infer_score"
    let req = InferRequest {
        name: "demo".to_string(),
        scale: 0.5,
    };
    let tensor = vec![1.0, 2.0, 3.0, 4.0];
    match client.import::<InferResponse, _>("infer_score", &(req, tensor)).await {
        Ok(res) => println!("INFER_SCORE:{}", res.score),
        Err(Error::Remote(e)) if e.error_type == "NotFoundError" => {}
        Err(e) => println!("INFER_SCORE_ERROR:{}", e),
    }

    // Try "count_up" (stream)
    match client.import_stream::<i64, _>("count_up", &(5i64,)).await {
        Ok(mut stream) => {
            let mut vals = Vec::new();
            while let Some(val) = stream.next().await {
                match val {
                    Ok(v) => vals.push(v.to_string()),
                    Err(e) => {
                        println!("STREAM_ERROR:{}", e);
                        return;
                    }
                }
            }
            println!("STREAM:{}", vals.join(","));
        }
        Err(Error::Remote(e)) if e.error_type == "NotFoundError" => {}
        Err(e) => println!("STREAM_START_ERROR:{}", e),
    }
}
