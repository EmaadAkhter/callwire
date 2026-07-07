from .server import export, serve, configure
from .client import Client, CallwireError
from .ref import ref, ref_stream
from .registry import serve_registry, DiscoverPool
from .orchestration import init, async_init, shutdown, async_shutdown

__version__ = "2.0.0"
__all__ = [
    "export", "serve", "configure",
    "Client", "CallwireError",
    "ref", "ref_stream",
    "serve_registry", "DiscoverPool",
    "init", "async_init", "shutdown", "async_shutdown",
    "__version__",
]

def __getattr__(name: str):
    # If the user tries to import an arbitrary name that isn't one of our static exports,
    # we dynamically import it as a remote callable function.
    if name in __all__:
        raise AttributeError(f"module '{__name__}' has no attribute '{name}'")
    return ref(name)

