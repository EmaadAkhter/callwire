import * as net from 'net';
import * as tls from 'tls';
import { BufferedReader, writeFrame } from './framing';
import { unpack, packResponse, packError, packStreamChunk, packStreamEnd, packStreamClose } from './codec';

class ClientStreamMarker {
  constructor(public handler: (chunks: AsyncIterable<unknown>) => Promise<unknown>) {}
}

class BidiStreamMarker {
  constructor(public handler: (chunks: AsyncIterable<unknown>) => AsyncIterable<unknown>) {}
}

export type HandlerFn = (args: unknown[]) => unknown | Promise<unknown> | AsyncIterable<unknown>;

export interface TlsServerOptions {
  /** PEM-encoded server certificate */
  cert: string;
  /** PEM-encoded server private key */
  key: string;
  /** PEM-encoded CA certificate for client verification (mTLS) */
  ca?: string;
  /** Request client certificate. Default: false */
  requestCert?: boolean;
  /** Reject connections without a client certificate when requestCert is true. Default: false */
  rejectUnauthorized?: boolean;
}

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
  private handlers = new Map<string, HandlerFn | ClientStreamMarker | BidiStreamMarker>();
  private netServer: net.Server | null = null;
  private tlsOptions?: TlsServerOptions;

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
   * Register a client-streaming function: client sends multiple chunks, server sends single response.
   * Handler receives an async iterable of chunks and returns the response.
   */
  exportClientStream(name: string, fn: (chunks: AsyncIterable<unknown>) => Promise<unknown>): void {
    this.handlers.set(name, new ClientStreamMarker(fn));
  }

  /**
   * Register a bidirectional-streaming function: both directions send/receive chunks concurrently.
   * Handler receives an async iterable of incoming chunks and returns an async iterable of outgoing chunks.
   */
  exportBidi(name: string, fn: (chunks: AsyncIterable<unknown>) => AsyncIterable<unknown>): void {
    this.handlers.set(name, new BidiStreamMarker(fn));
  }

  /**
   * Start listening on `host:port`. If `tlsOptions` is provided, the server
   * will use TLS (and optionally mTLS if `ca` is set).
   * Returns a Promise that resolves once the server is bound and ready.
   */
  serve(host: string, port: number, tlsOptions?: TlsServerOptions): Promise<void> {
    this.tlsOptions = tlsOptions;
    return new Promise((resolve, reject) => {
      if (tlsOptions) {
        this.netServer = tls.createServer({
          cert: tlsOptions.cert,
          key: tlsOptions.key,
          ca: tlsOptions.ca,
          requestCert: tlsOptions.requestCert ?? false,
          rejectUnauthorized: tlsOptions.rejectUnauthorized ?? false,
        }, (socket) => {
          this._handleConnection(socket as net.Socket);
        });
      } else {
        this.netServer = net.createServer((socket) => {
          this._handleConnection(socket);
        });
      }

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
    const streamInputs = new Map<number, { push: (v: unknown) => void; end: () => void; error: (e: Error) => void }>();

    const loop = async () => {
      try {
        while (true) {
          const payload = await reader.readFrame();
          const msg = unpack(payload);

          // Route streaming input frames to pending handlers
          if (msg.type === 'stream_chunk' || msg.type === 'stream_close' || msg.type === 'stream_end') {
            const input = streamInputs.get(msg.id);
            if (input) {
              if (msg.type === 'stream_chunk') {
                input.push(msg.result);
              } else if (msg.type === 'stream_close' || msg.type === 'stream_end') {
                input.end();
                streamInputs.delete(msg.id);
              }
            }
            continue;
          }

          if (msg.type !== 'request') continue;

          const func = msg.func!;
          const args = msg.args ?? [];
          const id = msg.id;
          const isBidi = msg.stream === true;

          const handlerEntry = this.handlers.get(func);
          if (!handlerEntry) {
            const errPayload = packError(id, 'NotFoundError', `function '${func}' not exported`);
            writeFrame(socket, errPayload);
            continue;
          }

          // Check if this is a streaming-input handler (client-stream or bidi)
          if (handlerEntry instanceof ClientStreamMarker || handlerEntry instanceof BidiStreamMarker) {
            // Set up input queue for this call
            const queue: unknown[] = [];
            let waiting: (() => void) | null = null;
            const inputIter: AsyncIterable<unknown> = {
              [Symbol.asyncIterator]: () => ({
                next: async () => {
                  while (queue.length === 0) {
                    await new Promise<void>(resolve => { waiting = resolve; });
                  }
                  return { value: queue.shift(), done: false };
                }
              })
            };
            const input = {
              push: (v: unknown) => { queue.push(v); waiting?.(); waiting = null; },
              end: () => { waiting?.(); waiting = null; },
              error: (e: Error) => { waiting?.(); waiting = null; }
            };
            streamInputs.set(id, input);
            this._runHandler(socket, id, handlerEntry, args, inputIter);
          } else {
            // Unary or server-streaming
            this._runHandler(socket, id, handlerEntry as HandlerFn, args);
          }
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
    handler: HandlerFn | ClientStreamMarker | BidiStreamMarker,
    args: unknown[],
    inputIter?: AsyncIterable<unknown>,
  ): Promise<void> {
    try {
      let result: unknown;

      if (handler instanceof ClientStreamMarker) {
        // Client-streaming: pass input iterable, get single response
        result = await handler.handler(inputIter!);
        writeFrame(socket, packResponse(id, result));
      } else if (handler instanceof BidiStreamMarker) {
        // Bidi-streaming: pass input iterable, get output iterable
        result = handler.handler(inputIter!);
        const iter = result as AsyncIterable<unknown>;
        for await (const chunk of iter) {
          if (socket.destroyed) return;
          writeFrame(socket, packStreamChunk(id, chunk));
        }
        if (!socket.destroyed) {
          writeFrame(socket, packStreamEnd(id));
        }
      } else {
        // Unary or server-streaming
        result = await (handler as HandlerFn)(args);
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
      }
    } catch (err: unknown) {
      const errMsg = err instanceof Error ? err.message : String(err);
      if (!socket.destroyed) {
        writeFrame(socket, packError(id, 'RuntimeError', errMsg));
      }
    }
  }
}
