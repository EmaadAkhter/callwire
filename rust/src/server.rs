use std::collections::HashMap;
use std::sync::{Arc, LazyLock, Mutex};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::mpsc;
use rmpv::Value;
use crate::codec::WireMessage;
use crate::errors::{CallwireError, Result};

pub type BoxFuture<'a, T> = std::pin::Pin<Box<dyn std::future::Future<Output = T> + Send + 'a>>;
pub type BoxStream<'a, T> = std::pin::Pin<Box<dyn futures_util::Stream<Item = T> + Send + 'a>>;

pub enum RegistryEntry {
    Unary(Arc<dyn Fn(Value) -> BoxFuture<'static, std::result::Result<Value, CallwireError>> + Send + Sync>),
    Stream(Arc<dyn Fn(Value) -> BoxFuture<'static, std::result::Result<BoxStream<'static, std::result::Result<Value, CallwireError>>, CallwireError>> + Send + Sync>),
    ClientStream(Arc<dyn Fn(BoxStream<'static, Value>) -> BoxFuture<'static, std::result::Result<Value, CallwireError>> + Send + Sync>),
    Bidi(Arc<dyn Fn(BoxStream<'static, Value>) -> BoxFuture<'static, std::result::Result<BoxStream<'static, std::result::Result<Value, CallwireError>>, CallwireError>> + Send + Sync>),
}

impl Clone for RegistryEntry {
    fn clone(&self) -> Self {
        match self {
            RegistryEntry::Unary(f) => RegistryEntry::Unary(f.clone()),
            RegistryEntry::Stream(f) => RegistryEntry::Stream(f.clone()),
            RegistryEntry::ClientStream(f) => RegistryEntry::ClientStream(f.clone()),
            RegistryEntry::Bidi(f) => RegistryEntry::Bidi(f.clone()),
        }
    }
}

/// Signal sent through the per-call input channel for client-streaming/bidi calls.
enum StreamInputMsg {
    Chunk(Value),
    Close,
}

/// Minimal adapter turning an `mpsc::Receiver<Value>` into a `futures_util::Stream`.
struct ReceiverValueStream(mpsc::Receiver<Value>);

impl futures_util::Stream for ReceiverValueStream {
    type Item = Value;
    fn poll_next(mut self: std::pin::Pin<&mut Self>, cx: &mut std::task::Context<'_>) -> std::task::Poll<Option<Value>> {
        self.0.poll_recv(cx)
    }
}

pub(crate) static REGISTRY: LazyLock<Mutex<HashMap<String, RegistryEntry>>> = LazyLock::new(|| {
    Mutex::new(HashMap::new())
});

/// Shutdown sender for the auto-serve loop. When `Some`, a server is running.
static AUTO_SERVE_TX: LazyLock<Mutex<Option<tokio::sync::watch::Sender<bool>>>> = LazyLock::new(|| {
    Mutex::new(None)
});

pub trait ToWireError {
    fn to_wire_error(self) -> CallwireError;
}

impl ToWireError for CallwireError {
    fn to_wire_error(self) -> CallwireError {
        self
    }
}

impl ToWireError for String {
    fn to_wire_error(self) -> CallwireError {
        CallwireError {
            error_type: "Error".to_string(),
            message: self,
        }
    }
}

impl ToWireError for &str {
    fn to_wire_error(self) -> CallwireError {
        CallwireError {
            error_type: "Error".to_string(),
            message: self.to_string(),
        }
    }
}

impl ToWireError for std::convert::Infallible {
    fn to_wire_error(self) -> CallwireError {
        unreachable!()
    }
}

pub fn register_unary<F, Args, Resp, Err>(name: &str, func: F)
where
    F: Fn(Args) -> std::result::Result<Resp, Err> + Send + Sync + 'static,
    Args: serde::de::DeserializeOwned + Send + 'static,
    Resp: serde::Serialize + Send + 'static,
    Err: ToWireError + Send + 'static,
{
    let func = Arc::new(func);
    let wrapped = Arc::new(move |args_val: Value| {
        let func = func.clone();
        Box::pin(async move {
            let clean_args = match args_val {
                Value::Array(ref vec) if vec.is_empty() => Value::Nil,
                other => other,
            };
            let args: Args = rmpv::ext::from_value(clean_args)
                .map_err(|e| CallwireError {
                    error_type: "TypeError".to_string(),
                    message: format!("Argument deserialization failed: {}", e),
                })?;
            match func(args) {
                Ok(resp) => {
                    let bytes = crate::codec::to_vec_map(&resp).map_err(|e| CallwireError {
                        error_type: "SerializationError".to_string(),
                        message: e.to_string(),
                    })?;
                    rmp_serde::from_slice(&bytes).map_err(|e| CallwireError {
                        error_type: "SerializationError".to_string(),
                        message: e.to_string(),
                    })
                }
                Err(err) => Err(err.to_wire_error()),
            }
        }) as BoxFuture<'static, std::result::Result<Value, CallwireError>>
    });
    
    REGISTRY.lock().unwrap().insert(name.to_string(), RegistryEntry::Unary(wrapped));
    spawn_auto_serve_if_idle();
}

pub fn register_stream<F, Args, S, Item, Err, SErr>(name: &str, func: F)
where
    F: Fn(Args) -> std::result::Result<S, Err> + Send + Sync + 'static,
    Args: serde::de::DeserializeOwned + Send + 'static,
    S: futures_util::Stream<Item = std::result::Result<Item, SErr>> + Send + 'static,
    Item: serde::Serialize + Send + 'static,
    Err: ToWireError + Send + 'static,
    SErr: ToWireError + Send + 'static,
{
    let func = Arc::new(func);
    let wrapped = Arc::new(move |args_val: Value| {
        let func = func.clone();
        Box::pin(async move {
            let clean_args = match args_val {
                Value::Array(ref vec) if vec.is_empty() => Value::Nil,
                other => other,
            };
            let args: Args = match rmpv::ext::from_value(clean_args) {
                Ok(args) => args,
                Err(e) => return Err(CallwireError {
                    error_type: "TypeError".to_string(),
                    message: format!("Argument deserialization failed: {}", e),
                }),
            };
            match func(args) {
                Ok(stream) => {
                    use futures_util::StreamExt;
                    let mapped_stream = stream.map(|res| {
                        match res {
                            Ok(item) => {
                                let bytes = match crate::codec::to_vec_map(&item) {
                                    Ok(b) => b,
                                    Err(e) => return Err(CallwireError {
                                        error_type: "SerializationError".to_string(),
                                        message: e.to_string(),
                                    }),
                                };
                                rmp_serde::from_slice(&bytes).map_err(|e| CallwireError {
                                    error_type: "SerializationError".to_string(),
                                    message: e.to_string(),
                                })
                            }
                            Err(e) => Err(e.to_wire_error()),
                        }
                    });
                    let boxed: BoxStream<'static, std::result::Result<Value, CallwireError>> = Box::pin(mapped_stream);
                    Ok(boxed)
                }
                Err(err) => Err(err.to_wire_error()),
            }
        }) as BoxFuture<'static, std::result::Result<BoxStream<'static, std::result::Result<Value, CallwireError>>, CallwireError>>
    });
    
    REGISTRY.lock().unwrap().insert(name.to_string(), RegistryEntry::Stream(wrapped));
    spawn_auto_serve_if_idle();
}

pub fn register_client_stream<F, Args, Resp, Err>(name: &str, func: F)
where
    F: Fn(BoxStream<'static, Args>) -> std::result::Result<Resp, Err> + Send + Sync + 'static,
    Args: serde::de::DeserializeOwned + Send + 'static,
    Resp: serde::Serialize + Send + 'static,
    Err: ToWireError + Send + 'static,
{
    let func = Arc::new(func);
    let wrapped = Arc::new(move |value_stream: BoxStream<'static, Value>| {
        let func = func.clone();
        Box::pin(async move {
            use futures_util::StreamExt;
            let args_stream: BoxStream<'static, Args> = Box::pin(value_stream.filter_map(|v| async move {
                rmpv::ext::from_value(v).ok()
            }));
            match func(args_stream) {
                Ok(resp) => {
                    let bytes = crate::codec::to_vec_map(&resp).map_err(|e| CallwireError {
                        error_type: "SerializationError".to_string(),
                        message: e.to_string(),
                    })?;
                    rmp_serde::from_slice(&bytes).map_err(|e| CallwireError {
                        error_type: "SerializationError".to_string(),
                        message: e.to_string(),
                    })
                }
                Err(err) => Err(err.to_wire_error()),
            }
        }) as BoxFuture<'static, std::result::Result<Value, CallwireError>>
    });

    REGISTRY.lock().unwrap().insert(name.to_string(), RegistryEntry::ClientStream(wrapped));
    spawn_auto_serve_if_idle();
}

pub fn register_bidi<F, Args, S, Item, Err, SErr>(name: &str, func: F)
where
    F: Fn(BoxStream<'static, Args>) -> std::result::Result<S, Err> + Send + Sync + 'static,
    Args: serde::de::DeserializeOwned + Send + 'static,
    S: futures_util::Stream<Item = std::result::Result<Item, SErr>> + Send + 'static,
    Item: serde::Serialize + Send + 'static,
    Err: ToWireError + Send + 'static,
    SErr: ToWireError + Send + 'static,
{
    let func = Arc::new(func);
    let wrapped = Arc::new(move |value_stream: BoxStream<'static, Value>| {
        let func = func.clone();
        Box::pin(async move {
            use futures_util::StreamExt;
            let args_stream: BoxStream<'static, Args> = Box::pin(value_stream.filter_map(|v| async move {
                rmpv::ext::from_value(v).ok()
            }));
            match func(args_stream) {
                Ok(stream) => {
                    let mapped_stream = stream.map(|res| {
                        match res {
                            Ok(item) => {
                                let bytes = match crate::codec::to_vec_map(&item) {
                                    Ok(b) => b,
                                    Err(e) => return Err(CallwireError {
                                        error_type: "SerializationError".to_string(),
                                        message: e.to_string(),
                                    }),
                                };
                                rmp_serde::from_slice(&bytes).map_err(|e| CallwireError {
                                    error_type: "SerializationError".to_string(),
                                    message: e.to_string(),
                                })
                            }
                            Err(e) => Err(e.to_wire_error()),
                        }
                    });
                    let boxed: BoxStream<'static, std::result::Result<Value, CallwireError>> = Box::pin(mapped_stream);
                    Ok(boxed)
                }
                Err(err) => Err(err.to_wire_error()),
            }
        }) as BoxFuture<'static, std::result::Result<BoxStream<'static, std::result::Result<Value, CallwireError>>, CallwireError>>
    });

    REGISTRY.lock().unwrap().insert(name.to_string(), RegistryEntry::Bidi(wrapped));
    spawn_auto_serve_if_idle();
}

/// Internal: check if a server is already running; if not, spawn one.
/// Called from register_* functions. Does NOT block — the caller must use
/// [`wait_serving`] if it needs to know when the listener is bound.
fn spawn_auto_serve_if_idle() {
    let mut tx_guard = AUTO_SERVE_TX.lock().unwrap();
    if tx_guard.is_some() {
        // Already running.
        return;
    }
    if std::env::var("CALLWIRE_AUTO").unwrap_or_default() == "0"
        || std::env::var("CALLWIRE_SPAWNED").as_deref() == Ok("1")
    {
        return;
    }

    let host = std::env::var("CALLWIRE_HOST").unwrap_or_else(|_| "localhost".to_string());
    let port = std::env::var("CALLWIRE_PORT").unwrap_or_else(|_| "9090".to_string());
    let addr = format!("{}:{}", host, port);

    let (tx, rx) = tokio::sync::watch::channel(false);
    *tx_guard = Some(tx);
    drop(tx_guard); // Release lock before spawning.

    match tokio::runtime::Handle::try_current() {
        Ok(handle) => {
            handle.spawn(async move {
                let listener = match TcpListener::bind(&addr).await {
                    Ok(l) => l,
                    Err(e) => {
                        eprintln!("callwire: auto-serve on {} failed to bind: {}", addr, e);
                        return;
                    }
                };
                run_accept_loop(listener, rx).await;
            });
        }
        Err(_) => {
            // Spawn a background OS thread with its own Tokio runtime if no global runtime exists.
            std::thread::spawn(move || {
                let rt = tokio::runtime::Builder::new_multi_thread()
                    .enable_all()
                    .build()
                    .unwrap();
                rt.block_on(async {
                    let listener = match TcpListener::bind(&addr).await {
                        Ok(l) => l,
                        Err(e) => {
                            eprintln!("callwire: auto-serve on {} failed to bind: {}", addr, e);
                            return;
                        }
                    };
                    run_accept_loop(listener, rx).await;
                });
            });
        }
    }
}

/// Start the auto-serve server and wait until the TCP listener is bound.
/// Returns when the listener is ready to accept connections.
/// This is a no-op if the server is already running.
pub async fn auto_serve() {
    {
        let guard = AUTO_SERVE_TX.lock().unwrap();
        if guard.is_some() {
            return;
        }
    }

    if std::env::var("CALLWIRE_AUTO").unwrap_or_default() == "0" {
        return;
    }

    let host = std::env::var("CALLWIRE_HOST").unwrap_or_else(|_| "localhost".to_string());
    let port = std::env::var("CALLWIRE_PORT").unwrap_or_else(|_| "9090".to_string());
    let addr = format!("{}:{}", host, port);

    // Bind the listener before storing the sender so callers know it's ready.
    let listener = match TcpListener::bind(&addr).await {
        Ok(l) => l,
        Err(e) => {
            eprintln!("callwire: auto-serve on {} failed to bind: {}", addr, e);
            return;
        }
    };

    let (tx, rx) = tokio::sync::watch::channel(false);
    {
        let mut guard = AUTO_SERVE_TX.lock().unwrap();
        if guard.is_some() {
            // Another task raced us and already bound — drop ours.
            return;
        }
        *guard = Some(tx);
    }

    tokio::spawn(run_accept_loop(listener, rx));
}

/// Explicitly start a server on `addr`, waiting until the listener is bound.
/// Returns a [`ServerHandle`] that can be used to shut it down.
pub async fn serve_on(addr: &str) -> Result<ServerHandle> {
    let listener = TcpListener::bind(addr).await?;
    let (tx, rx) = tokio::sync::watch::channel(false);
    tokio::spawn(run_accept_loop(listener, rx));
    Ok(ServerHandle { tx })
}

/// A handle to a running server. Drop or call `.close()` to stop it.
pub struct ServerHandle {
    pub(crate) tx: tokio::sync::watch::Sender<bool>,
}

impl ServerHandle {
    pub fn close(self) {
        let _ = self.tx.send(true);
    }
}

/// Shut down the global auto-serve server.
pub fn close() {
    let mut guard = AUTO_SERVE_TX.lock().unwrap();
    if let Some(tx) = guard.take() {
        let _ = tx.send(true);
    }
}

/// Serve forever on `addr` (no built-in shutdown).
pub async fn serve<A: tokio::net::ToSocketAddrs>(addr: A) -> Result<()> {
    let listener = TcpListener::bind(addr).await?;
    let (_tx, rx) = tokio::sync::watch::channel(false);
    run_accept_loop(listener, rx).await;
    Ok(())
}

pub(crate) async fn run_accept_loop(listener: TcpListener, mut rx: tokio::sync::watch::Receiver<bool>) {
    loop {
        tokio::select! {
            res = listener.accept() => {
                match res {
                    Ok((socket, _)) => {
                        tokio::spawn(handle_connection(socket, rx.clone()));
                    }
                    Err(_) => break,
                }
            }
            _ = rx.changed() => {
                if *rx.borrow() { break; }
            }
        }
    }
}

async fn handle_connection(socket: TcpStream, mut shutdown_rx: tokio::sync::watch::Receiver<bool>) {
    let (mut reader, stream_writer) = socket.into_split();
    let writer = Arc::new(tokio::sync::Mutex::new(stream_writer));
    let stream_inputs: Arc<Mutex<HashMap<u64, mpsc::Sender<StreamInputMsg>>>> =
        Arc::new(Mutex::new(HashMap::new()));

    loop {
        tokio::select! {
            res = crate::framing::read_frame(&mut reader) => {
                match res {
                    Ok(payload) => {
                        match crate::codec::unpack(&payload) {
                            Ok(msg) => {
                                let msg_type = msg.msg_type.as_str();

                                // Route follow-up frames of an in-progress streaming-input call
                                if msg_type == "stream_chunk" || msg_type == "stream_close" || msg_type == "stream_end" {
                                    let sender = {
                                        let inputs = stream_inputs.lock().unwrap();
                                        inputs.get(&msg.id).cloned()
                                    };
                                    if let Some(sender) = sender {
                                        let signal = if msg_type == "stream_chunk" {
                                            StreamInputMsg::Chunk(msg.result.unwrap_or(Value::Nil))
                                        } else {
                                            StreamInputMsg::Close
                                        };
                                        let _ = sender.send(signal).await;
                                    }
                                    continue;
                                }

                                if msg_type != "request" {
                                    continue;
                                }

                                let is_bidi = msg.stream.unwrap_or(false);
                                let func_name = msg.func.clone().unwrap_or_default();

                                let entry = {
                                    let reg = REGISTRY.lock().unwrap();
                                    reg.get(&func_name).cloned()
                                };

                                match entry {
                                    Some(RegistryEntry::Bidi(_)) if is_bidi => {
                                        let (tx, rx) = mpsc::channel(256);
                                        let id = msg.id;
                                        stream_inputs.lock().unwrap().insert(id, tx);
                                        let writer_clone = writer.clone();
                                        let inputs_clone = stream_inputs.clone();
                                        tokio::spawn(async move {
                                            dispatch_bidi(writer_clone, msg, rx).await;
                                            inputs_clone.lock().unwrap().remove(&id);
                                        });
                                    }
                                    Some(RegistryEntry::ClientStream(_)) if !is_bidi => {
                                        let (tx, rx) = mpsc::channel(256);
                                        stream_inputs.lock().unwrap().insert(msg.id, tx);
                                        let writer_clone = writer.clone();
                                        let inputs_clone = stream_inputs.clone();
                                        let id = msg.id;
                                        tokio::spawn(async move {
                                            dispatch_client_stream(writer_clone, msg, rx).await;
                                            inputs_clone.lock().unwrap().remove(&id);
                                        });
                                    }
                                    _ => {
                                        let writer_clone = writer.clone();
                                        tokio::spawn(async move {
                                            dispatch(writer_clone, msg).await;
                                        });
                                    }
                                }
                            }
                            Err(_) => {
                                // ignore malformed frame
                            }
                        }
                    }
                    Err(_) => break,
                }
            }
            _ = shutdown_rx.changed() => {
                if *shutdown_rx.borrow() { break; }
            }
        }
    }
}

async fn dispatch(writer: Arc<tokio::sync::Mutex<tokio::net::tcp::OwnedWriteHalf>>, msg: WireMessage) {
    let func_name = match &msg.func {
        Some(f) => f.clone(),
        None => {
            if let Ok(payload) = crate::codec::pack_error(msg.id, "TypeError", "missing func field") {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
            return;
        }
    };

    let entry = {
        let reg = REGISTRY.lock().unwrap();
        reg.get(&func_name).cloned()
    };

    let Some(entry) = entry else {
        if let Ok(payload) = crate::codec::pack_error(
            msg.id,
            "NotFoundError",
            &format!("function '{}' not exported", func_name),
        ) {
            let mut w = writer.lock().await;
            let _ = crate::framing::write_frame(&mut *w, &payload).await;
        }
        return;
    };

    let args = msg.args.unwrap_or(Value::Nil);

    match entry {
        RegistryEntry::Unary(handler) => {
            match handler(args).await {
                Ok(res) => {
                    if let Ok(payload) = crate::codec::pack_response(msg.id, &res) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
                Err(err) => {
                    if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
            }
        }
        RegistryEntry::Stream(handler) => {
            match handler(args).await {
                Ok(mut stream) => {
                    use futures_util::StreamExt;
                    while let Some(res) = stream.next().await {
                        match res {
                            Ok(val) => {
                                if let Ok(payload) = crate::codec::pack_stream_chunk(msg.id, &val) {
                                    let mut w = writer.lock().await;
                                    if crate::framing::write_frame(&mut *w, &payload).await.is_err() {
                                        return;
                                    }
                                }
                            }
                            Err(err) => {
                                if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                                    let mut w = writer.lock().await;
                                    let _ = crate::framing::write_frame(&mut *w, &payload).await;
                                }
                                return;
                            }
                        }
                    }
                    if let Ok(payload) = crate::codec::pack_stream_end(msg.id) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
                Err(err) => {
                    if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                        let mut w = writer.lock().await;
                        let _ = crate::framing::write_frame(&mut *w, &payload).await;
                    }
                }
            }
        }
        RegistryEntry::ClientStream(_) | RegistryEntry::Bidi(_) => {
            // Streaming-input calls are routed to dispatch_client_stream/dispatch_bidi
            // by handle_connection before reaching here — this arm should be unreachable
            // in normal operation, but guards against a stray unary/bidi mismatch.
            if let Ok(payload) = crate::codec::pack_error(
                msg.id,
                "TypeError",
                "function requires client-streaming or bidi-streaming call, not unary",
            ) {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
        }
    }
}

async fn dispatch_client_stream(
    writer: Arc<tokio::sync::Mutex<tokio::net::tcp::OwnedWriteHalf>>,
    msg: WireMessage,
    mut input_rx: mpsc::Receiver<StreamInputMsg>,
) {
    let func_name = msg.func.clone().unwrap_or_default();
    let entry = {
        let reg = REGISTRY.lock().unwrap();
        reg.get(&func_name).cloned()
    };

    let Some(RegistryEntry::ClientStream(handler)) = entry else {
        if let Ok(payload) = crate::codec::pack_error(
            msg.id,
            "NotFoundError",
            &format!("function '{}' not exported as client-stream", func_name),
        ) {
            let mut w = writer.lock().await;
            let _ = crate::framing::write_frame(&mut *w, &payload).await;
        }
        return;
    };

    let (value_tx, value_rx) = mpsc::channel::<Value>(256);
    tokio::spawn(async move {
        while let Some(signal) = input_rx.recv().await {
            match signal {
                StreamInputMsg::Chunk(v) => {
                    if value_tx.send(v).await.is_err() {
                        break;
                    }
                }
                StreamInputMsg::Close => break,
            }
        }
    });

    let value_stream: BoxStream<'static, Value> = Box::pin(ReceiverValueStream(value_rx));

    match handler(value_stream).await {
        Ok(res) => {
            if let Ok(payload) = crate::codec::pack_response(msg.id, &res) {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
        }
        Err(err) => {
            if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
        }
    }
}

async fn dispatch_bidi(
    writer: Arc<tokio::sync::Mutex<tokio::net::tcp::OwnedWriteHalf>>,
    msg: WireMessage,
    mut input_rx: mpsc::Receiver<StreamInputMsg>,
) {
    let func_name = msg.func.clone().unwrap_or_default();
    let entry = {
        let reg = REGISTRY.lock().unwrap();
        reg.get(&func_name).cloned()
    };

    let Some(RegistryEntry::Bidi(handler)) = entry else {
        if let Ok(payload) = crate::codec::pack_error(
            msg.id,
            "NotFoundError",
            &format!("function '{}' not exported as bidi-stream", func_name),
        ) {
            let mut w = writer.lock().await;
            let _ = crate::framing::write_frame(&mut *w, &payload).await;
        }
        return;
    };

    let (value_tx, value_rx) = mpsc::channel::<Value>(256);
    tokio::spawn(async move {
        while let Some(signal) = input_rx.recv().await {
            match signal {
                StreamInputMsg::Chunk(v) => {
                    if value_tx.send(v).await.is_err() {
                        break;
                    }
                }
                StreamInputMsg::Close => break,
            }
        }
    });

    let value_stream: BoxStream<'static, Value> = Box::pin(ReceiverValueStream(value_rx));

    match handler(value_stream).await {
        Ok(mut out_stream) => {
            use futures_util::StreamExt;
            while let Some(res) = out_stream.next().await {
                match res {
                    Ok(val) => {
                        if let Ok(payload) = crate::codec::pack_stream_chunk(msg.id, &val) {
                            let mut w = writer.lock().await;
                            if crate::framing::write_frame(&mut *w, &payload).await.is_err() {
                                return;
                            }
                        }
                    }
                    Err(err) => {
                        if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                            let mut w = writer.lock().await;
                            let _ = crate::framing::write_frame(&mut *w, &payload).await;
                        }
                        return;
                    }
                }
            }
            if let Ok(payload) = crate::codec::pack_stream_end(msg.id) {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
        }
        Err(err) => {
            if let Ok(payload) = crate::codec::pack_error(msg.id, &err.error_type, &err.message) {
                let mut w = writer.lock().await;
                let _ = crate::framing::write_frame(&mut *w, &payload).await;
            }
        }
    }
}
