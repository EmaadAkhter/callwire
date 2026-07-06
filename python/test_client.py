import subprocess
import sys
import threading
import time

sys.path.insert(0, ".")

from callwire import export, serve, Client, CallwireError


def test_python_client_to_python_server():
    calls = []

    @export
    def greet(name):
        return f"hi {name}"

    @export
    def fail():
        raise ValueError("nooo")

    def server():
        serve("localhost", 9200)

    t = threading.Thread(target=server, daemon=True)
    t.start()
    time.sleep(0.2)

    c = Client()
    c.connect("localhost", 9200)

    result = c.call("greet", ["world"])
    assert result == "hi world", f"got {result!r}"
    print("[PASS] Python client → Python server: greet('world') =", result)

    try:
        c.call("fail", [])
        assert False, "expected error"
    except CallwireError as e:
        assert e.error_type == "ValueError"
        assert e.message == "nooo"
        print("[PASS] Python client → Python server: error path:", e)

    try:
        c.call("nope", [])
        assert False, "expected error"
    except CallwireError as e:
        assert e.error_type == "NotFoundError"
        print("[PASS] Python client → Python server: not-found path:", e)

    c.close()


if __name__ == "__main__":
    test_python_client_to_python_server()
    print("\nAll Python client tests passed.")
