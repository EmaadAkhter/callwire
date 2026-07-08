"""Python import script: spawns every language's export server itself via
callwire.init() (reads ../callwire.toml, the 9-service aggregator), then
calls "add"(10,20) on all 8 OTHER languages — one command, no separate
terminal needed to start servers first."""
import os
import sys
import time
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
    # The aggregator callwire.toml lives one directory up (examples/matrix/),
    # not in this script's own folder — orchestrator reads callwire.toml
    # from the current working directory, so switch to it before init().
    matrix_root = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
    os.chdir(matrix_root)
    callwire.init()  # spawns all 9 export servers as child processes
    time.sleep(2.5)  # give slower-starting workers (JVM, tsx) time to bind


def call_all():
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
call_all()
callwire.shutdown()

# Force exit — some spawned children (esp. subprocesses launched via
# shell=True, or lingering client reader threads) can outlive
# shutdown()'s terminate()/wait() cleanup and keep the interpreter
# alive past its expected exit. All real work is done by this point.
sys.stdout.flush()
os._exit(0)
