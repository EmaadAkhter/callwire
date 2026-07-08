"""Python export script: exports "add" on a fixed port. init() performs
the setup and is called unconditionally at the bottom — auto-starts as
soon as the script runs (or is imported), no separate manual step."""
import callwire

MATRIX_PORT = 9102


def init():
    callwire.configure(host="0.0.0.0", port=MATRIX_PORT)

    @callwire.export
    def add(a, b):
        return a + b

    print(f"Python matrix export listening on :{MATRIX_PORT}")


init()

if __name__ == "__main__":
    import time
    while True:
        time.sleep(1)
