import sys
import threading
import time

sys.path.insert(0, ".")

from callwire import export, serve, Client, CallwireError

def test_python_client_batch():
    @export
    def py_batch_double(x):
        return x * 2

    @export
    def py_batch_len(s):
        return len(s)

    @export
    def py_batch_fail():
        raise ValueError("boom")

    def server():
        serve("localhost", 19810)

    t = threading.Thread(target=server, daemon=True)
    t.start()
    time.sleep(0.2)

    c = Client()
    c.connect("localhost", 19810)

    # 1. Success batch
    results = c.batch([
        ("py_batch_double", [21]),
        ("py_batch_len", ["hello"]),
        ("py_batch_double", [10]),
    ])
    assert results == [42, 5, 20], f"expected [42, 5, 20], got {results}"
    print("[PASS] Python batch: success path")

    # 2. Failure batch (raises error from first failing call)
    try:
        c.batch([
            ("py_batch_double", [10]),
            ("py_batch_fail", []),
        ])
        assert False, "expected error"
    except CallwireError as e:
        assert e.error_type == "ValueError"
        assert e.message == "boom"
        print("[PASS] Python batch: error path")

    # 3. Not found batch
    try:
        c.batch([
            ("does_not_exist", []),
        ])
        assert False, "expected error"
    except CallwireError as e:
        assert e.error_type == "NotFoundError"
        print("[PASS] Python batch: not-found path")

    c.close()

if __name__ == "__main__":
    test_python_client_batch()
    print("\nAll Python batch tests passed.")
