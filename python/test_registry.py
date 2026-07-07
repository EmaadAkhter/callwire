import socket
import threading
import time
import unittest
import callwire

class TestRegistry(unittest.TestCase):
    def test_registry_discovery(self):
        # Dynamically allocate ports
        def get_free_port():
            s = socket.socket()
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
            s.close()
            return port

        reg_port = get_free_port()
        worker_port = get_free_port()

        # Start registry server
        reg_thread = threading.Thread(
            target=callwire.serve_registry,
            kwargs={"host": "127.0.0.1", "port": reg_port},
            daemon=True
        )
        reg_thread.start()
        time.sleep(0.1)

        # Worker logic
        @callwire.export
        def get_msg(name):
            return f"Hello {name}"

        worker_thread = threading.Thread(
            target=callwire.serve,
            kwargs={"host": "127.0.0.1", "port": worker_port},
            daemon=True
        )
        worker_thread.start()
        time.sleep(0.1)

        # Register worker with registry
        reg_client = callwire.Client()
        reg_client.connect("127.0.0.1", reg_port)
        reg_client.call("register", ["test-service", f"127.0.0.1:{worker_port}"])
        reg_client.close()

        # Discover and call
        pool = callwire.DiscoverPool(f"127.0.0.1:{reg_port}", "test-service")
        client = pool.get()
        res = client.call("get_msg", ["World"])
        self.assertEqual(res, "Hello World")
        pool.close()

if __name__ == "__main__":
    unittest.main()
