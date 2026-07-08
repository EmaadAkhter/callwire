import * as os from 'os';
import * as path from 'path';
import * as fs from 'fs';
import { execSync } from 'child_process';
import { Client, Server } from '../src';

/**
 * Node-to-Node integration tests.
 * These tests start a real TypeScript server on a random port, then connect
 * a client to it, verifying end-to-end behavior without any mocks.
 */
describe('Callwire TypeScript: Node → Node', () => {
  let server: Server;
  let client: Client;
  let port: number;

  beforeEach(async () => {
    server = new Server();

    server.export('add', ([a, b]) => (a as number) + (b as number));

    server.export('greet', ([name]) => `Hello, ${name}! Welcome to Callwire.`);

    server.export('echo', ([val]) => val);

    // Streaming: count from 1 to n
    server.export('count_up', async function* ([n]) {
      for (let i = 1; i <= (n as number); i++) {
        yield i;
      }
    });

    server.export('throws', () => {
      throw new Error('intentional error from test');
    });

    await server.serve('127.0.0.1', 0); // 0 = random OS-assigned port
    port = server.address()!.port;

    client = new Client();
    await client.connect('127.0.0.1', port);
  });

  afterEach(async () => {
    client.close();
    await server.close();
  });

  test('unary call: add(10, 20) = 30', async () => {
    const result = await client.call<number>('add', [10, 20]);
    expect(result).toBe(30);
  });

  test('unary call: greet("TypeScript")', async () => {
    const greeting = await client.call<string>('greet', ['TypeScript']);
    expect(greeting).toContain('TypeScript');
    expect(greeting).toContain('Hello');
  });

  test('unary call: echo null', async () => {
    const result = await client.call('echo', [null]);
    expect(result).toBeNull();
  });

  test('unary call: echo complex object', async () => {
    const obj = { x: 1, y: [2, 3], z: 'hello' };
    const result = await client.call<typeof obj>('echo', [obj]);
    expect(result).toEqual(obj);
  });

  test('error: call unknown function returns CallwireError', async () => {
    await expect(client.call('no_such_function', [])).rejects.toThrow('NotFoundError');
  });

  test('error: handler throws returns CallwireError', async () => {
    await expect(client.call('throws', [])).rejects.toThrow('RuntimeError');
  });

  test('batch: concurrent calls return in order', async () => {
    const results = await client.batch([
      ['add', [1, 2]],
      ['add', [10, 10]],
      ['greet', ['Batch']],
    ]);
    expect(results[0]).toBe(3);
    expect(results[1]).toBe(20);
    expect((results[2] as string)).toContain('Batch');
  });

  test('streaming: count_up(5) yields [1,2,3,4,5]', async () => {
    const chunks: number[] = [];
    for await (const chunk of client.callStream<number>('count_up', [5])) {
      chunks.push(chunk);
    }
    expect(chunks).toEqual([1, 2, 3, 4, 5]);
  });

  test('multiple concurrent calls complete correctly', async () => {
    const calls = Array.from({ length: 20 }, (_, i) =>
      client.call<number>('add', [i, i])
    );
    const results = await Promise.all(calls);
    results.forEach((r, i) => expect(r).toBe(i * 2));
  });

  test('close-during-active-call rejects pending cleanly', async () => {
    // Register a slow handler
    server.export('slow', async () => {
      await new Promise(r => setTimeout(r, 5000));
      return 'done';
    });

    // Fire a slow call. We need to let it pass the async boundary in
    // _resolveWorker before closing, so we advance the event loop.
    const callPromise = client.call<string>('slow', []);
    await new Promise(r => setTimeout(r, 5));
    client.close();
    await expect(callPromise).rejects.toThrow('Connection closed');
  });

  test('close-during-active-stream terminates stream', async () => {
    server.export('infinite_stream', async function* () {
      let i = 0;
      while (true) {
        yield ++i;
        await new Promise(r => setTimeout(r, 50));
      }
    });

    const stream = client.callStream<number>('infinite_stream', []);
    const iter = stream[Symbol.asyncIterator]();

    // Read a few chunks
    expect((await iter.next()).value).toBe(1);
    expect((await iter.next()).value).toBe(2);
    expect((await iter.next()).value).toBe(3);

    // Close mid-stream — the stream's internal queue has an error item
    // pushed by close(). The next next() picks it up and throws.
    client.close();
    await expect(iter.next()).rejects.toThrow('Connection closed');
  });
});

describe('Callwire TypeScript: TLS', () => {
  let certDir: string;
  let certFile: string;
  let keyFile: string;

  beforeAll(() => {
    certDir = fs.mkdtempSync(path.join(os.tmpdir(), 'callwire-ts-tls-'));
    certFile = path.join(certDir, 'cert.pem');
    keyFile = path.join(certDir, 'key.pem');
    execSync(
      `openssl req -x509 -newkey rsa:2048 -keyout "${keyFile}" -out "${certFile}" -days 365 -nodes -subj '/CN=localhost'`,
      { stdio: 'pipe' },
    );
  });

  afterAll(() => {
    fs.rmSync(certDir, { recursive: true, force: true });
  });

  test('TLS round-trip with self-signed cert', async () => {
    const server = new Server();
    server.export('add', ([a, b]) => (a as number) + (b as number));
    server.export('greet', ([name]) => `Hello, ${name}!`);

    const tlsOpts = { cert: fs.readFileSync(certFile, 'utf8'), key: fs.readFileSync(keyFile, 'utf8') };
    await server.serve('127.0.0.1', 0, tlsOpts);
    const port = server.address()!.port;

    const client = new Client({ tls: { rejectUnauthorized: false } });
    await client.connect('127.0.0.1', port);

    const result = await client.call<number>('add', [10, 20]);
    expect(result).toBe(30);

    const greeting = await client.call<string>('greet', ['TLS']);
    expect(greeting).toBe('Hello, TLS!');

    client.close();
    await server.close();
  });
});
