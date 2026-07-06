import socket
import threading
import time
from callwire.framing import write_frame, read_frame
from callwire.codec import pack_request, pack_response, pack_error, unpack
from callwire.server import export, serve

# ── Smoke test: real TCP dispatch ──────────────────────────────────

def test_dispatch_via_tcp():
    received = []

    @export
    def add(a, b):
        return a + b

    @export
    def fail():
        raise ValueError("something broke")

    results = {}

    def client_thread():
        time.sleep(0.1)
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.connect(("localhost", 9191))

        # test 1: success
        write_frame(sock, pack_request(1, "add", [3, 4]))
        payload = read_frame(sock)
        msg = unpack(payload)
        results["add_ok"] = (msg["type"] == "response" and msg["result"] == 7)

        # test 2: error
        write_frame(sock, pack_request(2, "fail", []))
        payload = read_frame(sock)
        msg = unpack(payload)
        results["fail_err"] = (
            msg["type"] == "error"
            and msg["error_type"] == "ValueError"
            and msg["message"] == "something broke"
        )

        # test 3: not found
        write_frame(sock, pack_request(3, "nope", []))
        payload = read_frame(sock)
        msg = unpack(payload)
        results["not_found"] = (
            msg["type"] == "error"
            and msg["error_type"] == "NotFoundError"
        )

        sock.close()

    server_thread = threading.Thread(
        target=serve,
        args=("localhost", 9191),
        daemon=True,
    )
    server_thread.start()

    ct = threading.Thread(target=client_thread, daemon=True)
    ct.start()
    ct.join(timeout=5)

    assert results.get("add_ok"), f"add_ok failed: {results}"
    assert results.get("fail_err"), f"fail_err failed: {results}"
    assert results.get("not_found"), f"not_found failed: {results}"
    print("[PASS] dispatch via TCP: success, error, and not-found paths")


if __name__ == "__main__":
    test_dispatch_via_tcp()
    print("\nAll smoke tests passed.")
