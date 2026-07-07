import os
import socket
import ssl
import subprocess
import threading
import time
import unittest
import tempfile
import callwire

class TestPythonTLS(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp_dir = tempfile.TemporaryDirectory()
        cls.dir_path = cls.temp_dir.name

        cls.key_path = os.path.join(cls.dir_path, "server.key")
        cls.cert_path = os.path.join(cls.dir_path, "server.crt")

        # Generate a self-signed certificate using openssl
        cmd = [
            "openssl", "req", "-x509", "-newkey", "rsa:2048",
            "-keyout", cls.key_path, "-out", cls.cert_path,
            "-days", "1", "-nodes",
            "-subj", "/CN=localhost"
        ]
        subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)

    @classmethod
    def tearDownClass(cls):
        cls.temp_dir.cleanup()

    def test_one_way_tls(self):
        # Dynamically allocate port
        s = socket.socket()
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
        s.close()

        @callwire.export
        def tls_add(a, b):
            return a + b

        # Start TLS server in background thread
        server_tls = {
            "certfile": self.cert_path,
            "keyfile": self.key_path,
        }
        server_thread = threading.Thread(
            target=callwire.serve,
            kwargs={"host": "localhost", "port": port, "tls": server_tls},
            daemon=True
        )
        server_thread.start()
        time.sleep(0.1)

        # Connect with client verifying certificate via cafile
        client = callwire.Client()
        client.connect(
            host="localhost",
            port=port,
            tls={
                "cafile": self.cert_path,
                "insecure": False
            }
        )

        res = client.call("tls_add", [15, 25])
        self.assertEqual(res, 40)
        client.close()

    def test_mtls(self):
        s = socket.socket()
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
        s.close()

        @callwire.export
        def mtls_greet(name):
            return f"Hello {name}"

        # Start mTLS server requiring client certificate
        server_tls = {
            "certfile": self.cert_path,
            "keyfile": self.key_path,
            "cafile": self.cert_path, # Trust client certs signed by this CA
        }
        server_thread = threading.Thread(
            target=callwire.serve,
            kwargs={"host": "localhost", "port": port, "tls": server_tls},
            daemon=True
        )
        server_thread.start()
        time.sleep(0.1)

        # Connect client presenting cert/key for mTLS
        client = callwire.Client()
        client.connect(
            host="localhost",
            port=port,
            tls={
                "cafile": self.cert_path,
                "certfile": self.cert_path,
                "keyfile": self.key_path,
                "insecure": False
            }
        )

        res = client.call("mtls_greet", ["Secure World"])
        self.assertEqual(res, "Hello Secure World")
        client.close()

if __name__ == "__main__":
    unittest.main()
