import struct
import socket
import threading
import time
from callwire.framing import write_frame, read_frame, _read_exact
from callwire.codec import pack_request, pack_response, pack_error, unpack
from callwire.errors import exception_to_wire
from callwire.server import export, serve, _dispatch

# ── Step 2: framing + codec self-test ──────────────────────────────

def test_roundtrip():
    original = pack_request(1, "predict", [1.5, 2.0])
    decoded = unpack(original)
    assert decoded["id"] == 1
    assert decoded["type"] == "request"
    assert decoded["func"] == "predict"
    assert decoded["args"] == [1.5, 2.0]
    print("[PASS] pack_request / unpack roundtrip")

    resp = pack_response(2, {"size": 42})
    decoded = unpack(resp)
    assert decoded["id"] == 2
    assert decoded["type"] == "response"
    assert decoded["result"] == {"size": 42}
    print("[PASS] pack_response / unpack roundtrip")

    err = pack_error(3, "ValueError", "inches must be positive")
    decoded = unpack(err)
    assert decoded["id"] == 3
    assert decoded["type"] == "error"
    assert decoded["error_type"] == "ValueError"
    assert decoded["message"] == "inches must be positive"
    print("[PASS] pack_error / unpack roundtrip")

    assert len(original) == len(pack_request(1, "predict", [1.5, 2.0]))
    print("[PASS] deterministic encoding")

    triple = pack_request(1, "x", [])
    assert unpack(triple)["args"] == []
    print("[PASS] empty args")


def test_framing_via_memory():
    import io

    class FakeSocket:
        def __init__(self, data):
            self.data = data
            self.pos = 0

        def sendall(self, b):
            pass

        def recv(self, n):
            chunk = self.data[self.pos : self.pos + n]
            self.pos += len(chunk)
            return chunk

    payload = pack_request(42, "foo", [1, 2, 3])
    framed = struct.pack(">I", len(payload)) + payload

    fakesock = FakeSocket(framed)
    result = read_frame(fakesock)
    assert result == payload, f"Expected {payload!r}, got {result!r}"
    print("[PASS] _read_exact reads full frame")


def test_partial_read():
    class PartialSocket:
        def __init__(self, data, split_at):
            self.data = data
            self.pos = 0
            self.split_at = split_at

        def sendall(self, b):
            pass

        def recv(self, n):
            actual_n = min(n, self.split_at)
            chunk = self.data[self.pos : self.pos + actual_n]
            self.pos += len(chunk)
            return chunk

    payload = pack_request(7, "test", ["hello"])
    framed = struct.pack(">I", len(payload)) + payload

    psock = PartialSocket(framed, split_at=3)
    result = read_frame(psock)
    assert result == payload, f"Partial read failed: {result!r} != {payload!r}"
    print("[PASS] partial reads reassembled correctly")


# ── Step 3: errors ─────────────────────────────────────────────────

def test_exported_error():
    exc_type, msg = exception_to_wire(ValueError("bad val"))
    assert exc_type == "ValueError"
    assert msg == "bad val"
    print("[PASS] registered ValueError exposed")

class _UnlistedError(Exception):
    pass

def test_internal_error():
    exc_type, msg = exception_to_wire(_UnlistedError("no file"))
    assert exc_type == "InternalError"
    assert msg == "an internal error occurred"
    print("[PASS] unlisted exception hidden as InternalError")


# ── Step 3: dispatch ───────────────────────────────────────────────

def test_dispatch_basic():
    calls = []

    @export
    def double(x):
        return x * 2

    from callwire.server import _registry
    assert "double" in _registry
    assert _registry["double"](3) == 6
    print("[PASS] export decorator registers function")


if __name__ == "__main__":
    test_roundtrip()
    test_framing_via_memory()
    test_partial_read()
    test_exported_error()
    test_internal_error()
    test_dispatch_basic()
    print("\nAll Python self-tests passed.")
