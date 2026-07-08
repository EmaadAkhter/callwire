import msgpack


def pack_request(id: int, func: str, args: list, stream: bool = False) -> bytes:
    msg = {"id": id, "type": "request", "func": func, "args": args}
    if stream:
        msg["stream"] = True
    return msgpack.packb(msg)


def pack_response(id: int, result) -> bytes:
    return msgpack.packb({"id": id, "type": "response", "result": result})


def pack_error(id: int, error_type: str, message: str) -> bytes:
    return msgpack.packb(
        {"id": id, "type": "error", "error_type": error_type, "message": message}
    )


def pack_stream_chunk(id: int, result) -> bytes:
    return msgpack.packb({"id": id, "type": "stream_chunk", "result": result})


def pack_stream_end(id: int) -> bytes:
    return msgpack.packb({"id": id, "type": "stream_end"})


def pack_stream_chunk(id: int, result) -> bytes:
    return msgpack.packb({"id": id, "type": "stream_chunk", "result": result})


def pack_stream_close(id: int) -> bytes:
    return msgpack.packb({"id": id, "type": "stream_close"})


def pack_bidi_request(id: int, func: str, args: list = None) -> bytes:
    if args is None:
        args = []
    return msgpack.packb({"id": id, "type": "request", "func": func, "args": args, "stream": True})


def unpack(payload: bytes) -> dict:
    # raw=False decodes str properly — don't drop this flag
    #
    # Known edge: Python None unpacks into Go as the type's zero value (0 for int).
    # See SPEC.md type mapping table for details.
    return msgpack.unpackb(payload, raw=False)
