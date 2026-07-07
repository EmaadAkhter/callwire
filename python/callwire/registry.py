import threading
from typing import List, Dict
from .server import export, serve
from .client import Client

class RegistryServer:
    def __init__(self):
        self.mu = threading.Lock()
        self.services: Dict[str, List[str]] = {}

    def register(self, service_name: str, addr: str) -> None:
        with self.mu:
            addrs = self.services.setdefault(service_name, [])
            if addr not in addrs:
                addrs.append(addr)

    def discover(self, service_name: str) -> List[str]:
        with self.mu:
            addrs = self.services.get(service_name)
            if not addrs:
                raise ValueError(f"service not found: {service_name}")
            return addrs

import socket
from .server import export, _handle_connection

def serve_registry(host: str = "localhost", port: int = 9090) -> None:
    reg = RegistryServer()
    export(reg.register)
    export(reg.discover)

    listener = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind((host, port))
    listener.listen()

    while True:
        try:
            conn, _ = listener.accept()
            t = threading.Thread(target=_handle_connection, args=(conn,), daemon=True)
            t.start()
        except OSError:
            break

class DiscoverPool:
    def __init__(self, registry_addr: str, service_name: str):
        self.registry_addr = registry_addr
        self.service_name = service_name
        self.mu = threading.Lock()
        self.clients: List[Client] = []
        self.addrs: List[str] = []
        self.refresh()

    def refresh(self) -> None:
        with self.mu:
            host, port_str = self.registry_addr.split(":")
            reg_client = Client()
            reg_client.connect(host, int(port_str))
            try:
                addrs = reg_client.call("discover", [self.service_name])
            finally:
                reg_client.close()

            # Close old clients
            for c in self.clients:
                c.close()
            self.clients.clear()
            self.addrs.clear()

            for addr in addrs:
                try:
                    h, p = addr.split(":")
                    c = Client()
                    c.connect(h, int(p))
                    self.clients.append(c)
                    self.addrs.append(addr)
                except Exception:
                    pass

            if not self.clients:
                raise RuntimeError(f"no healthy service instances found for {self.service_name}")

    def get(self) -> Client:
        with self.mu:
            if not self.clients:
                raise RuntimeError("no clients available in pool")
            return self.clients[0]

    def close(self) -> None:
        with self.mu:
            for c in self.clients:
                c.close()
            self.clients.clear()
