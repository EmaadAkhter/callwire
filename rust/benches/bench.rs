use criterion::{criterion_group, criterion_main, Criterion, BatchSize};
use callwire::{Client, register_unary, serve_on};
use std::net::TcpListener;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;
use tokio::runtime::Runtime;

fn pick_port() -> u16 {
    TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port()
}

fn rt() -> Runtime {
    tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .unwrap()
}

fn run_server(addr: String, setup: impl FnOnce() + Send + 'static) -> Arc<AtomicBool> {
    let done = Arc::new(AtomicBool::new(false));
    let done_clone = done.clone();
    std::thread::spawn(move || {
        let rt = rt();
        rt.block_on(async {
            setup();
            let handle = serve_on(&addr).await.unwrap();
            while !done_clone.load(Ordering::Relaxed) {
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
            handle.close();
        });
    });
    std::thread::sleep(Duration::from_millis(300));
    done
}

fn bench_latency_noop(c: &mut Criterion) {
    let port = pick_port();
    let addr = format!("127.0.0.1:{}", port);
    let addr2 = addr.clone();

    let done = run_server(addr2, || {
        register_unary("noop", |(): ()| Ok::<(), String>(()));
    });

    let rt = rt();
    c.bench_function("latency/noop", |b| {
        b.iter_batched(
            || rt.block_on(Client::connect(&addr)).unwrap(),
            |client| {
                rt.block_on(async {
                    let _: () = client.import("noop", &()).await.unwrap();
                });
            },
            BatchSize::SmallInput,
        );
    });

    done.store(true, Ordering::Relaxed);
    std::thread::sleep(Duration::from_millis(100));
}

fn bench_latency_add(c: &mut Criterion) {
    let port = pick_port();
    let addr = format!("127.0.0.1:{}", port);
    let addr2 = addr.clone();

    let done = run_server(addr2, || {
        register_unary("add", |(a, b): (i64, i64)| Ok::<i64, String>(a + b));
    });

    let rt = rt();
    c.bench_function("latency/add", |b| {
        b.iter_batched(
            || rt.block_on(Client::connect(&addr)).unwrap(),
            |client| {
                rt.block_on(async {
                    let _: i64 = client.import("add", &(10i64, 20i64)).await.unwrap();
                });
            },
            BatchSize::SmallInput,
        );
    });

    done.store(true, Ordering::Relaxed);
    std::thread::sleep(Duration::from_millis(100));
}

fn bench_latency_echo_string(c: &mut Criterion) {
    let port = pick_port();
    let addr = format!("127.0.0.1:{}", port);
    let addr2 = addr.clone();
    let payload = "x".repeat(1024);

    let done = run_server(addr2, || {
        register_unary("echo", |(s,): (String,)| Ok::<String, String>(s));
    });

    let rt = rt();
    c.bench_function("latency/echo_string_1kb", |b| {
        b.iter_batched(
            || rt.block_on(Client::connect(&addr)).unwrap(),
            |client| {
                let s = payload.clone();
                rt.block_on(async {
                    let _: String = client.import("echo", &(s,)).await.unwrap();
                });
            },
            BatchSize::SmallInput,
        );
    });

    done.store(true, Ordering::Relaxed);
    std::thread::sleep(Duration::from_millis(100));
}

fn bench_throughput_concurrent(c: &mut Criterion) {
    let port = pick_port();
    let addr = format!("127.0.0.1:{}", port);
    let addr2 = addr.clone();

    let done = run_server(addr2, || {
        register_unary("noop", |(): ()| Ok::<(), String>(()));
    });

    let rt = rt();
    for &workers in &[1, 5, 10, 50] {
        c.bench_function(&format!("throughput/{}_workers", workers), |b| {
            b.iter_batched(
                || rt.block_on(Client::connect(&addr)).unwrap(),
                |client| {
                    rt.block_on(async {
                        let mut handles = Vec::new();
                        for _ in 0..workers {
                            handles.push(client.import::<(), _>("noop", &()));
                        }
                        futures_util::future::join_all(handles).await;
                    });
                },
                BatchSize::SmallInput,
            );
        });
    }

    done.store(true, Ordering::Relaxed);
    std::thread::sleep(Duration::from_millis(100));
}

criterion_group! {
    name = benches;
    config = Criterion::default()
        .measurement_time(Duration::from_secs(5))
        .warm_up_time(Duration::from_secs(2));
    targets = bench_latency_noop, bench_latency_add, bench_latency_echo_string, bench_throughput_concurrent
}
criterion_main!(benches);
