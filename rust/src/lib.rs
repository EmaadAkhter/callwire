pub mod client;
pub mod server;
pub mod framing;
pub mod codec;
pub mod errors;
pub mod tls;
pub mod registry;
pub mod orchestration;

pub use client::{Client, CallwireStream};
pub use server::{register_unary, register_stream, serve, serve_on, auto_serve, close, ServerHandle, ToWireError};
pub use tls::{TlsConfig, serve_on_tls};
pub use registry::{serve_registry, DiscoverPool};
pub use errors::{CallwireError, Error, Result};
pub use orchestration::{init, OrchestratorGuard};
pub use rmpv;

#[macro_export]
macro_rules! export {
    ($name:expr, $func:expr) => {
        $crate::register_unary($name, $func);
    };
}

#[macro_export]
macro_rules! export_stream {
    ($name:expr, $func:expr) => {
        $crate::register_stream($name, $func);
    };
}



