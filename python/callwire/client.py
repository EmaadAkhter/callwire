import queue
import socket
import threading
import time
import logging

from .framing import read_frame, write_frame
from .codec import pack_request, pack_stream_close, pack_stream_end, pack_bidi_request, unpack

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
        
        # Routing client extensions
        self._is_registry = False
        self._worker_clients = {}
        self._route_cache = {}

    def connect(self, host="localhost", port=9090, tls=None, is_registry=False):
        self._host = host
        self._port = port
        self._tls = tls
        self._is_registry = is_registry
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
        # Close any dynamically connected workers
        for wc in list(self._worker_clients.values()):
            try:
                wc.close()
            except Exception:
                pass
        self._worker_clients.clear()

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
                with self._conn_lock:
                    if not self._connected:
                        new_conn.close()
                        return
                    self.conn = new_conn
                return  # success
            except OSError:
                continue

    def _read_loop(self):
        try:
            while self._connected:
                try:
                    with self._conn_lock:
                        conn = self.conn
                    payload = read_frame(conn)
                except (OSError, AttributeError, TypeError):
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

    def _resolve_worker(self, func: str):
        if not self._is_registry:
            return None
        
        # Don't route internal registry calls
        if func.startswith("callwire."):
            return None

        addr = self._route_cache.get(func)
        if not addr:
            # Query the registry for this function's location
            addrs = self.call("callwire.discover", [func])
            if not addrs or not isinstance(addrs, list):
                raise CallwireError("NotFoundError", f"function '{func}' not found in registry")
            addr = addrs[0]
            self._route_cache[func] = addr

        if addr not in self._worker_clients:
            host, port_str = addr.split(":")
            wc = Client(reconnect=self._reconnect)
            wc.connect(host, int(port_str), tls=self._tls)
            self._worker_clients[addr] = wc
        
        return self._worker_clients[addr]

    def call(self, func: str, args: list):
        """Make a unary (request/response) call and return the result."""
        worker = self._resolve_worker(func)
        if worker:
            return worker.call(func, args)

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
        return msg.get("result")

    def batch(self, calls: list):
        """
        Execute multiple calls concurrently.
        calls: list of (func_name, args)
        Returns: list of results. Raises CallwireError if any call fails.
        """
        if self._is_registry:
            # Group calls by target worker
            worker_calls = {}
            results = [None] * len(calls)
            for idx, (func, args) in enumerate(calls):
                worker = self._resolve_worker(func)
                if not worker:
                    raise ConnectionError("routing resolved to empty worker")
                worker_calls.setdefault(worker, []).append((idx, func, args))

            for worker, items in worker_calls.items():
                batch_payload = [(func, args) for _, func, args in items]
                batch_res = worker.batch(batch_payload)
                for (original_idx, _, _), val in zip(items, batch_res):
                    results[original_idx] = val
            return results

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
            results.append(val.get("result"))
        return results

    def call_stream(self, func: str, args: list, timeout: float = 30.0):
        """
        Call a streaming server function and yield each chunk as it arrives.
        """
        worker = self._resolve_worker(func)
        if worker:
            yield from worker.call_stream(func, args, timeout=timeout)
            return

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
                yield msg.get("result")
            elif msg_type == "stream_end":
                return
            elif msg_type == "error":
                raise CallwireError(msg["error_type"], msg["message"])
            else:
                # Unexpected frame type — treat as end-of-stream.
                return

    def ref(self, func: str):
        """Bind a remote function once and return a reusable callable function."""
        return lambda *args: self.call(func, list(args))

    def ref_stream(self, func: str):
        """Bind a remote streaming function once and return a reusable generator-maker."""
        return lambda *args: self.call_stream(func, list(args))

    def export_stream(self, func: str):
        """
        Begin client-streaming: client sends multiple chunks, server sends single response.
        Returns ExportStream context manager.

        Usage:
            stream = client.export_stream("process_batch")
            with stream:
                stream.send(item1)
                stream.send(item2)
                result = stream.recv()
        """
        return ExportStream(self, func)

    def bidi_stream(self, func: str):
        """
        Begin bidirectional-streaming: both sides send/recv chunks concurrently.
        Returns BidiStream context manager.

        Usage:
            stream = client.bidi_stream("chat")
            with stream:
                stream.send(msg1)
                msg2 = stream.recv()
        """
        return BidiStream(self, func)


class ExportStream:
    """Client-streaming: send multiple chunks, receive single response."""

    def __init__(self, client: "Client", func: str):
        self.client = client
        self.func = func
        self.id = None
        self.q = None

    def __enter__(self):
        if not self.client._connected:
            raise ConnectionError("not connected")
        self.id = self.client._next_request_id()
        self.q = queue.Queue(maxsize=256)
        with self.client._pending_lock:
            self.client._pending[self.id] = self.q
        # Send initial request
        payload = pack_request(self.id, self.func, [])
        with self.client._write_lock:
            write_frame(self.client.conn, payload)
        return self

    def __exit__(self, *args):
        pass

    def send(self, chunk):
        """Send a chunk to the server."""
        if not self.id:
            raise RuntimeError("stream not open")
        payload = pack_stream_chunk(self.id, chunk)
        with self.client._write_lock:
            write_frame(self.client.conn, payload)

    def close_and_recv(self, timeout: float = 30.0):
        """Signal end of chunks and wait for server response."""
        if not self.id:
            raise RuntimeError("stream not open")
        # Send stream_close
        payload = pack_stream_close(self.id)
        with self.client._write_lock:
            write_frame(self.client.conn, payload)
        # Wait for response
        try:
            msg = self.q.get(timeout=timeout)
        except queue.Empty:
            with self.client._pending_lock:
                self.client._pending.pop(self.id, None)
            raise TimeoutError("stream timed out waiting for response")

        if isinstance(msg, _Done):
            raise ConnectionError("connection closed during stream")

        if msg.get("type") == "error":
            raise CallwireError(msg["error_type"], msg["message"])

        return msg.get("result")


class BidiStream:
    """Bidirectional-streaming: concurrent send/recv both directions."""

    def __init__(self, client: "Client", func: str):
        self.client = client
        self.func = func
        self.id = None
        self.q = None
        self.closed_send = False

    def __enter__(self):
        if not self.client._connected:
            raise ConnectionError("not connected")
        self.id = self.client._next_request_id()
        self.q = queue.Queue(maxsize=256)
        with self.client._pending_lock:
            self.client._pending[self.id] = self.q
        # Send initial bidi request (stream=true)
        payload = pack_bidi_request(self.id, self.func, [])
        with self.client._write_lock:
            write_frame(self.client.conn, payload)
        return self

    def __exit__(self, *args):
        pass

    def send(self, chunk):
        """Send a chunk to the server."""
        if not self.id or self.closed_send:
            raise RuntimeError("stream not open or already closed")
        payload = pack_stream_chunk(self.id, chunk)
        with self.client._write_lock:
            write_frame(self.client.conn, payload)

    def close_send(self):
        """Signal end of sending. Must still call recv() until it returns None."""
        if not self.id:
            raise RuntimeError("stream not open")
        self.closed_send = True
        payload = pack_stream_end(self.id)
        with self.client._write_lock:
            write_frame(self.client.conn, payload)

    def recv(self, timeout: float = 30.0):
        """Receive next chunk from server. Returns None when stream_end received."""
        if not self.id:
            raise RuntimeError("stream not open")
        try:
            msg = self.q.get(timeout=timeout)
        except queue.Empty:
            raise TimeoutError("stream timed out waiting for chunk")

        if isinstance(msg, _Done):
            raise ConnectionError("connection closed during stream")

        msg_type = msg.get("type", "")
        if msg_type == "stream_chunk":
            return msg.get("result")
        elif msg_type == "stream_end":
            return None
        elif msg_type == "error":
            raise CallwireError(msg["error_type"], msg["message"])
        else:
            return None

