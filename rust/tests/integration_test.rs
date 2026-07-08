use callwire::{Client, ServerHandle};
use futures_util::StreamExt;
use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Debug, PartialEq, Clone)]
struct InferRequest {
    name: String,
    scale: f64,
}

#[derive(Serialize, Deserialize, Debug, PartialEq)]
struct InferResponse {
    name: String,
    score: f64,
    count: i64,
}

/// Helper: register all functions and start a server on `addr`.
/// Returns a `ServerHandle` for shutdown.
async fn setup_server(addr: &str) -> ServerHandle {
    // Disable global auto-serve — tests manage their own server lifecycle.
    unsafe { std::env::set_var("CALLWIRE_AUTO", "0"); }

    callwire::register_unary("add", |(a, b): (i64, i64)| -> Result<i64, String> {
        Ok(a + b)
    });

    callwire::register_unary("crash", |(): ()| -> Result<i64, String> {
        Err("boom".to_string())
    });

    callwire::register_unary("infer", |(req, tensor): (InferRequest, Vec<f64>)| -> Result<InferResponse, String> {
        let sum: f64 = tensor.iter().sum();
        Ok(InferResponse {
            name: req.name,
            score: sum * req.scale,
            count: tensor.len() as i64,
        })
    });

    callwire::register_stream("count_up", |(n,): (i64,)| -> Result<_, String> {
        let s = futures_util::stream::iter((1..=n).map(|i| Ok::<i64, std::convert::Infallible>(i)));
        Ok(s)
    });

    callwire::serve_on(addr).await.expect("failed to bind server")
}


#[tokio::test]
async fn test_unary_add() {
    let handle = setup_server("127.0.0.1:19600").await;
    let client = Client::connect("127.0.0.1:19600").await.unwrap();

    let res: i64 = client.import("add", &(10i64, 20i64)).await.unwrap();
    assert_eq!(res, 30);

    handle.close();
}

#[tokio::test]
async fn test_unary_error() {
    let handle = setup_server("127.0.0.1:19601").await;
    let client = Client::connect("127.0.0.1:19601").await.unwrap();

    let err = client.import::<i64, _>("crash", &()).await.unwrap_err();
    assert!(err.to_string().contains("boom"), "error was: {}", err);

    handle.close();
}

#[tokio::test]
async fn test_not_found() {
    let handle = setup_server("127.0.0.1:19602").await;
    let client = Client::connect("127.0.0.1:19602").await.unwrap();

    let err = client.import::<i64, _>("does_not_exist", &()).await.unwrap_err();
    assert!(err.to_string().contains("not exported"), "error was: {}", err);

    handle.close();
}

#[tokio::test]
async fn test_composite_args() {
    let handle = setup_server("127.0.0.1:19603").await;
    let client = Client::connect("127.0.0.1:19603").await.unwrap();

    let req = InferRequest { name: "demo".to_string(), scale: 0.5 };
    let tensor = vec![1.0f64, 2.0, 3.0, 4.0];
    let resp: InferResponse = client.import("infer", &(req, tensor)).await.unwrap();
    assert_eq!(resp, InferResponse { name: "demo".to_string(), score: 5.0, count: 4 });

    handle.close();
}

#[tokio::test]
async fn test_streaming() {
    let handle = setup_server("127.0.0.1:19604").await;
    let client = Client::connect("127.0.0.1:19604").await.unwrap();

    let mut stream = client.import_stream::<i64, _>("count_up", &(5i64,)).await.unwrap();
    let mut got = vec![];
    while let Some(v) = stream.next().await {
        got.push(v.unwrap());
    }
    assert_eq!(got, vec![1, 2, 3, 4, 5]);

    handle.close();
}

#[tokio::test]
async fn test_reconnect() {
    // Start server on port 19605
    let handle1 = setup_server("127.0.0.1:19605").await;

    // Connect a client with reconnect enabled
    let client = Client::connect_with_reconnect("127.0.0.1:19605").await.unwrap();

    // Verify initial connection
    let res: i64 = client.import("add", &(1i64, 2i64)).await.unwrap();
    assert_eq!(res, 3);

    // Drop server 1
    handle1.close();
    tokio::time::sleep(tokio::time::Duration::from_millis(50)).await;

    // Restart server on same port
    let handle2 = setup_server("127.0.0.1:19605").await;

    // Client should reconnect and succeed
    let res2: i64 = client.import("add", &(10i64, 20i64)).await.unwrap();
    assert_eq!(res2, 30);

    handle2.close();
}

#[tokio::test]
async fn test_concurrent_calls() {
    let handle = setup_server("127.0.0.1:19606").await;
    let client = std::sync::Arc::new(Client::connect("127.0.0.1:19606").await.unwrap());

    // Fire 10 concurrent calls
    let mut tasks = vec![];
    for i in 0i64..10 {
        let c = client.clone();
        tasks.push(tokio::spawn(async move {
            c.import::<i64, _>("add", &(i, i)).await
        }));
    }

    for (i, task) in tasks.into_iter().enumerate() {
        let res = task.await.unwrap().unwrap();
        assert_eq!(res, (i as i64) * 2);
    }

    handle.close();
}

#[tokio::test]
async fn test_close_during_call() {
    // Register a handler that blocks so we can close before it returns
    callwire::register_unary("slow_add", |(a, b): (i64, i64)| -> Result<i64, String> {
        std::thread::sleep(std::time::Duration::from_secs(5));
        Ok(a + b)
    });

    let handle = setup_server("127.0.0.1:19608").await;
    let client = Client::connect("127.0.0.1:19608").await.unwrap();

    let client2 = client.clone();

    // Fire a slow call in a background task
    let join = tokio::spawn(async move {
        client2.import::<i64, _>("slow_add", &(1i64, 2i64)).await
    });

    // Give the call time to reach the pending map
    tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;

    // Close the client while the call is in-flight
    client.close();

    let result = join.await.unwrap();
    assert!(result.is_err(), "expected error after close, got: {:?}", result);

    handle.close();
}

#[tokio::test]
async fn test_close_during_stream() {
    let handle = setup_server("127.0.0.1:19609").await;
    let client = Client::connect("127.0.0.1:19609").await.unwrap();

    let mut stream = client.import_stream::<i64, _>("count_up", &(100i64,)).await.unwrap();

    // Read a few chunks
    let first = stream.next().await;
    assert!(first.is_some());
    assert_eq!(first.unwrap().unwrap(), 1);
    let second = stream.next().await;
    assert!(second.is_some());
    assert_eq!(second.unwrap().unwrap(), 2);

    // Close the client mid-stream
    client.close();

    // After close, the stream should terminate cleanly
    let mut count = 2;
    while let Some(chunk) = stream.next().await {
        match chunk {
            Ok(_) => count += 1,
            Err(_) => break,
        }
    }
    assert!(count < 100, "stream should have terminated before reaching 100 (got {count} chunks)");

    handle.close();
}

#[tokio::test]
async fn test_batch() {
    let handle = setup_server("127.0.0.1:19607").await;
    let client = Client::connect("127.0.0.1:19607").await.unwrap();

    // 1. Success batch
    let calls = vec![
        ("add", rmpv::Value::Array(vec![10.into(), 20.into()])),
        ("add", rmpv::Value::Array(vec![1.into(), 2.into()])),
    ];
    let results = client.batch(calls).await.unwrap();
    assert_eq!(results.len(), 2);
    assert_eq!(results[0], rmpv::Value::Integer(30.into()));
    assert_eq!(results[1], rmpv::Value::Integer(3.into()));

    // 2. Failure batch
    let calls_fail = vec![
        ("add", rmpv::Value::Array(vec![1.into(), 2.into()])),
        ("crash", rmpv::Value::Nil),
    ];
    let err = client.batch(calls_fail).await.unwrap_err();
    assert!(err.to_string().contains("boom"), "expected boom error, got: {}", err);

    handle.close();
}

