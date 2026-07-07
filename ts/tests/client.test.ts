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
});
