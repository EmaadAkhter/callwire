"""Python export script: exports "add" on a fixed port. init() performs
the setup and is called unconditionally at the bottom.

Calls callwire.serve() explicitly rather than relying on @export's
auto-serve — auto-serve is gated on CALLWIRE_SPAWNED (unset = auto-start,
"1" = don't), and the orchestrator (callwire.toml + init()) sets
CALLWIRE_SPAWNED=1 on every spawned worker for its own registry-worker
bookkeeping. Under the orchestrator, auto-serve would silently no-op and
this server would never bind. configure(auto=False) forces auto-serve off
unconditionally (standalone or orchestrated) so the explicit serve() call
below is always the one true listener, and blocks the process (this is the
long-running server, no separate "keep alive" loop needed)."""
import callwire

MATRIX_PORT = 9102


def init():
    callwire.configure(host="0.0.0.0", port=MATRIX_PORT, auto=False)

    @callwire.export
    def add(a, b):
        return a + b

    print(f"Python matrix export listening on :{MATRIX_PORT}")


init()
callwire.serve(host="0.0.0.0", port=MATRIX_PORT)
