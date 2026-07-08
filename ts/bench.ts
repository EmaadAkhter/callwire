import { Client, Server } from './src';
import { performance } from 'perf_hooks';

function sleep(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms));
}

function avg(a: number[]): number {
  return a.reduce((s, v) => s + v, 0) / a.length;
}

function fmtMicros(ns: number): string {
  return (ns / 1000).toFixed(1) + ' µs';
}

function fmtOps(ns: number): string {
  return (1_000_000_000 / ns).toFixed(0);
}

interface BenchResult {
  name: string;
  meanNs: number;
  minNs: number;
  maxNs: number;
  ops: number;
}

async function bench(
  name: string,
  fn: () => Promise<void>,
  iterations = 1000,
  warmup = 100,
): Promise<BenchResult> {
  // warmup
  for (let i = 0; i < warmup; i++) {
    await fn();
  }

  const times: number[] = [];
  for (let i = 0; i < iterations; i++) {
    const start = performance.now();
    await fn();
    const elapsed = performance.now() - start;
    times.push(elapsed * 1_000_000); // convert to ns
  }

  times.sort((a, b) => a - b);
  // trim top/bottom 5%
  const trim = Math.floor(times.length * 0.05);
  const trimmed = times.slice(trim, times.length - trim);

  const mean = avg(trimmed);
  return {
    name,
    meanNs: mean,
    minNs: trimmed[0],
    maxNs: trimmed[trimmed.length - 1],
    ops: 1_000_000_000 / mean,
  };
}

function pad(s: string, n: number): string {
  return s.padEnd(n);
}

async function main() {
  console.log('\n  TypeScript Callwire Benchmarks');
  console.log('  ==============================\n');

  const server = new Server();
  server.export('noop', () => null);
  server.export('add', ([a, b]: [number, number]) => a + b);
  server.export('echo', ([s]: [string]) => s);
  server.export('echo_large', ([s]: [string]) => s);
  server.export('count_up', async function* ([n]: [number]) {
    for (let i = 1; i <= n; i++) yield i;
  });

  await server.serve('127.0.0.1', 0);
  const port = server.address()!.port;
  const client = new Client();
  await client.connect('127.0.0.1', port);

  const results: BenchResult[] = [];

  // Phase 1: Latency
  console.log(`  ${pad('Benchmark', 40)} ${pad('Mean', 14)} ${pad('Min', 14)} ${pad('Max', 14)} ${pad('Ops/s', 10)}`);
  console.log('  ' + '-'.repeat(92));

  results.push(
    await bench('noop (void→void)', () => client.call('noop', []), 500),
  );
  results.push(
    await bench('add(10, 20)', () => client.call<number>('add', [10, 20]), 500),
  );
  results.push(
    await bench('echo(string 10B)', () => client.call<string>('echo', ['hello']), 500),
  );
  results.push(
    await bench('echo(string 1KB)', () => client.call<string>('echo_large', ['x'.repeat(1024)]), 500),
  );
  results.push(
    await bench('batch(5 calls)', async () => {
      await client.batch([
        ['add', [1, 2]],
        ['add', [3, 4]],
        ['add', [5, 6]],
        ['add', [7, 8]],
        ['add', [9, 10]],
      ]);
    }, 300),
  );

  for (const r of results) {
    console.log(
      `  ${pad(r.name, 40)} ${pad(fmtMicros(r.meanNs), 14)} ${pad(fmtMicros(r.minNs), 14)} ${pad(fmtMicros(r.maxNs), 14)} ${pad(r.ops.toFixed(0), 10)}`,
    );
  }

  // Phase 2: Streaming
  console.log('');
  console.log('  --- Streaming ---\n');
  const streamResults: BenchResult[] = [];

  streamResults.push(
    await bench('stream count_up(100)', async () => {
      const stream = client.callStream<number>('count_up', [100]);
      for await (const _ of stream) { /* drain */ }
    }, 100, 10),
  );

  for (const r of streamResults) {
    console.log(
      `  ${pad(r.name, 40)} ${pad(fmtMicros(r.meanNs), 14)} ${pad(fmtMicros(r.minNs), 14)} ${pad(fmtMicros(r.maxNs), 14)} ${pad(r.ops.toFixed(0), 10)}`,
    );
  }

  // Phase 3: Throughput with concurrency
  console.log('');
  console.log('  --- Throughput ---\n');

  for (const workers of [1, 5, 10, 50]) {
    const r = await bench(
      `${workers} concurrent calls`,
      async () => {
        const calls = Array.from({ length: workers }, () =>
          client.call<number>('add', [1, 2]),
        );
        await Promise.all(calls);
      },
      workers === 50 ? 50 : 100,
      10,
    );
    console.log(
      `  ${pad(r.name, 40)} ${pad(fmtMicros(r.meanNs), 14)} ${pad(fmtMicros(r.minNs), 14)} ${pad(fmtMicros(r.maxNs), 14)} ${pad(r.ops.toFixed(0), 10)}`,
    );
  }

  client.close();
  await server.close();

  console.log('\n  Done.\n');
}

main().catch(console.error);
