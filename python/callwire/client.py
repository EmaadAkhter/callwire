import queue
import socket
import threading

from .framing import read_frame, write_frame
from .codec import pack_request, unpack


class CallwireError(Exception):
    def __init__(self, error_type: str, message: str):
        self.error_type = error_type
        self.message = message
        super().__init__(f"{error_type}: {message}")


class _Done:
    pass


DONE = _Done()


class Client:
    def __init__(self):
        self.conn = None
        self._write_lock = threading.Lock()
        self._pending = {}
        self._pending_lock = threading.Lock()
        self._next_id = 0
        self._reader_thread = None
        self._connected = False

    def connect(self, host="localhost", port=9090):
        self.conn = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.conn.connect((host, port))
        self._connected = True
        self._reader_thread = threading.Thread(target=self._read_loop, daemon=True)
        self._reader_thread.start()

    def close(self):
        self._connected = False
        self._pending_lock.acquire()
        pending = list(self._pending.values())
        self._pending.clear()
        self._pending_lock.release()
        for q in pending:
            q.put(DONE)
        if self.conn:
            try:
                self.conn.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            self.conn.close()
            self.conn = None
        if (
            self._reader_thread
            and self._reader_thread.is_alive()
            and self._reader_thread is not threading.current_thread()
        ):
            self._reader_thread.join(timeout=2)

    def _read_loop(self):
        try:
            while self._connected:
                try:
                    payload = read_frame(self.conn)
                except ConnectionError:
                    break
                try:
                    msg = unpack(payload)
                except Exception:
                    continue
                self._pending_lock.acquire()
                q = self._pending.pop(msg["id"], None)
                self._pending_lock.release()
                if q is not None:
                    q.put(msg)
        finally:
            self._connected = False
            if self.conn:
                try:
                    self.conn.close()
                except OSError:
                    pass
                self.conn = None

    def call(self, func: str, args: list):
        if not self._connected:
            raise ConnectionError("not connected")
        self._next_id += 1
        id_ = self._next_id
        q = queue.Queue(maxsize=1)

        self._pending_lock.acquire()
        self._pending[id_] = q
        self._pending_lock.release()

        payload = pack_request(id_, func, args if args is not None else [])

        with self._write_lock:
            write_frame(self.conn, payload)

        val = q.get()

        if isinstance(val, _Done):
            raise ConnectionError("connection closed while waiting for response")

        msg = val
        if msg["type"] == "error":
            raise CallwireError(msg["error_type"], msg["message"])
        return msg["result"]
