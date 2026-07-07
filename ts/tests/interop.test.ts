/**
 * Cross-language integration test: TypeScript, Python, and Rust.
 *
 * Runs full cross-language integration testing by launching target language processes.
 */

import { Client, Server } from '../src';
import { spawn, ChildProcess } from 'child_process';
import * as path from 'path';
import * as fs from 'fs';
import * as net from 'net';

const REPO_ROOT = path.resolve(__dirname, '../..');

function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const srv = net.createServer();
    srv.listen(0, '127.0.0.1', () => {
      const port = (srv.address() as net.AddressInfo).port;
      srv.close(() => resolve(port));
    });
  });
}

function waitPort(port: number, timeout = 5000): Promise<void> {
  return new Promise((resolve, reject) => {
    const start = Date.now();
    const check = () => {
      const socket = new net.Socket();
      socket.connect(port, '127.0.0.1', () => {
        socket.destroy();
        resolve();
      });
      socket.on('error', () => {
        if (Date.now() - start > timeout) {
          reject(new Error(`Timeout waiting for port ${port}`));
        } else {
          setTimeout(check, 100);
        }
      });
    };
    check();
  });
}

describe('Callwire TypeScript: Multi-language Interoperability', () => {
  let tsServer: Server;
  let tsPort: number;

  beforeAll(async () => {
    // Setup a TS Server that Go/Python/Rust clients can call
    tsServer = new Server();
    tsServer.export('add', ([a, b]) => (a as number) + (b as number));
    tsServer.export('greet', ([name]) => `Hello, ${name}!`);
    tsServer.export('crash', () => { throw new Error('boom'); });
    tsServer.export('infer_score', ([req, tensor]) => {
      const t = tensor as number[];
      const sum = t.reduce((x, y) => x + y, 0);
      const r = req as { name: string; scale: number };
      return {
        name: r.name,
        score: sum * r.scale,
        count: t.length
      };
    });
    tsServer.export('count_up', async function* ([n]) {
      for (let i = 1; i <= (n as number); i++) yield i;
    });

    tsPort = await freePort();
    await tsServer.serve('127.0.0.1', tsPort);
  });

  afterAll(async () => {
    await tsServer.close();
  });

  test('Python client → TypeScript server', async () => {
    const pyCode = `
import sys
sys.path.insert(0, "${path.join(REPO_ROOT, 'python')}")
from callwire import Client

c = Client()
c.connect("localhost", ${tsPort})
print("ADD:" + str(c.call("add", [10, 20])))
print("GREET:" + str(c.call("greet", ["Python"])))
try:
    c.call("crash", [])
except Exception as e:
    print("ERROR:" + str(e))
chunks = list(c.call_stream("count_up", [5]))
print("STREAM:" + ",".join(str(x) for x in chunks))
c.close()
`;

    const pyBin = path.join(REPO_ROOT, 'python/.venv/bin/python');
    const child = spawn(pyBin, ['-c', pyCode]);
    let output = '';

    await new Promise<void>((resolve, reject) => {
      child.stdout.on('data', (d) => output += d.toString());
      child.stderr.on('data', (d) => output += d.toString());
      child.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error(`Python client exited with code ${code}\nOutput: ${output}`));
      });
    });

    expect(output).toContain('ADD:30');
    expect(output).toContain('GREET:Hello, Python!');
    expect(output).toContain('ERROR:RuntimeError: boom');
    expect(output).toContain('STREAM:1,2,3,4,5');
  });

  test('Rust client → TypeScript server', async () => {
    // Compile cross_lang_client if not done
    const build = spawn('cargo', ['build', '--example', 'cross_lang_client', '--quiet'], {
      cwd: path.join(REPO_ROOT, 'rust')
    });
    await new Promise<void>((resolve, reject) => {
      build.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error('cargo build failed'));
      });
    });

    const clientBin = path.join(REPO_ROOT, 'rust/target/debug/examples/cross_lang_client');
    const child = spawn(clientBin, [`127.0.0.1:${tsPort}`]);
    let output = '';

    await new Promise<void>((resolve, reject) => {
      child.stdout.on('data', (d) => output += d.toString());
      child.stderr.on('data', (d) => output += d.toString());
      child.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error(`Rust client exited with code ${code}\nOutput: ${output}`));
      });
    });

    expect(output).toContain('ADD:30');
    expect(output).toContain('ERROR:boom');
    expect(output).toContain('INFER_SCORE:5');
    expect(output).toContain('STREAM:1,2,3,4,5');
  });

  test('TypeScript client → Python server', async () => {
    const pyPort = await freePort();
    const pyServerCode = `
import sys
sys.path.insert(0, "${path.join(REPO_ROOT, 'python')}")
from callwire import export, serve

@export
def add(a, b):
    return a + b

@export
def greet(name):
    return f"Hello, {name}!"

serve("localhost", ${pyPort})
`;

    const pyBin = path.join(REPO_ROOT, 'python/.venv/bin/python');
    const pyProc = spawn(pyBin, ['-c', pyServerCode], {
      env: { ...process.env, CALLWIRE_AUTO: '0' }
    });

    try {
      await waitPort(pyPort);

      const client = new Client();
      await client.connect('127.0.0.1', pyPort);

      const addResult = await client.call<number>('add', [12, 13]);
      expect(addResult).toBe(25);

      const greetResult = await client.call<string>('greet', ['TS']);
      expect(greetResult).toBe('Hello, TS!');

      client.close();
    } finally {
      pyProc.kill('SIGKILL');
    }
  });

  test('TypeScript client → Rust server', async () => {
    const rustPort = await freePort();
    // Build cross_lang_server example
    const build = spawn('cargo', ['build', '--example', 'cross_lang_server', '--quiet'], {
      cwd: path.join(REPO_ROOT, 'rust')
    });
    await new Promise<void>((resolve, reject) => {
      build.on('close', (code) => {
        if (code === 0) resolve();
        else reject(new Error('cargo build failed'));
      });
    });

    const serverBin = path.join(REPO_ROOT, 'rust/target/debug/examples/cross_lang_server');
    const rustProc = spawn(serverBin, [String(rustPort)], {
      env: { ...process.env, CALLWIRE_AUTO: '0' }
    });

    try {
      await waitPort(rustPort);

      const client = new Client();
      await client.connect('127.0.0.1', rustPort);

      const addResult = await client.call<number>('add', [3, 4]);
      expect(addResult).toBe(7);

      const stream = client.callStream<number>('count_up', [3]);
      const chunks: number[] = [];
      for await (const chunk of stream) {
        chunks.push(chunk);
      }
      expect(chunks).toEqual([1, 2, 3]);

      client.close();
    } finally {
      rustProc.kill('SIGKILL');
    }
  });
});
