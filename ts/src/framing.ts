import * as net from 'net';

/**
 * BufferedReader wraps a net.Socket and provides an async `readExactly(n)` 
 * method. It uses a single continuous 'data' listener to buffer all incoming
 * data, ensuring frames are never interleaved or lost.
 */
export class BufferedReader {
  private buf = Buffer.alloc(0);
  private waiters: Array<(buf: Buffer) => void> = [];
  private closed = false;

  constructor(private socket: net.Socket) {
    socket.on('data', (chunk: Buffer) => {
      this.buf = Buffer.concat([this.buf, chunk]);
      this._drainWaiters();
    });

    socket.once('close', () => {
      this.closed = true;
      // Reject all pending waiters
      for (const w of this.waiters) {
        w(Buffer.alloc(0));
      }
      this.waiters = [];
    });

    socket.once('error', () => {
      this.closed = true;
      for (const w of this.waiters) {
        w(Buffer.alloc(0));
      }
      this.waiters = [];
    });
  }

  private _drainWaiters(): void {
    while (this.waiters.length > 0) {
      // Peek at the first waiter's needed size (we can't know it here without
      // wrapping, so just wake every waiter and let them re-check)
      const waiter = this.waiters[0];
      waiter(Buffer.alloc(0)); // signal "data available"
      this.waiters.shift();
    }
  }

  async readExactly(n: number): Promise<Buffer> {
    while (this.buf.length < n) {
      if (this.closed) {
        throw new Error('Socket closed');
      }
      await new Promise<void>(resolve => {
        this.waiters.push(() => resolve());
      });
    }
    const result = this.buf.subarray(0, n);
    this.buf = this.buf.subarray(n);
    return result;
  }

  /**
   * Read one Callwire frame: [4-byte big-endian uint32 length][N bytes payload]
   */
  async readFrame(): Promise<Buffer> {
    const header = await this.readExactly(4);
    const len = header.readUInt32BE(0);
    if (len === 0) return Buffer.alloc(0);
    return this.readExactly(len);
  }
}

/**
 * Writes one Callwire frame to a socket.
 * Frame format: [4-byte big-endian uint32 length][N bytes msgpack payload]
 */
export function writeFrame(socket: net.Socket, payload: Buffer): boolean {
  const header = Buffer.allocUnsafe(4);
  header.writeUInt32BE(payload.length, 0);
  return socket.write(Buffer.concat([header, payload]));
}
