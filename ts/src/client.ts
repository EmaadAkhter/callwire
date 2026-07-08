import * as net from 'net';
import * as tls from 'tls';
import { EventEmitter } from 'events';
import { BufferedReader, writeFrame } from './framing';
import { packRequest, unpack, WireMessage } from './codec';

let defaultClient: Client | null = null;

function getDefaultClient(): Client {
  if (!defaultClient) {
    defaultClient = new Client();
    const host = process.env.CALLWIRE_HOST || '127.0.0.1';
    const port = process.env.CALLWIRE_PORT ? parseInt(process.env.CALLWIRE_PORT, 10) : 9090;
    
    // Connect synchronously in background, letting connection resolve or reject on actual calls
    if (process.env.CALLWIRE_REGISTRY) {
      const [rHost, rPortStr] = process.env.CALLWIRE_REGISTRY.split(':');
      defaultClient.connectRegistry(rHost, parseInt(rPortStr, 10)).catch(() => {});
    } else {
      defaultClient.connect(host, port).catch(() => {});
    }
  }
  return defaultClient;
}

// Clean up default client on exit
process.on('exit', () => {
  if (defaultClient) {
    defaultClient.close();
    defaultClient = null;
  }
});

/**
 * Bind a remote function to a reusable local function using the default client.
 */
export function ref<T = unknown>(func: string, client?: Client): (...args: unknown[]) => Promise<T> {
  const c = client || getDefaultClient();
  return (...args: unknown[]) => c.call<T>(func, args);
}

/**
 * Bind a remote streaming function to a reusable generator using the default client.
 */
export function refStream<T = unknown>(func: string, client?: Client): (...args: unknown[]) => AsyncGenerator<T> {
  const c = client || getDefaultClient();
  return (...args: unknown[]) => c.callStream<T>(func, args);
}

/**
 * A proxy object that dynamically wraps any property access as a remote unary function call.
 * 
 * @example
 * ```ts
 * import { remote } from 'callwire';
 * const result = await remote.add(10, 20);
 * ```
 */
export const remote = new Proxy({} as Record<string, (...args: unknown[]) => Promise<any>>, {
  get: (target, prop) => {
    if (typeof prop === 'string') {
      return ref(prop);
    }
    return undefined;
  }
});

export class CallwireError extends Error {
  constructor(
    public readonly errorType: string,
    public readonly errorMessage: string,
  ) {
    super(`${errorType}: ${errorMessage}`);
    this.name = 'CallwireError';
  }
}

type PendingUnary = {
  kind: 'unary';
  resolve: (msg: WireMessage) => void;
  reject: (err: Error) => void;
};

type PendingStream = {
  kind: 'stream';
  push: (chunk: unknown) => void;
  end: () => void;
  error: (err: Error) => void;
};

type Pending = PendingUnary | PendingStream;

export interface TlsClientOptions {
  /** PEM-encoded CA certificate for server verification (optional; skip to trust self-signed) */
  ca?: string;
  /** PEM-encoded client certificate for mTLS (optional) */
  cert?: string;
  /** PEM-encoded client key for mTLS (optional) */
  key?: string;
  /** Skip server certificate verification. Default: false */
  rejectUnauthorized?: boolean;
  /** Override SNI servername (defaults to host) */
  servername?: string;
}

export interface ClientOptions {
  /** Auto-reconnect on disconnect with exponential backoff. Default: false */
  reconnect?: boolean;
  /** Timeout for unary calls in milliseconds. Default: 30000 */
  timeout?: number;
  /** TLS options — if set, connect over TLS */
  tls?: TlsClientOptions;
}

/**
 * Callwire TypeScript Client.
 *
 * @example
 * ```ts
 * const client = new Client();
 * await client.connect('localhost', 9090);
 *
 * const result = await client.call<number>('add', [10, 20]);
 * console.log(result); // 30
 *
 * client.close();
 * ```
 */
export class Client extends EventEmitter {
  private socket: net.Socket | null = null;
  private pending = new Map<number, Pending>();
  private nextId = 0;
  private connected = false;
  private readonly reconnect: boolean;
  private readonly timeout: number;
  private readonly tlsOpts: TlsClientOptions | undefined;
  private host = '';
  private port = 0;

  // Registry routing state
  private isRegistry = false;
  private routeCache = new Map<string, string>();
  private workerClients = new Map<string, Client>();

  constructor(opts: ClientOptions = {}) {
    super();
    this.reconnect = opts.reconnect ?? false;
    this.timeout = opts.timeout ?? 30_000;
    this.tlsOpts = opts.tls;
  }

  /**
   * Connect to a standard Callwire server.
   */
  async connect(host: string, port: number): Promise<void> {
    this.host = host;
    this.port = port;
    await this._connectSocket(host, port);
  }

  /**
   * Connect to a Callwire registry server. All subsequent calls will be
   * automatically routed to the correct worker without any client-side
   * discovery code needed.
   *
   * @example
   * ```ts
   * const client = new Client();
   * await client.connectRegistry('localhost', 29000);
   * const result = await client.call<number>('add', [10, 20]);
   * // 'add' was registered by a worker — the client finds it automatically!
   * ```
   */
  async connectRegistry(host: string, port: number): Promise<void> {
    await this.connect(host, port);
    this.isRegistry = true;
  }

  private async _connectSocket(host: string, port: number): Promise<void> {
    return new Promise((resolve, reject) => {
      let sock: net.Socket;
      if (this.tlsOpts) {
        sock = tls.connect({
          host,
          port,
          ca: this.tlsOpts.ca,
          cert: this.tlsOpts.cert,
          key: this.tlsOpts.key,
          rejectUnauthorized: this.tlsOpts.rejectUnauthorized ?? true,
          servername: this.tlsOpts.servername ?? host,
        }, () => {
          this.socket = sock;
          this.connected = true;
          this._startReading();
          resolve();
        });
      } else {
        sock = net.createConnection({ host, port }, () => {
          this.socket = sock;
          this.connected = true;
          this._startReading();
          resolve();
        });
      }
      sock.once('error', reject);
    });
  }

  private _startReading(): void {
    if (!this.socket) return;
    const sock = this.socket;
    const reader = new BufferedReader(sock);

    const loop = async () => {
      try {
        while (this.connected && this.socket === sock) {
          const payload = await reader.readFrame();
          const msg = unpack(payload);
          this._dispatch(msg);
        }
      } catch {
        this._handleDisconnect();
      }
    };

    loop();
  }

  private _dispatch(msg: WireMessage): void {
    const entry = this.pending.get(msg.id);
    if (!entry) return;

    if (entry.kind === 'unary') {
      if (msg.type !== 'stream_chunk') {
        this.pending.delete(msg.id);
      }
      entry.resolve(msg);
    } else if (entry.kind === 'stream') {
      if (msg.type === 'stream_chunk') {
        entry.push(msg.result);
      } else if (msg.type === 'stream_end') {
        this.pending.delete(msg.id);
        entry.end();
      } else if (msg.type === 'error') {
        this.pending.delete(msg.id);
        entry.error(new CallwireError(msg.error_type ?? 'Error', msg.message ?? 'unknown'));
      }
    }
  }

  private _handleDisconnect(): void {
    this.connected = false;
    const err = new Error('Connection closed');

    for (const entry of this.pending.values()) {
      if (entry.kind === 'unary') entry.reject(err);
      else entry.error(err);
    }
    this.pending.clear();

    if (this.reconnect) {
      this._reconnectLoop();
    }

    this.emit('disconnect');
  }

  private async _reconnectLoop(): Promise<void> {
    let backoff = 50;
    const maxBackoff = 5000;

    while (!this.connected) {
      await new Promise(r => setTimeout(r, backoff));
      backoff = Math.min(backoff * 2, maxBackoff);

      try {
        await this._connectSocket(this.host, this.port);
        this.emit('reconnect');
        return;
      } catch {
        // continue
      }
    }
  }

  private _nextId(): number {
    return ++this.nextId;
  }

  private async _resolveWorker(func: string): Promise<Client | null> {
    if (!this.isRegistry) return null;
    if (func.startsWith('callwire.')) return null;

    let addr = this.routeCache.get(func);
    if (!addr) {
      const addrs = await this.call<string[]>('callwire.discover', [func]);
      if (!addrs || addrs.length === 0) {
        throw new CallwireError('NotFoundError', `function '${func}' not found in registry`);
      }
      addr = addrs[0];
      this.routeCache.set(func, addr);
    }

    let worker = this.workerClients.get(addr);
    if (!worker) {
      const [wHost, wPortStr] = addr.split(':');
      worker = new Client({ reconnect: this.reconnect, timeout: this.timeout });
      await worker.connect(wHost, parseInt(wPortStr, 10));
      this.workerClients.set(addr, worker);
    }

    return worker;
  }

  /**
   * Make a unary (request/response) RPC call.
   *
   * @param func - The name of the remote function
   * @param args - Positional arguments
   * @returns Promise that resolves with the return value
   */
  async call<T = unknown>(func: string, args: unknown[]): Promise<T> {
    const worker = await this._resolveWorker(func);
    if (worker) return worker.call<T>(func, args);

    if (!this.connected || !this.socket) {
      throw new Error('Not connected');
    }

    const id = this._nextId();
    const payload = packRequest(id, func, args);

    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`call '${func}' timed out after ${this.timeout}ms`));
      }, this.timeout);

      this.pending.set(id, {
        kind: 'unary',
        resolve: (msg: WireMessage) => {
          clearTimeout(timer);
          if (msg.type === 'error') {
            reject(new CallwireError(msg.error_type ?? 'Error', msg.message ?? ''));
          } else {
            resolve(msg.result as T);
          }
        },
        reject: (err: Error) => {
          clearTimeout(timer);
          reject(err);
        },
      });

      writeFrame(this.socket!, payload);
    });
  }

  /**
   * Execute multiple calls concurrently (batch mode).
   * Returns an array of results in the same order as `calls`.
   */
  async batch(calls: Array<[string, unknown[]]>): Promise<unknown[]> {
    return Promise.all(calls.map(([func, args]) => this.call(func, args)));
  }

  /**
   * Call a streaming server function.
   * Returns an async generator that yields each chunk as it arrives.
   *
   * @example
   * ```ts
   * for await (const chunk of client.callStream<number>('count_up', [5])) {
   *   console.log(chunk);
   * }
   * ```
   */
  async *callStream<T = unknown>(func: string, args: unknown[]): AsyncGenerator<T> {
    const worker = await this._resolveWorker(func);
    if (worker) {
      yield* worker.callStream<T>(func, args);
      return;
    }

    if (!this.connected || !this.socket) {
      throw new Error('Not connected');
    }

    const id = this._nextId();
    const payload = packRequest(id, func, args);

    // Use a queue + deferred-resolve so no chunk is ever lost to a race.
    const queue: Array<{ type: 'chunk'; value: unknown } | { type: 'end' } | { type: 'error'; err: Error }> = [];
    let waiting: (() => void) | null = null;

    const enqueue = (item: typeof queue[number]) => {
      queue.push(item);
      waiting?.();
      waiting = null;
    };

    this.pending.set(id, {
      kind: 'stream',
      push: (value) => enqueue({ type: 'chunk', value }),
      end: () => enqueue({ type: 'end' }),
      error: (err) => enqueue({ type: 'error', err }),
    });

    writeFrame(this.socket, payload);

    while (true) {
      while (queue.length === 0) {
        await new Promise<void>(resolve => { waiting = resolve; });
      }

      const item = queue.shift()!;
      if (item.type === 'chunk') {
        yield item.value as T;
      } else if (item.type === 'end') {
        return;
      } else {
        throw item.err;
      }
    }
  }

  /**
   * Bind a remote function once and return a reusable callable function.
   */
  ref<T = unknown>(func: string): (...args: unknown[]) => Promise<T> {
    return (...args: unknown[]) => this.call<T>(func, args);
  }

  /**
   * Bind a remote streaming function once and return a reusable generator-maker.
   */
  refStream<T = unknown>(func: string): (...args: unknown[]) => AsyncGenerator<T> {
    return (...args: unknown[]) => this.callStream<T>(func, args);
  }

  /**
   * Close the connection and release all resources.
   */
  close(): void {
    this.connected = false;
    this.socket?.destroy();
    this.socket = null;

    const err = new Error('Connection closed');
    for (const entry of this.pending.values()) {
      if (entry.kind === 'unary') entry.reject(err);
      else entry.error(err);
    }
    this.pending.clear();

    for (const worker of this.workerClients.values()) {
      worker.close();
    }
    this.workerClients.clear();
    this.routeCache.clear();
  }
}
