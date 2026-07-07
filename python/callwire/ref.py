import threading
import atexit

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


def ref(func, host=None, port=None):
    if host is None:
        host = _AUTO_CONFIG["host"]
    if port is None:
        port = _AUTO_CONFIG["port"]

    key = (host, port)
    with _cache_lock:
        if key not in _cache:
            c = Client()
            c.connect(host, port)
            _cache[key] = c
        client = _cache[key]

    def caller(*args):
        return client.call(func, list(args))

    return caller
