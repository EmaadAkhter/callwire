"""Python import script: calls "add"(10,20) on every OTHER language's matrix
export server (best-effort — SKIP if a port isn't reachable)."""
import callwire

TARGETS = {
    "go": 9101,
    "rust": 9103,
    "ts": 9104,
    "java": 9105,
    "c": 9106,
    "cpp": 9107,
    "swift": 9108,
    "cobol": 9109,
}


def init():
    for name, port in TARGETS.items():
        client = callwire.Client()
        try:
            client.connect("127.0.0.1", port)
        except Exception as e:
            print(f"{name:8s} SKIP (not running: {e})")
            continue
        try:
            result = client.call("add", [10, 20])
            print(f"{name:8s} OK  add(10,20) = {result}")
        except Exception as e:
            print(f"{name:8s} SKIP (call failed: {e})")
        finally:
            client.close()


init()
