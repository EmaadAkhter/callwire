use thiserror::Error;

#[derive(Debug, Error, Clone, serde::Serialize, serde::Deserialize)]
#[error("{error_type}: {message}")]
pub struct CallwireError {
    #[serde(rename = "error_type")]
    pub error_type: String,
    #[serde(rename = "message")]
    pub message: String,
}

#[derive(Debug, Error)]
pub enum Error {
    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("MessagePack decode error: {0}")]
    MsgpackDecode(#[from] rmp_serde::decode::Error),

    #[error("MessagePack encode error: {0}")]
    MsgpackEncode(#[from] rmp_serde::encode::Error),

    #[error("Callwire remote error: {0}")]
    Remote(#[from] CallwireError),

    #[error("Connection closed")]
    ConnectionClosed,

    #[error("Type error: {0}")]
    TypeError(String),

    #[error("Not found: {0}")]
    NotFound(String),

    #[error("Internal error: {0}")]
    Internal(String),
}

pub type Result<T> = std::result::Result<T, Error>;
