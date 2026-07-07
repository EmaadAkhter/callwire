import atexit
import os
import socket
import threading

from .framing import read_frame, write_frame
from .codec import pack_response, pack_error, unpack
from .errors import exception_to_wire

_registry = {}

_AUTO_CONFIG = {
    "host": os.environ.get("CALLWIRE_HOST", "localhost"),
    "port": int(os.environ.get("CALLWIRE_PORT", "9090")),
    "auto": os.environ.get("CALLWIRE_AUTO", "1") != "0",
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


def serve(host="localhost", port=9090):
    global _auto_listener, _auto_started
    if _auto_listener is not None:
        if host == _AUTO_CONFIG["host"] and port == _AUTO_CONFIG["port"]:
            return
        _auto_listener.close()
        _auto_listener = None
        _auto_started = False
    _start_listener(host, port)
    while True:
        try:
            conn, _ = _auto_listener.accept()
            threading.Thread(target=_handle_connection, args=(conn,), daemon=True).start()
        except OSError:
            break


def _start_listener(host, port):
    global _auto_listener, _auto_started
    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind((host, port))
    listener.listen()
    _auto_listener = listener
    _auto_started = True
    print(f"callwire: listening on {host}:{port}")


def _handle_connection(conn):
    try:
        while True:
            payload = read_frame(conn)
            msg = unpack(payload)
            _dispatch(conn, msg)
    except ConnectionError:
        pass
    finally:
        conn.close()


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
    try:
        result = _registry[func_name](*args)
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
