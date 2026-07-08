"""
callwire.orchestration
======================
Zero-configuration multi-language process orchestration.

Two modes, selected automatically via environment:

  Orchestrator mode (default):
    - Reads callwire.toml from the current working directory.
    - Starts a dynamic registry on a random OS-assigned port.
    - Spawns declared worker services as child subprocesses, injecting:
        CALLWIRE_SPAWNED=1
        CALLWIRE_REGISTRY=127.0.0.1:<port>
    - Writes child PIDs to .callwire/pids for hot-reload recovery.
    - Returns when all workers have registered (or after a timeout).

  Worker mode (CALLWIRE_SPAWNED=1):
    - Skips spawning.  Does NOT start its own children.
    - Starts the local RPC server on a random OS-assigned port.
    - Connects to the parent's registry (CALLWIRE_REGISTRY) and registers
      every locally-exported function as: func_name -> 127.0.0.1:<port>
    - Runs an orphan-detection watcher that calls os._exit(0) if the parent
      process dies unexpectedly (prevents zombie workers).
"""

from __future__ import annotations

import atexit
import os
import signal
import socket
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import List, Optional

# ── module-level state ──────────────────────────────────────────────────────

_spawned: List[subprocess.Popen] = []
_registry_thread: Optional[threading.Thread] = None
_shutdown_event = threading.Event()
_initialized = False
_init_lock = threading.Lock()


# ── helpers ─────────────────────────────────────────────────────────────────

def _free_port() -> int:
    """Ask the OS for an available TCP port."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _load_toml() -> dict:
    """Parse callwire.toml from the current working directory."""
    toml_path = Path("callwire.toml")
    if not toml_path.exists():
        return {}
    try:
        import tomllib  # Python 3.11+
    except ImportError:
        try:
            import tomli as tomllib  # backport: pip install tomli
        except ImportError:
            raise ImportError(
                "callwire.init() requires Python 3.11+ or `pip install tomli` "
                "to parse callwire.toml"
            )
    with open(toml_path, "rb") as f:
        return tomllib.load(f)


def _wait_for_port(host: str, port: int, timeout: float = 5.0) -> bool:
    """Block until a TCP port is open, or until timeout elapses."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.1):
                return True
        except OSError:
            time.sleep(0.05)
    return False


def _kill_stale_pids():
    """On startup, kill any PIDs left from a previous hot-reload cycle."""
    pid_file = Path(".callwire") / "pids"
    if not pid_file.exists():
        return
    try:
        for line in pid_file.read_text().splitlines():
            line = line.strip()
            if line:
                try:
                    os.kill(int(line), signal.SIGTERM)
                except (ProcessLookupError, ValueError):
                    pass  # already gone
    finally:
        pid_file.unlink(missing_ok=True)


def _write_pid_file():
    """Persist child PIDs so hot-reload recovery can kill them."""
    pid_dir = Path(".callwire")
    pid_dir.mkdir(exist_ok=True)
    with open(pid_dir / "pids", "w") as f:
        for p in _spawned:
            f.write(f"{p.pid}\n")


# ── worker mode ─────────────────────────────────────────────────────────────

def _init_as_worker():
    """
    Called when CALLWIRE_SPAWNED=1.
    Starts the local RPC server on a random port, then registers every
    exported function name with the parent's registry.
    """
    from .server import _registry  # the dict populated by @export
    from .client import Client

    registry_addr = os.environ.get("CALLWIRE_REGISTRY", "")
    if not registry_addr:
        print("[callwire] Worker mode: CALLWIRE_REGISTRY not set — skipping registration", file=sys.stderr)
        return

    # Pick a free port and start the RPC server in a daemon thread
    port = _free_port()
    worker_host = "127.0.0.1"

    # Import the blocking server and run it in background
    from .server import _start_listener, _auto_serve_loop, _AUTO_CONFIG
    _AUTO_CONFIG["host"] = worker_host
    _AUTO_CONFIG["port"] = port
    _AUTO_CONFIG["auto"] = False  # We will manage startup ourselves

    listener_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    listener_sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener_sock.bind((worker_host, port))
    listener_sock.listen()
    listener_sock.settimeout(1.0)

    t = threading.Thread(
        target=_auto_serve_loop,
        args=(listener_sock,),
        daemon=True,
        name="callwire-worker-server",
    )
    t.start()

    time.sleep(0.2)  # brief grace period for the socket to become ready

    # Register all exported functions with the parent registry
    reg_host, reg_port_str = registry_addr.rsplit(":", 1)
    reg_client = Client()
    reg_client.connect(reg_host, int(reg_port_str))

    worker_addr = f"{worker_host}:{port}"
    registered = []
    for func_name in list(_registry.keys()):
        if func_name in ("register", "discover"):  # skip built-in registry fns
            continue
        try:
            reg_client.call("callwire.register", [func_name, worker_addr])
            registered.append(func_name)
        except Exception as exc:
            print(f"[callwire] Warning: could not register '{func_name}': {exc}", file=sys.stderr)

    reg_client.close()
    print(f"[callwire] Worker registered {registered} → {worker_addr}", flush=True)

    # Orphan detection: exit cleanly if parent dies
    parent_pid = os.getppid()

    def _orphan_watcher():
        while not _shutdown_event.is_set():
            time.sleep(2)
            try:
                current_ppid = os.getppid()
                # On Unix, if parent dies the process is re-parented to PID 1
                if current_ppid == 1 and current_ppid != parent_pid:
                    print("[callwire] Parent process gone — worker exiting", file=sys.stderr)
                    os._exit(0)
            except Exception:
                os._exit(0)

    threading.Thread(target=_orphan_watcher, daemon=True, name="callwire-orphan-watcher").start()


# ── orchestrator mode ────────────────────────────────────────────────────────

def _init_as_orchestrator():
    """
    Called in the parent process.
    Reads callwire.toml, starts a registry, spawns declared services as
    child processes, and waits briefly for them to self-register.
    """
    global _spawned

    config = _load_toml()
    services = config.get("services", {})
    if not services:
        return  # Nothing to orchestrate

    # 1. Kill any PIDs left from a previous run (hot-reload scenario)
    _kill_stale_pids()

    # 2. Start the dynamic registry on a random port
    reg_port = _free_port()
    registry_addr = f"127.0.0.1:{reg_port}"
    _start_registry_background(reg_port)
    os.environ["CALLWIRE_REGISTRY"] = registry_addr

    is_prod = os.environ.get("CALLWIRE_ENV", "dev").lower() == "prod"

    # 3. Spawn each declared worker service
    env_base = os.environ.copy()
    env_base["CALLWIRE_SPAWNED"] = "1"
    env_base["CALLWIRE_REGISTRY"] = registry_addr

    for service_name, service_cfg in services.items():
        cmd_key = "prod_cmd" if is_prod else "dev_cmd"
        cmd = service_cfg.get(cmd_key) or service_cfg.get("dev_cmd") or service_cfg.get("prod_cmd")
        if not cmd:
            print(f"[callwire] Warning: service '{service_name}' has no command — skipping", file=sys.stderr)
            continue

        proc = subprocess.Popen(
            cmd,
            shell=True,
            env=env_base,
            # Inherit parent's stdout/stderr so logs are visible
            # start_new_session=True puts the shell AND everything it execs
            # (e.g. "cd x && ./worker") in its own process group. Without
            # this, shutdown()'s proc.terminate() only signals the shell
            # wrapper — the actual worker binary underneath is a separate
            # process that never receives SIGTERM, keeps running, and (since
            # it inherited the parent's stdout/stderr) keeps those pipes
            # open — which hangs anything reading the orchestrator's output
            # (e.g. `python3 script.py | grep ...`) even after the
            # orchestrator process itself has exited.
            start_new_session=True,
        )
        _spawned.append(proc)
        print(f"[callwire] Spawned '{service_name}' (PID {proc.pid}): {cmd}", flush=True)

    # 4. Save PIDs for hot-reload cleanup
    if _spawned:
        _write_pid_file()

    # 5. Brief wait for workers to come up and self-register
    #    (proportional to the number of workers, max 5 s)
    wait_time = min(1.5 * max(len(_spawned), 1), 5.0)
    time.sleep(wait_time)

    print(f"[callwire] Orchestrator ready — registry at {registry_addr}", flush=True)


def _start_registry_background(port: int):
    """
    Start the Callwire registry server in a background thread so it does
    not block the caller.  The registry registers 'callwire.register' and
    'callwire.discover' RPC methods on this process's _registry dict.
    """
    from .registry import RegistryServer
    from .server import _handle_connection

    reg = RegistryServer()

    # Register the registry's own methods under the 'callwire.*' namespace
    # directly into this process's server registry dict
    from .server import _registry as srv_registry
    srv_registry["callwire.register"] = reg.register
    srv_registry["callwire.discover"] = reg.discover

    def _run():
        listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        listener.bind(("127.0.0.1", port))
        listener.listen()
        listener.settimeout(1.0)
        print(f"[callwire] Registry listening on 127.0.0.1:{port}", flush=True)
        while not _shutdown_event.is_set():
            try:
                conn, _ = listener.accept()
                threading.Thread(
                    target=_handle_connection, args=(conn,), daemon=True
                ).start()
            except socket.timeout:
                continue
            except OSError:
                break
        listener.close()

    t = threading.Thread(target=_run, daemon=True, name="callwire-registry")
    t.start()


# ── public API ───────────────────────────────────────────────────────────────

def init():
    """
    Initialize Callwire orchestration.

    Call this once at application startup — in a FastAPI lifespan, an
    Express bootstrap function, a Go ``main()``, or a ``callwire run`` CLI.

    * If ``CALLWIRE_SPAWNED=1`` the process is a worker: it registers its
      exported functions with the parent registry and starts an orphan watcher.
    * Otherwise it is the orchestrator: it reads ``callwire.toml``, starts
      a registry, and spawns declared worker services as child processes.
    """
    global _initialized
    with _init_lock:
        if _initialized:
            return
        _initialized = True

    if os.environ.get("CALLWIRE_SPAWNED") == "1":
        _init_as_worker()
    else:
        _init_as_orchestrator()


async def async_init():
    """Async-compatible wrapper for use in FastAPI ``lifespan`` or similar."""
    import asyncio
    loop = asyncio.get_event_loop()
    await loop.run_in_executor(None, init)


def shutdown():
    """
    Terminate all spawned worker subprocesses and stop the registry.
    Safe to call multiple times.
    """
    _shutdown_event.set()
    for proc in list(_spawned):
        try:
            # Signal the whole process group (shell wrapper + whatever it
            # exec'd), not just the shell — see the start_new_session
            # comment in _init_as_orchestrator for why this matters.
            os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
            proc.wait(timeout=3)
        except Exception:
            try:
                os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
            except Exception:
                try:
                    proc.kill()
                except Exception:
                    pass
    _spawned.clear()
    try:
        Path(".callwire/pids").unlink(missing_ok=True)
    except Exception:
        pass


async def async_shutdown():
    """Async-compatible wrapper for use in FastAPI ``lifespan`` or similar."""
    import asyncio
    loop = asyncio.get_event_loop()
    await loop.run_in_executor(None, shutdown)


# Register shutdown on normal interpreter exit
atexit.register(shutdown)
