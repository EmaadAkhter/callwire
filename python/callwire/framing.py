import struct


def write_frame(sock, payload: bytes) -> None:
    length_prefix = struct.pack(">I", len(payload))
    sock.sendall(length_prefix + payload)


def read_frame(sock) -> bytes:
    length_bytes = _read_exact(sock, 4)
    length = struct.unpack(">I", length_bytes)[0]
    return _read_exact(sock, length)


def _read_exact(sock, n: int) -> bytes:
    buf = bytearray()
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise ConnectionError("socket closed mid-frame")
        buf.extend(chunk)
    return bytes(buf)
