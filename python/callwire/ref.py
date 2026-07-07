import threading
import atexit
import os

from .client import Client
from .server import _AUTO_CONFIG

_cache = {}
_cache_lock = threading.Lock()


def _cleanup_cache():
    with _cache_lock:
        for client in _cache.values():
            try:
                client.close()
            except Exception:
                pass
        _cache.clear()

atexit.register(_cleanup_cache)


def _get_default_client(host=None, port=None):
    if host is None:
        host = os.environ.get("CALLWIRE_HOST", _AUTO_CONFIG["host"])
    if port is None:
        port = int(os.environ.get("CALLWIRE_PORT", _AUTO_CONFIG["port"]))

    registry = os.environ.get("CALLWIRE_REGISTRY")
    key = (host, port, registry)
    
    with _cache_lock:
        if key not in _cache:
            c = Client()
            if registry:
                r_host, r_port_str = registry.rsplit(":", 1)
                c.connect(r_host, int(r_port_str), is_registry=True)
            else:
                c.connect(host, port)
            _cache[key] = c
        return _cache[key]


def ref(func, host=None, port=None, client=None):
    """Bind a remote function once and return a reusable local function."""
    c = client if client is not None else _get_default_client(host, port)
    return lambda *args: c.call(func, list(args))


def ref_stream(func, host=None, port=None, client=None):
    """Bind a remote streaming function once and return a reusable generator-maker."""
    c = client if client is not None else _get_default_client(host, port)
    return lambda *args: c.call_stream(func, list(args))

