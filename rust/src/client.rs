use std::collections::HashMap;
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;
use tokio::net::TcpStream;
use tokio::sync::{mpsc, oneshot};
use serde::Serialize;
use serde::de::DeserializeOwned;
use crate::errors::{Error, Result, CallwireError};
use crate::codec::{pack_request, WireMessage};
use crate::tls::TlsConfig;

use tokio::sync::Mutex as TokioMutex;

#[derive(Clone)]
pub struct Client {
    inner: Arc<ClientInner>,
}

enum ClientInner {
    Direct {
        tx: mpsc::Sender<ActorCommand>,
        next_id: Arc<AtomicU64>,
    },
    Routing {
        registry_client: Box<Client>,
        route_cache: TokioMutex<HashMap<String, String>>,
        worker_clients: TokioMutex<HashMap<String, Client>>,
        reconnect: bool,
    },
}

enum ActorCommand {
    Call {
        id: u64,
        payload: Vec<u8>,
        tx: oneshot::Sender<Result<WireMessage>>,
    },
    CallStream {
        id: u64,
        payload: Vec<u8>,
        tx: mpsc::Sender<Result<WireMessage>>,
    },
    Shutdown,
}

#[derive(Clone)]
enum PendingSender {
    Unary(Arc<MutexOneshotSender>),
    Stream(mpsc::Sender<Result<WireMessage>>),
}

// oneshot::Sender doesn't implement Clone or sync safely, so we wrap it
struct MutexOneshotSender(std::sync::Mutex<Option<oneshot::Sender<Result<WireMessage>>>>);

impl MutexOneshotSender {
    fn new(tx: oneshot::Sender<Result<WireMessage>>) -> Self {
        Self(std::sync::Mutex::new(Some(tx)))
    }
    
    fn send(&self, val: Result<WireMessage>) {
        if let Ok(mut guard) = self.0.lock() {
            if let Some(tx) = guard.take() {
                let _ = tx.send(val);
            }
        }
    }
}

impl Client {
    pub async fn connect<A: AsRef<str>>(addr: A) -> Result<Self> {
        Self::connect_impl(addr.as_ref().to_string(), false).await
    }

    pub async fn connect_with_reconnect<A: AsRef<str>>(addr: A) -> Result<Self> {
        Self::connect_impl(addr.as_ref().to_string(), true).await
    }

    async fn connect_impl(addr: String, reconnect: bool) -> Result<Self> {
        let stream = TcpStream::connect(&addr).await?;
        let (tx, rx) = mpsc::channel(1024);
        
        tokio::spawn(run_actor(addr, reconnect, stream, rx));
        
        Ok(Self {
            inner: Arc::new(ClientInner::Direct {
                tx,
                next_id: Arc::new(AtomicU64::new(0)),
            })
        })
    }

    /// Connect over TLS. Called from [`TlsConfig::connect`] and
    /// [`TlsConfig::connect_with_reconnect`].
    pub(crate) async fn connect_tls_impl(addr: String, cfg: TlsConfig, reconnect: bool) -> Result<Self> {
        let stream = crate::tls::dial_tls(&addr, &cfg).await
            .map_err(|e| Error::Internal(e.to_string()))?;
        let (tx, rx) = mpsc::channel(1024);
        tokio::spawn(run_tls_actor(addr, cfg, reconnect, stream, rx));
        Ok(Self {
            inner: Arc::new(ClientInner::Direct {
                tx,
                next_id: Arc::new(AtomicU64::new(0)),
            })
        })
    }

    pub async fn connect_registry<A: AsRef<str>>(addr: A) -> Result<Self> {
        Self::connect_registry_impl(addr.as_ref().to_string(), false).await
    }

    pub async fn connect_registry_with_reconnect<A: AsRef<str>>(addr: A) -> Result<Self> {
        Self::connect_registry_impl(addr.as_ref().to_string(), true).await
    }

    async fn connect_registry_impl(addr: String, reconnect: bool) -> Result<Self> {
        let registry_client = if reconnect {
            Self::connect_with_reconnect(&addr).await?
        } else {
            Self::connect(&addr).await?
        };
        Ok(Self {
            inner: Arc::new(ClientInner::Routing {
                registry_client: Box::new(registry_client),
                route_cache: TokioMutex::new(HashMap::new()),
                worker_clients: TokioMutex::new(HashMap::new()),
                reconnect,
            })
        })
    }

    /// Close the connection and shut down the background actor task.
    /// Once called, the client will no longer process requests.
    pub fn close(&self) {
        match &*self.inner {
            ClientInner::Direct { tx, .. } => {
                let _ = tx.try_send(ActorCommand::Shutdown);
            }
            ClientInner::Routing { registry_client, worker_clients, .. } => {
                registry_client.close();
                let workers = worker_clients.try_lock();
                if let Ok(mut workers) = workers {
                    for (_, client) in workers.drain() {
                        client.close();
                    }
                }
            }
        }
    }

    fn resolve_worker<'a>(&'a self, func_name: &'a str) -> futures_util::future::BoxFuture<'a, Result<Option<Self>>> {
        use futures_util::FutureExt;
        async move {
            match &*self.inner {
                ClientInner::Direct { .. } => Ok(None),
                ClientInner::Routing { registry_client, route_cache, worker_clients, reconnect } => {
                    if func_name.starts_with("callwire.") {
                        return Ok(None);
                    }
                    
                    let mut cache = route_cache.lock().await;
                    let addr = if let Some(addr) = cache.get(func_name) {
                        addr.clone()
                    } else {
                        let addrs: Vec<String> = registry_client.import("callwire.discover", &(func_name.to_string(),)).await?;
                        if addrs.is_empty() {
                            return Err(Error::Remote(CallwireError {
                                error_type: "NotFoundError".to_string(),
                                message: format!("function '{}' not registered", func_name),
                            }));
                        }
                        let addr = addrs[0].clone();
                        cache.insert(func_name.to_string(), addr.clone());
                        addr
                    };

                    let mut workers = worker_clients.lock().await;
                    if !workers.contains_key(&addr) {
                        let client = if *reconnect {
                            Client::connect_with_reconnect(&addr).await?
                        } else {
                            Client::connect(&addr).await?
                        };
                        workers.insert(addr.clone(), client);
                    }
                    Ok(Some(workers.get(&addr).unwrap().clone()))
                }
            }
        }.boxed()
    }

    pub async fn import<Resp, Args>(&self, func_name: &str, args: &Args) -> Result<Resp>
    where
        Resp: DeserializeOwned,
        Args: Serialize,
    {
        if let Some(worker) = self.resolve_worker(func_name).await? {
            worker.import_direct(func_name, args).await
        } else {
            self.import_direct(func_name, args).await
        }
    }

    pub(crate) async fn import_direct<Resp, Args>(&self, func_name: &str, args: &Args) -> Result<Resp>
    where
        Resp: DeserializeOwned,
        Args: Serialize,
    {
        let (tx, next_id) = match &*self.inner {
            ClientInner::Direct { tx, next_id } => (tx, next_id),
            _ => unreachable!(),
        };

        let id = next_id.fetch_add(1, Ordering::SeqCst) + 1;
        let payload = pack_request(id, func_name, args)?;
        let (otx, rx) = oneshot::channel();
        
        tx.send(ActorCommand::Call { id, payload, tx: otx }).await
            .map_err(|_| Error::ConnectionClosed)?;
            
        let msg = rx.await.map_err(|_| Error::ConnectionClosed)??;
        
        if msg.msg_type == "error" {
            let err_type = msg.error_type.unwrap_or_else(|| "Error".to_string());
            let err_msg = msg.message.unwrap_or_else(|| "unknown error".to_string());
            return Err(Error::Remote(CallwireError {
                error_type: err_type,
                message: err_msg,
            }));
        }
        
        let val = msg.result.unwrap_or(rmpv::Value::Nil);
        let resp: Resp = rmpv::ext::from_value(val)
            .map_err(|e| Error::TypeError(format!("failed to decode response: {e}")))?;
        Ok(resp)
    }

    pub async fn import_stream<Resp, Args>(&self, func_name: &str, args: &Args) -> Result<CallwireStream<Resp>>
    where
        Resp: DeserializeOwned + Unpin,
        Args: Serialize,
    {
        if let Some(worker) = self.resolve_worker(func_name).await? {
            worker.import_stream_direct(func_name, args).await
        } else {
            self.import_stream_direct(func_name, args).await
        }
    }

    pub(crate) async fn import_stream_direct<Resp, Args>(&self, func_name: &str, args: &Args) -> Result<CallwireStream<Resp>>
    where
        Resp: DeserializeOwned + Unpin,
        Args: Serialize,
    {
        let (tx, next_id) = match &*self.inner {
            ClientInner::Direct { tx, next_id } => (tx, next_id),
            _ => unreachable!(),
        };

        let id = next_id.fetch_add(1, Ordering::SeqCst) + 1;
        let payload = pack_request(id, func_name, args)?;
        let (otx, rx) = mpsc::channel(256);
        
        tx.send(ActorCommand::CallStream { id, payload, tx: otx }).await
            .map_err(|_| Error::ConnectionClosed)?;
            
        Ok(CallwireStream {
            rx,
            _phantom: std::marker::PhantomData,
        })
    }

    pub async fn batch(&self, calls: Vec<(&str, rmpv::Value)>) -> Result<Vec<rmpv::Value>> {
        let mut futures = Vec::new();
        for (func_name, args) in calls {
            let func_name = func_name.to_string();
            futures.push(async move {
                self.import::<rmpv::Value, rmpv::Value>(&func_name, &args).await
            });
        }
        let results = futures_util::future::join_all(futures).await;
        let mut out = Vec::new();
        for res in results {
            out.push(res?);
        }
        Ok(out)
    }
}

pub struct CallwireStream<Resp> {
    rx: mpsc::Receiver<Result<WireMessage>>,
    _phantom: std::marker::PhantomData<Resp>,
}

impl<Resp> futures_util::Stream for CallwireStream<Resp>
where
    Resp: DeserializeOwned + Unpin,
{
    type Item = Result<Resp>;
    
    fn poll_next(mut self: std::pin::Pin<&mut Self>, cx: &mut std::task::Context<'_>) -> std::task::Poll<Option<Self::Item>> {
        match self.rx.poll_recv(cx) {
            std::task::Poll::Ready(Some(Ok(msg))) => {
                if msg.msg_type == "stream_end" {
                    std::task::Poll::Ready(None)
                } else if msg.msg_type == "error" {
                    let err_type = msg.error_type.unwrap_or_else(|| "Error".to_string());
                    let err_msg = msg.message.unwrap_or_else(|| "unknown error".to_string());
                    std::task::Poll::Ready(Some(Err(Error::Remote(CallwireError {
                        error_type: err_type,
                        message: err_msg,
                    }))))
                } else if msg.msg_type == "stream_chunk" {
                    let val = msg.result.unwrap_or(rmpv::Value::Nil);
                    match rmpv::ext::from_value(val) {
                        Ok(v) => std::task::Poll::Ready(Some(Ok(v))),
                        Err(e) => std::task::Poll::Ready(Some(Err(Error::TypeError(format!("failed to decode stream chunk: {e}"))))),
                    }
                } else {
                    std::task::Poll::Ready(Some(Err(Error::Internal(format!("unexpected message type: {}", msg.msg_type)))))
                }
            }
            std::task::Poll::Ready(Some(Err(e))) => std::task::Poll::Ready(Some(Err(e))),
            std::task::Poll::Ready(None) => std::task::Poll::Ready(None),
            std::task::Poll::Pending => std::task::Poll::Pending,
        }
    }
}

enum ReaderMessage {
    Frame(WireMessage),
    Closed,
}

fn spawn_reader(mut read_half: tokio::net::tcp::OwnedReadHalf, tx: mpsc::Sender<ReaderMessage>) {
    tokio::spawn(async move {
        loop {
            match crate::framing::read_frame(&mut read_half).await {
                Ok(payload) => {
                    match crate::codec::unpack(&payload) {
                        Ok(msg) => {
                            if tx.send(ReaderMessage::Frame(msg)).await.is_err() {
                                break;
                            }
                        }
                        Err(_) => {
                            // ignore malformed packet
                        }
                    }
                }
                Err(_) => {
                    let _ = tx.send(ReaderMessage::Closed).await;
                    break;
                }
            }
        }
    });
}

async fn reconnect_loop(addr: &str) -> Result<TcpStream> {
    let mut backoff = Duration::from_millis(50);
    let max_backoff = Duration::from_secs(5);
    loop {
        tokio::time::sleep(backoff).await;
        backoff = std::cmp::min(backoff * 2, max_backoff);
        if let Ok(stream) = TcpStream::connect(addr).await {
            return Ok(stream);
        }
    }
}

fn drain_pending(pending: &mut HashMap<u64, PendingSender>) {
    for (_, sender) in pending.drain() {
        match sender {
            PendingSender::Unary(tx) => {
                tx.send(Err(Error::ConnectionClosed));
            }
            PendingSender::Stream(tx) => {
                tokio::spawn(async move {
                    let _ = tx.send(Err(Error::ConnectionClosed)).await;
                });
            }
        }
    }
}

async fn run_actor(
    addr: String,
    reconnect: bool,
    stream: TcpStream,
    mut rx: mpsc::Receiver<ActorCommand>,
) {
    let mut pending: HashMap<u64, PendingSender> = HashMap::new();
    
    let (stream_reader, stream_writer) = stream.into_split();
    let (reader_tx, mut reader_rx) = mpsc::channel(1024);
    spawn_reader(stream_reader, reader_tx.clone());
    
    let mut writer = Some(stream_writer);
    
    loop {
        tokio::select! {
            cmd = rx.recv() => {
                let Some(cmd) = cmd else {
                    break;
                };
                match cmd {
                    ActorCommand::Shutdown => break,
                    ActorCommand::Call { id, payload, tx } => {
                        let sender = PendingSender::Unary(Arc::new(MutexOneshotSender::new(tx)));
                        pending.insert(id, sender);
                        let write_ok = if let Some(w) = &mut writer {
                            crate::framing::write_frame(w, &payload).await.is_ok()
                        } else {
                            false
                        };
                        
                        if !write_ok {
                            drop(writer.take());
                            drain_pending(&mut pending);
                            if !reconnect {
                                break;
                            }
                            if let Ok(new_stream) = reconnect_loop(&addr).await {
                                let (new_r, new_w) = new_stream.into_split();
                                writer = Some(new_w);
                                spawn_reader(new_r, reader_tx.clone());
                            } else {
                                break;
                            }
                        }
                    }
                    ActorCommand::CallStream { id, payload, tx } => {
                        let sender = PendingSender::Stream(tx);
                        pending.insert(id, sender);
                        let write_ok = if let Some(w) = &mut writer {
                            crate::framing::write_frame(w, &payload).await.is_ok()
                        } else {
                            false
                        };
                        
                        if !write_ok {
                            drop(writer.take());
                            drain_pending(&mut pending);
                            if !reconnect {
                                break;
                            }
                            if let Ok(new_stream) = reconnect_loop(&addr).await {
                                let (new_r, new_w) = new_stream.into_split();
                                writer = Some(new_w);
                                spawn_reader(new_r, reader_tx.clone());
                            } else {
                                break;
                            }
                        }
                    }
                }
            }
            msg = reader_rx.recv() => {
                let Some(msg) = msg else {
                    continue;
                };
                match msg {
                    ReaderMessage::Frame(msg) => {
                        let id = msg.id;
                        let msg_type = msg.msg_type.clone();
                        
                        let remove = msg_type != "stream_chunk";
                        let entry = if remove {
                            pending.remove(&id)
                        } else {
                            pending.get(&id).cloned()
                        };
                        
                        if let Some(sender) = entry {
                            match sender {
                                PendingSender::Unary(tx) => {
                                    tx.send(Ok(msg));
                                }
                                PendingSender::Stream(tx) => {
                                    if tx.send(Ok(msg)).await.is_err() {
                                        pending.remove(&id);
                                    }
                                }
                            }
                        }
                    }
                    ReaderMessage::Closed => {
                        #[allow(unused_assignments)]
                        {
                            writer = None;
                        }
                        drain_pending(&mut pending);
                        if !reconnect {
                            break;
                        }
                        if let Ok(new_stream) = reconnect_loop(&addr).await {
                            let (new_r, new_w) = new_stream.into_split();
                            writer = Some(new_w);
                            spawn_reader(new_r, reader_tx.clone());
                        } else {
                            break;
                        }
                    }
                }
            }
        }
    }
    drain_pending(&mut pending);
}

// ──────────────────────────────────────────────────────────────────────────────
// TLS actor
// ──────────────────────────────────────────────────────────────────────────────

type TlsClientStream = tokio_rustls::client::TlsStream<TcpStream>;

fn spawn_tls_reader(stream_reader: tokio::io::ReadHalf<TlsClientStream>, tx: mpsc::Sender<ReaderMessage>) {
    tokio::spawn(async move {
        let mut r = stream_reader;
        loop {
            match crate::framing::read_frame(&mut r).await {
                Ok(payload) => {
                    match crate::codec::unpack(&payload) {
                        Ok(msg) => {
                            if tx.send(ReaderMessage::Frame(msg)).await.is_err() {
                                break;
                            }
                        }
                        Err(_) => { /* ignore malformed */ }
                    }
                }
                Err(_) => {
                    let _ = tx.send(ReaderMessage::Closed).await;
                    break;
                }
            }
        }
    });
}

async fn tls_reconnect_loop(addr: &str, cfg: &TlsConfig) -> std::io::Result<TlsClientStream> {
    let mut backoff = Duration::from_millis(50);
    let max_backoff = Duration::from_secs(5);
    loop {
        tokio::time::sleep(backoff).await;
        backoff = std::cmp::min(backoff * 2, max_backoff);
        if let Ok(stream) = crate::tls::dial_tls(addr, cfg).await {
            return Ok(stream);
        }
    }
}

async fn run_tls_actor(
    addr: String,
    cfg: TlsConfig,
    reconnect: bool,
    stream: TlsClientStream,
    mut rx: mpsc::Receiver<ActorCommand>,
) {
    let mut pending: HashMap<u64, PendingSender> = HashMap::new();

    let (stream_reader, stream_writer) = tokio::io::split(stream);
    let (reader_tx, mut reader_rx) = mpsc::channel(1024);
    spawn_tls_reader(stream_reader, reader_tx.clone());

    let mut writer: Option<tokio::io::WriteHalf<TlsClientStream>> = Some(stream_writer);

    loop {
        tokio::select! {
            cmd = rx.recv() => {
                let Some(cmd) = cmd else { break; };
                match cmd {
                    ActorCommand::Shutdown => break,
                    ActorCommand::Call { id, payload, tx } => {
                        let sender = PendingSender::Unary(Arc::new(MutexOneshotSender::new(tx)));
                        pending.insert(id, sender);
                        let write_ok = if let Some(w) = &mut writer {
                            crate::framing::write_frame(w, &payload).await.is_ok()
                        } else { false };
                        if !write_ok {
                            drop(writer.take());
                            drain_pending(&mut pending);
                            if !reconnect { break; }
                            match tls_reconnect_loop(&addr, &cfg).await {
                                Ok(new_stream) => {
                                    let (nr, nw) = tokio::io::split(new_stream);
                                    writer = Some(nw);
                                    spawn_tls_reader(nr, reader_tx.clone());
                                }
                                Err(_) => break,
                            }
                        }
                    }
                    ActorCommand::CallStream { id, payload, tx } => {
                        let sender = PendingSender::Stream(tx);
                        pending.insert(id, sender);
                        let write_ok = if let Some(w) = &mut writer {
                            crate::framing::write_frame(w, &payload).await.is_ok()
                        } else { false };
                        if !write_ok {
                            drop(writer.take());
                            drain_pending(&mut pending);
                            if !reconnect { break; }
                            match tls_reconnect_loop(&addr, &cfg).await {
                                Ok(new_stream) => {
                                    let (nr, nw) = tokio::io::split(new_stream);
                                    writer = Some(nw);
                                    spawn_tls_reader(nr, reader_tx.clone());
                                }
                                Err(_) => break,
                            }
                        }
                    }
                }
            }
            msg = reader_rx.recv() => {
                let Some(msg) = msg else { continue; };
                match msg {
                    ReaderMessage::Frame(msg) => {
                        let id = msg.id;
                        let msg_type = msg.msg_type.clone();
                        let remove = msg_type != "stream_chunk";
                        let entry = if remove {
                            pending.remove(&id)
                        } else {
                            pending.get(&id).cloned()
                        };
                        if let Some(sender) = entry {
                            match sender {
                                PendingSender::Unary(tx) => { tx.send(Ok(msg)); }
                                PendingSender::Stream(tx) => {
                                    if tx.send(Ok(msg)).await.is_err() {
                                        pending.remove(&id);
                                    }
                                }
                            }
                        }
                    }
                    ReaderMessage::Closed => {
                        #[allow(unused_assignments)]
                        { writer = None; }
                        drain_pending(&mut pending);
                        if !reconnect { break; }
                        match tls_reconnect_loop(&addr, &cfg).await {
                            Ok(new_stream) => {
                                let (nr, nw) = tokio::io::split(new_stream);
                                writer = Some(nw);
                                spawn_tls_reader(nr, reader_tx.clone());
                            }
                            Err(_) => break,
                        }
                    }
                }
            }
        }
    }
    drain_pending(&mut pending);
}
