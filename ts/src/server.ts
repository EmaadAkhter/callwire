import * as net from 'net';
import { BufferedReader, writeFrame } from './framing';
import { unpack, packResponse, packError, packStreamChunk, packStreamEnd } from './codec';

export type HandlerFn = (args: unknown[]) => unknown | Promise<unknown> | AsyncIterable<unknown>;

/**
 * Callwire TypeScript Server.
 *
 * @example
 * ```ts
 * const server = new Server();
 *
 * server.export('add', ([a, b]) => (a as number) + (b as number));
 * server.export('greet', ([name]) => `Hello, ${name}!`);
 *
 * await server.serve('0.0.0.0', 9090);
 * console.log('Callwire server running on :9090');
 * ```
 */
export class Server {
  private handlers = new Map<string, HandlerFn>();
  private netServer: net.Server | null = null;

  /**
   * Register a function as a callable RPC endpoint.
   * The handler receives an array of positional arguments.
   *
   * - Return a value for unary (request/response) calls.
   * - Return an `AsyncGenerator` or any `AsyncIterable` for streaming calls.
   */
  export(name: string, fn: HandlerFn): void {
    this.handlers.set(name, fn);
  }

  /**
   * Start listening on `host:port`. Returns a Promise that resolves once the
   * server is bound and ready to accept connections.
   */
  serve(host: string, port: number): Promise<void> {
    return new Promise((resolve, reject) => {
      this.netServer = net.createServer((socket) => {
        this._handleConnection(socket);
      });

      this.netServer.once('error', reject);
      this.netServer.listen(port, host, () => resolve());
    });
  }

  /**
   * Stop the server and close all active connections.
   */
  close(): Promise<void> {
    return new Promise((resolve) => {
      if (!this.netServer) return resolve();
      this.netServer.close(() => resolve());
    });
  }

  /** Address the server is listening on (available after serve()). */
  address(): net.AddressInfo | null {
    const addr = this.netServer?.address();
    return typeof addr === 'object' ? addr : null;
  }

  private _handleConnection(socket: net.Socket): void {
    const reader = new BufferedReader(socket);

    const loop = async () => {
      try {
        while (true) {
          const payload = await reader.readFrame();
          const msg = unpack(payload);

          if (msg.type !== 'request') continue;

          const func = msg.func!;
          const args = msg.args ?? [];
          const id = msg.id;

          const handler = this.handlers.get(func);
          if (!handler) {
            const errPayload = packError(id, 'NotFoundError', `function '${func}' not exported`);
            writeFrame(socket, errPayload);
            continue;
          }

          // Run handler — don't await to support concurrent requests
          this._runHandler(socket, id, handler, args);
        }
      } catch {
        socket.destroy();
      }
    };

    loop();
  }

  private async _runHandler(
    socket: net.Socket,
    id: number,
    handler: HandlerFn,
    args: unknown[],
  ): Promise<void> {
    try {
      const result = await handler(args);

      // Check if the result is an async iterable (streaming)
      if (result !== null && typeof result === 'object' && Symbol.asyncIterator in (result as object)) {
        const iter = result as AsyncIterable<unknown>;
        for await (const chunk of iter) {
          if (socket.destroyed) return;
          writeFrame(socket, packStreamChunk(id, chunk));
        }
        if (!socket.destroyed) {
          writeFrame(socket, packStreamEnd(id));
        }
      } else {
        writeFrame(socket, packResponse(id, result));
      }
    } catch (err: unknown) {
      const errMsg = err instanceof Error ? err.message : String(err);
      if (!socket.destroyed) {
        writeFrame(socket, packError(id, 'RuntimeError', errMsg));
      }
    }
  }
}
