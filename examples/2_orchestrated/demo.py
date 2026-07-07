"""
Callwire Orchestrated Demo
==========================
Run ONE file. Callwire handles the rest.

This demo reads callwire.toml from the project root, spawns Go and Rust workers
in the background, waits for them to register themselves, then calls their
functions — without you ever knowing what ports they're on.

Usage (from the project root):
    python examples/2_orchestrated/demo.py

What happens under the hood:
    1. callwire.init() reads callwire.toml
    2. Starts a dynamic registry on a random OS port (e.g. :49200)
    3. Spawns 'go-worker' and 'rust-worker' child processes with:
           CALLWIRE_SPAWNED=1
           CALLWIRE_REGISTRY=:49200
    4. Each worker binds its own random port and self-registers its functions
    5. This demo connects to :49200 and routes calls transparently
    6. All calls — add(), multiply(), greet(), predict(), embed() — just work

Also works inside FastAPI / async contexts:
    @asynccontextmanager
    async def lifespan(app: FastAPI):
        await callwire.async_init()
        yield
        await callwire.async_shutdown()
"""

import sys
import os

# Use the local callwire package from the repo
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "python"))

import callwire


def main():
    print("=" * 60)
    print("  Callwire Orchestrated Demo")
    print("  Run ONE file. Everything else is automatic.")
    print("=" * 60)
    print()

    # ── This is the only call you need ──────────────────────────────
    callwire.init()
    # ────────────────────────────────────────────────────────────────

    registry_addr = os.environ.get("CALLWIRE_REGISTRY")
    if not registry_addr:
        print("[demo] No registry found — is callwire.toml present in the project root?")
        sys.exit(1)

    host, port_str = registry_addr.split(":")
    print(f"[demo] Registry running at {host}:{port_str} — connecting routing client...")
    print()

    client = callwire.Client()
    client.connect(host, int(port_str), is_registry=True)

    # ── Go worker functions ──────────────────────────────────────────
    print("── Calling Go worker functions ─────────────────────────────")

    try:
        result = client.call("add", [15, 27])
        print(f"  add(15, 27)           = {result}")
        assert result == 42
    except Exception as e:
        print(f"  add() failed: {e}")

    try:
        result = client.call("multiply", [6, 7])
        print(f"  multiply(6, 7)        = {result}")
        assert result == 42
    except Exception as e:
        print(f"  multiply() failed: {e}")

    try:
        result = client.call("greet", ["Orchestrator"])
        print(f"  greet('Orchestrator') = {result!r}")
    except Exception as e:
        print(f"  greet() failed: {e}")

    # ── Rust worker functions ────────────────────────────────────────
    print()
    print("── Calling Rust worker functions ───────────────────────────")

    try:
        result = client.call("predict", ["some_input_data"])
        print(f"  predict('some_input_data') = {result!r}")
    except Exception as e:
        print(f"  predict() failed: {e}")

    try:
        result = client.call("embed", ["embedding_query"])
        print(f"  embed('embedding_query')   = {result}")
    except Exception as e:
        print(f"  embed() failed: {e}")

    print()
    print("✅ Demo complete — all calls routed transparently: Python → Go & Rust")
    print()

    client.close()
    callwire.shutdown()


if __name__ == "__main__":
    main()
