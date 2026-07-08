import atexit
import collections.abc
import inspect
import os
import queue
import socket
import ssl
import threading
import time
import typing

from .framing import read_frame, write_frame
from .codec import pack_response, pack_error, pack_stream_chunk, pack_stream_end, pack_stream_close, unpack
from .errors import exception_to_wire

_registry = {}

_AUTO_CONFIG = {
    "host": os.environ.get("CALLWIRE_HOST", "localhost"),
    "port": int(os.environ.get("CALLWIRE_PORT", "9090")),
    "auto": os.environ.get("CALLWIRE_AUTO", "1") != "0" and os.environ.get("CALLWIRE_SPAWNED") != "1",
}
_auto_started = False
_auto_listener = None


def configure(host=None, port=None, auto=None):
    if _registry:
        print(
            "callwire: warning: configure() called after @export — "
            "set before any @export for it to take effect"
        )
    if host is not None:
        _AUTO_CONFIG["host"] = host
    if port is not None:
        _AUTO_CONFIG["port"] = port
    if auto is not None:
        _AUTO_CONFIG["auto"] = auto


def export(func):
    _registry[func.__name__] = func
    if _AUTO_CONFIG["auto"]:
        _ensure_server()
    return func


def _ensure_server():
    global _auto_started, _auto_listener
    if _auto_started:
        return
    _auto_started = True
    host = _AUTO_CONFIG["host"]
    port = _AUTO_CONFIG["port"]
    try:
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind((host, port))
        listener.listen()
        listener.settimeout(1.0)
        _auto_listener = listener
        t = threading.Thread(target=_auto_serve_loop, args=(listener,), daemon=True)
        t.start()
        print(f"callwire: listening on {host}:{port}")
    except OSError as e:
        print(f"callwire: failed to start server on {host}:{port}: {e}")
        _auto_started = False
        raise RuntimeError(f"callwire: failed to start server on {host}:{port}: {e}") from e


def _auto_serve_loop(listener):
    try:
        while True:
            try:
                conn, _ = listener.accept()
                threading.Thread(target=_handle_connection, args=(conn,), daemon=True).start()
            except socket.timeout:
                continue
    except OSError:
        pass


def serve(host="localhost", port=9090, tls=None):
    """Start the callwire server, blocking until shutdown.

    Args:
        host: bind address (default "localhost").
        port: bind port (default 9090).
        tls: optional dict with keys:
            - "certfile" (str, required): path to PEM server certificate.
            - "keyfile"  (str, required): path to PEM private key.
            - "cafile"   (str, optional): path to PEM CA cert; if given,
              client certificates are required (mTLS).
    """
    global _auto_listener, _auto_started
    if _auto_listener is not None:
        if host == _AUTO_CONFIG["host"] and port == _AUTO_CONFIG["port"]:
            return
        _auto_listener.close()
        _auto_listener = None
        _auto_started = False
    _start_listener(host, port, tls=tls)
    while True:
        try:
            conn, _ = _auto_listener.accept()
            threading.Thread(target=_handle_connection, args=(conn,), daemon=True).start()
        except OSError:
            break


def _start_listener(host, port, tls=None):
    global _auto_listener, _auto_started
    raw = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    raw.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    raw.bind((host, port))
    raw.listen()
    if tls is not None:
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.load_cert_chain(
            certfile=tls["certfile"],
            keyfile=tls.get("keyfile"),
        )
        if tls.get("cafile"):
            ctx.load_verify_locations(cafile=tls["cafile"])
            ctx.verify_mode = ssl.CERT_REQUIRED
        listener = ctx.wrap_socket(raw, server_side=True)
    else:
        listener = raw
    _auto_listener = listener
    _auto_started = True
    scheme = "TLS" if tls else "plain"
    print(f"callwire: listening on {host}:{port} ({scheme})")


def _handle_connection(conn):
    streams = {}  # id -> queue for streaming calls
    try:
        while True:
            payload = read_frame(conn)
            msg = unpack(payload)

            # Route streaming messages to pending streams
            msg_type = msg.get("type")
            if msg_type in ("stream_chunk", "stream_close", "stream_end"):
                msg_id = msg["id"]
                if msg_id in streams:
                    streams[msg_id].put(msg)
                    if msg_type in ("stream_close", "stream_end"):
                        # Don't keep stream pending after close/end from client
                        pass
                    continue

            # New request: set up stream state if needed, then dispatch
            if msg.get("type") == "request":
                msg_id = msg["id"]
                func_name = msg.get("func")
                fn = _registry.get(func_name)

                # Check if this will be client-streaming or bidi-streaming
                is_bidi = msg.get("stream", False)
                if fn and _expects_stream_input(fn):
                    # Set up stream for this call
                    q = queue.Queue(maxsize=256)
                    streams[msg_id] = q
                    threading.Thread(
                        target=_dispatch_streaming,
                        args=(conn, msg, q, streams, is_bidi),
                        daemon=True
                    ).start()
                    continue

            # Unary call
            _dispatch(conn, msg)
    except (ConnectionError, EOFError):
        pass
    finally:
        conn.close()


def _expects_stream_input(fn):
    """Check if function expects a stream (generator/iterable input)."""
    if not inspect.isfunction(fn) and not inspect.ismethod(fn):
        return False
    sig = inspect.signature(fn)
    params = list(sig.parameters.values())
    if not params:
        return False
    # First param's type hint must be exactly typing.Iterator/Iterable/AsyncIterator
    # (or their collections.abc equivalents), not merely contain that name as a
    # substring — e.g. a param hinted `MyIteratorWrapper` must NOT match.
    first_param = params[0]
    if first_param.annotation == inspect.Parameter.empty:
        return False
    origin = typing.get_origin(first_param.annotation)
    if origin is not None:
        return origin in (
            collections.abc.Iterator,
            collections.abc.Iterable,
            collections.abc.AsyncIterator,
            collections.abc.AsyncIterable,
        )
    return first_param.annotation in (
        typing.Iterator,
        typing.Iterable,
        typing.AsyncIterator,
        typing.AsyncIterable,
        collections.abc.Iterator,
        collections.abc.Iterable,
        collections.abc.AsyncIterator,
        collections.abc.AsyncIterable,
    )


def _dispatch_streaming(conn, msg, q: queue.Queue, streams, is_bidi):
    """Dispatch a streaming call (client-stream or bidi). Runs in background thread."""
    msg_id = msg["id"]
    func_name = msg.get("func")
    fn = _registry.get(func_name)

    if not fn:
        write_frame(conn, pack_error(msg_id, "NotFoundError", f"function '{func_name}' not exported"))
        streams.pop(msg_id, None)
        return

    try:
        # Create an iterator that drains the queue until stream_close or stream_end
        chunks = _stream_iterator(q, msg_id, is_bidi)
        result = fn(chunks)
        write_frame(conn, pack_response(msg_id, result))
    except Exception as e:
        error_type, message = exception_to_wire(e)
        write_frame(conn, pack_error(msg_id, error_type, message))
    finally:
        streams.pop(msg_id, None)


def _stream_iterator(q: queue.Queue, msg_id, is_bidi):
    """Iterator that yields chunks from queue until stream_close or stream_end."""
    while True:
        try:
            msg = q.get(timeout=30.0)
        except queue.Empty:
            return

        msg_type = msg.get("type")
        if msg_type == "stream_chunk":
            yield msg.get("result")
        elif msg_type == "stream_close":
            # Client-streaming: close received, stop iterating
            return
        elif msg_type == "stream_end":
            # Bidi-streaming: client done sending
            if is_bidi:
                # For bidi, don't return yet—client_close_send() was called
                # Server should keep processing
                pass
            return


def _dispatch(conn, msg):
    id_, func_name = msg["id"], msg["func"]
    args = msg.get("args")
    if args is None:
        args = []
    if func_name not in _registry:
        write_frame(
            conn, pack_error(id_, "NotFoundError", f"function '{func_name}' not exported")
        )
        return
    fn = _registry[func_name]
    try:
        if inspect.isgeneratorfunction(fn):
            # Stream: send one stream_chunk per yielded value, then stream_end.
            gen = fn(*args)
            try:
                for value in gen:
                    write_frame(conn, pack_stream_chunk(id_, value))
            except Exception as e:
                error_type, message = exception_to_wire(e)
                write_frame(conn, pack_error(id_, error_type, message))
                return
            write_frame(conn, pack_stream_end(id_))
        else:
            result = fn(*args)
            write_frame(conn, pack_response(id_, result))
    except Exception as e:
        error_type, message = exception_to_wire(e)
        write_frame(conn, pack_error(id_, error_type, message))


@atexit.register
def _cleanup():
    global _auto_listener
    if _auto_listener is not None:
        try:
            _auto_listener.close()
        except OSError:
            pass
        _auto_listener = None
