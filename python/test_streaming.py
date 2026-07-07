"""
Python streaming smoke test:
  - Python server with a generator function
  - Python client using call_stream()
  - Verifies all chunks received in order
"""
import sys
import threading
import time

sys.path.insert(0, ".")

from callwire.server import export, serve, _registry
from callwire.client import Client, CallwireError


def test_python_streaming():
    @export
    def squares(n):
        for i in range(1, n + 1):
            yield i * i

    # Start server in background thread
    t = threading.Thread(target=serve, args=("localhost", 9210), daemon=True)
    t.start()
    time.sleep(0.15)

    c = Client()
    c.connect("localhost", 9210)

    chunks = list(c.call_stream("squares", [5]))
    assert chunks == [1, 4, 9, 16, 25], f"Expected [1,4,9,16,25], got {chunks}"
    print(f"[PASS] Python streaming: squares(5) = {chunks}")

    # Verify error propagation mid-stream (generator raises)
    @export
    def error_mid_stream(n):
        for i in range(n):
            if i == 2:
                raise ValueError("bad at 2")
            yield i

    try:
        list(c.call_stream("error_mid_stream", [5]))
        assert False, "expected CallwireError"
    except CallwireError as e:
        assert e.error_type == "ValueError", f"wrong type: {e.error_type}"
        assert "bad at 2" in e.message, f"wrong msg: {e.message}"
        print(f"[PASS] Python streaming error propagation: {e}")

    # Verify empty stream works
    @export
    def empty_stream():
        return
        yield  # makes it a generator

    chunks = list(c.call_stream("empty_stream", []))
    assert chunks == [], f"Expected [], got {chunks}"
    print(f"[PASS] Python streaming empty stream: {chunks}")

    c.close()


if __name__ == "__main__":
    test_python_streaming()
    print("\nAll Python streaming tests passed.")
