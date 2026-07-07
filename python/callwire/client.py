import queue
import socket
import threading
import time
import logging

from .framing import read_frame, write_frame
from .codec import pack_request, unpack

logger = logging.getLogger("callwire.client")


class CallwireError(Exception):
    def __init__(self, error_type: str, message: str):
        self.error_type = error_type
        self.message = message
        super().__init__(f"{error_type}: {message}")


class _Done:
    pass


DONE = _Done()


class Client:
    """
    Thread-safe callwire client.

    Basic usage::

        c = Client()
        c.connect("localhost", 9090)
        result = c.call("add", [1, 2])

    Streaming (server exports a generator)::

        for chunk in c.call_stream("count_to", [5]):
            print(chunk)

    Reconnect on drop::

        c = Client(reconnect=True)
        c.connect("localhost", 9090)
    """

    def __init__(self, reconnect: bool = False):
        self._reconnect = reconnect
        self._host = None
        self._port = None
        self.conn = None
        self._write_lock = threading.Lock()
        self._pending: dict[int, queue.Queue] = {}
        self._pending_lock = threading.Lock()
        self._next_id = 0
        self._next_id_lock = threading.Lock()
        self._reader_thread = None
        self._connected = False
        self._conn_lock = threading.Lock()

    def connect(self, host="localhost", port=9090, tls=None):
        self._host = host
        self._port = port
        self._tls = tls
        self.conn = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.conn.connect((host, port))
        if tls:
            import ssl
            ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
            if tls.get("cafile"):
                ssl_context.load_verify_locations(cafile=tls["cafile"])
            else:
                ssl_context.check_hostname = False
                ssl_context.verify_mode = ssl.CERT_NONE
            if tls.get("certfile") and tls.get("keyfile"):
                ssl_context.load_cert_chain(certfile=tls["certfile"], keyfile=tls["keyfile"])
            if tls.get("insecure", False):
                ssl_context.check_hostname = False
                ssl_context.verify_mode = ssl.CERT_NONE
            self.conn = ssl_context.wrap_socket(self.conn, server_hostname=host)
        self._connected = True
        self._reader_thread = threading.Thread(target=self._read_loop, daemon=True)
        self._reader_thread.start()

    def _close_conn(self, shutdown=False):
        """Thread-safe socket closure and connection cleanup."""
        with self._conn_lock:
            if self.conn is not None:
                if shutdown:
                    try:
                        self.conn.shutdown(socket.SHUT_RDWR)
                    except OSError:
                        pass
                try:
                    self.conn.close()
                except OSError:
                    pass
                self.conn = None

    def close(self):
        self._connected = False
        with self._pending_lock:
            pending = list(self._pending.values())
            self._pending.clear()
        for q in pending:
            q.put(DONE)
        self._close_conn(shutdown=True)
        if (
            self._reader_thread
            and self._reader_thread.is_alive()
            and self._reader_thread is not threading.current_thread()
        ):
            self._reader_thread.join(timeout=2)

    def _drain_pending(self):
        """Push a sentinel DONE into all pending queues so waiters unblock."""
        with self._pending_lock:
            for q in self._pending.values():
                q.put(DONE)
            self._pending.clear()

    def _reconnect_loop(self):
        """Attempt to reconnect with exponential back-off."""
        backoff = 0.05
        while self._connected:
            time.sleep(backoff)
            backoff = min(backoff * 2, 5.0)
            try:
                new_conn = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                new_conn.connect((self._host, self._port))
                if self._tls:
                    import ssl
                    ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                    if self._tls.get("cafile"):
                        ssl_context.load_verify_locations(cafile=self._tls["cafile"])
                    else:
                        ssl_context.check_hostname = False
                        ssl_context.verify_mode = ssl.CERT_NONE
                    if self._tls.get("certfile") and self._tls.get("keyfile"):
                        ssl_context.load_cert_chain(certfile=self._tls["certfile"], keyfile=self._tls["keyfile"])
                    if self._tls.get("insecure", False):
                        ssl_context.check_hostname = False
                        ssl_context.verify_mode = ssl.CERT_NONE
                    new_conn = ssl_context.wrap_socket(new_conn, server_hostname=self._host)
                self.conn = new_conn
                return  # success
            except OSError:
                continue

    def _read_loop(self):
        try:
            while self._connected:
                try:
                    payload = read_frame(self.conn)
                except ConnectionError:
                    if self._reconnect and self._connected:
                        self._drain_pending()
                        self._reconnect_loop()
                        continue
                    break
                try:
                    msg = unpack(payload)
                except Exception as e:
                    logger.error("Failed to decode callwire frame payload: %s", e)
                    continue
                msg_id = msg["id"]
                msg_type = msg.get("type", "")

                with self._pending_lock:
                    q = self._pending.get(msg_id)
                    # For stream_chunk we do NOT remove the entry — more chunks
                    # may arrive.  For everything else (response, error,
                    # stream_end) we pop the entry so the queue is cleaned up.
                    if q is not None and msg_type not in ("stream_chunk",):
                        del self._pending[msg_id]

                if q is not None:
                    q.put(msg)
        finally:
            self._connected = False
            self._close_conn()

    def _next_request_id(self) -> int:
        with self._next_id_lock:
            self._next_id += 1
            return self._next_id

    def call(self, func: str, args: list):
        """Make a unary (request/response) call and return the result."""
        if not self._connected:
            raise ConnectionError("not connected")
        id_ = self._next_request_id()
        q: queue.Queue = queue.Queue(maxsize=1)

        with self._pending_lock:
            self._pending[id_] = q

        payload = pack_request(id_, func, args if args is not None else [])
        with self._write_lock:
            write_frame(self.conn, payload)

        try:
            val = q.get(timeout=30.0)
        except queue.Empty:
            with self._pending_lock:
                self._pending.pop(id_, None)
            raise TimeoutError("call timed out waiting for response")

        if isinstance(val, _Done):
            raise ConnectionError("connection closed while waiting for response")

        msg = val
        if msg["type"] == "error":
            raise CallwireError(msg["error_type"], msg["message"])
        return msg["result"]

    def batch(self, calls: list):
        """
        Execute multiple calls concurrently.
        calls: list of (func_name, args)
        Returns: list of results. Raises CallwireError if any call fails.
        """
        if not self._connected:
            raise ConnectionError("not connected")

        qs = []
        payloads = []
        for func, args in calls:
            id_ = self._next_request_id()
            q = queue.Queue(maxsize=1)
            with self._pending_lock:
                self._pending[id_] = q
            qs.append((id_, q))
            payloads.append(pack_request(id_, func, args if args is not None else []))

        with self._write_lock:
            for payload in payloads:
                write_frame(self.conn, payload)

        results = []
        for id_, q in qs:
            try:
                val = q.get(timeout=30.0)
            except queue.Empty:
                with self._pending_lock:
                    self._pending.pop(id_, None)
                raise TimeoutError(f"batch call timed out waiting for response")
            if isinstance(val, _Done):
                raise ConnectionError("connection closed while waiting for response")
            if val["type"] == "error":
                raise CallwireError(val["error_type"], val["message"])
            results.append(val["result"])
        return results

    def call_stream(self, func: str, args: list, timeout: float = 30.0):
        """
        Call a streaming server function and yield each chunk as it arrives.

        Raises ``CallwireError`` if the server returns an error frame.
        Raises ``ConnectionError`` if the connection drops mid-stream.
        Raises ``TimeoutError`` if no chunk is received within `timeout` seconds.

        Usage::

            for chunk in client.call_stream("count_to", [5]):
                print(chunk)
        """
        if not self._connected:
            raise ConnectionError("not connected")
        id_ = self._next_request_id()
        # Large buffer so the reader thread doesn't stall.
        q: queue.Queue = queue.Queue(maxsize=256)

        with self._pending_lock:
            self._pending[id_] = q

        payload = pack_request(id_, func, args if args is not None else [])
        with self._write_lock:
            write_frame(self.conn, payload)

        while True:
            try:
                val = q.get(timeout=timeout)
            except queue.Empty:
                with self._pending_lock:
                    self._pending.pop(id_, None)
                raise TimeoutError("stream timed out waiting for chunk")

            if isinstance(val, _Done):
                raise ConnectionError("connection closed during stream")

            msg = val
            msg_type = msg.get("type", "")

            if msg_type == "stream_chunk":
                yield msg["result"]
            elif msg_type == "stream_end":
                return
            elif msg_type == "error":
                raise CallwireError(msg["error_type"], msg["message"])
            else:
                # Unexpected frame type — treat as end-of-stream.
                return
