use serde::{Serialize, Deserialize};
use rmpv::Value;

/// Wire messages are encoded as msgpack maps (string keys) so that optional
/// fields can be absent without breaking deserialization. This matches the
/// Python/Go implementations which use msgpack dicts.
pub fn to_vec_map<T: Serialize>(value: &T) -> Result<Vec<u8>, rmp_serde::encode::Error> {
    let mut buf = Vec::new();
    let mut ser = rmp_serde::Serializer::new(&mut buf)
        .with_struct_map();
    value.serialize(&mut ser)?;
    Ok(buf)
}


#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WireMessage {
    pub id: u64,
    #[serde(rename = "type")]
    pub msg_type: String,

    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub func: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub args: Option<Value>,

    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub result: Option<Value>,

    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub error_type: Option<String>,

    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub message: Option<String>,
}

pub fn pack_request<A: Serialize>(id: u64, func: &str, args: &A) -> Result<Vec<u8>, rmp_serde::encode::Error> {
    #[derive(Serialize)]
    struct RequestRef<'a, A> {
        id: u64,
        #[serde(rename = "type")]
        msg_type: &'static str,
        func: &'a str,
        args: &'a A,
    }

    let req = RequestRef {
        id,
        msg_type: "request",
        func,
        args,
    };
    to_vec_map(&req)
}

pub fn pack_response<R: Serialize>(id: u64, result: &R) -> Result<Vec<u8>, rmp_serde::encode::Error> {
    #[derive(Serialize)]
    struct ResponseRef<'a, R> {
        id: u64,
        #[serde(rename = "type")]
        msg_type: &'static str,
        result: &'a R,
    }
    let resp = ResponseRef {
        id,
        msg_type: "response",
        result,
    };
    to_vec_map(&resp)
}

pub fn pack_error(id: u64, error_type: &str, message: &str) -> Result<Vec<u8>, rmp_serde::encode::Error> {
    #[derive(Serialize)]
    struct ErrorRef<'a> {
        id: u64,
        #[serde(rename = "type")]
        msg_type: &'static str,
        error_type: &'a str,
        message: &'a str,
    }
    let err = ErrorRef {
        id,
        msg_type: "error",
        error_type,
        message,
    };
    to_vec_map(&err)
}

pub fn pack_stream_chunk<R: Serialize>(id: u64, result: &R) -> Result<Vec<u8>, rmp_serde::encode::Error> {
    #[derive(Serialize)]
    struct ChunkRef<'a, R> {
        id: u64,
        #[serde(rename = "type")]
        msg_type: &'static str,
        result: &'a R,
    }
    let chunk = ChunkRef {
        id,
        msg_type: "stream_chunk",
        result,
    };
    to_vec_map(&chunk)
}

pub fn pack_stream_end(id: u64) -> Result<Vec<u8>, rmp_serde::encode::Error> {
    #[derive(Serialize)]
    struct EndRef {
        id: u64,
        #[serde(rename = "type")]
        msg_type: &'static str,
    }
    let end = EndRef {
        id,
        msg_type: "stream_end",
    };
    to_vec_map(&end)
}

/// Decode an inbound wire message. Accepts both map and array formats.
pub fn unpack(payload: &[u8]) -> Result<WireMessage, rmp_serde::decode::Error> {
    rmp_serde::from_slice(payload)
}
