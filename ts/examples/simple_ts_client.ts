/**
 * TypeScript Callwire Example
 *
 * Demonstrates the TypeScript client connecting to the Go server.
 *
 * Prerequisites:
 *   1. Start the Go server:
 *      cd go/callwire && go run ../../examples/simple_go_server.go
 *
 *   2. Run this example:
 *      cd ts && npx ts-node examples/simple_ts_client.ts
 */

import { Client } from '../src';

async function main() {
  const client = new Client();

  const registryAddr = process.env.CALLWIRE_REGISTRY;

  try {
    if (registryAddr) {
      const [host, portStr] = registryAddr.split(':');
      await client.connectRegistry(host, Number(portStr));
      console.log(`Connected to dynamic Registry at ${registryAddr} (zero-config routing).\n`);
    } else {
      await client.connect('localhost', 9090);
      console.log('Connected directly to Go RPC Server at localhost:9090.\n');
    }
  } catch {
    if (registryAddr) {
      console.error(`Could not connect to registry at ${registryAddr}.`);
    } else {
      console.error('Could not connect to the Go server. Please run simple_go_server.go first!');
    }
    process.exit(1);
  }

  // 1. Unary call: add
  const sum = await client.call<number>('add', [15, 27]);
  console.log(`RPC 'add(15, 27)' returned: ${sum}`);

  // 2. Unary call: greet
  const greeting = await client.call<string>('greet', ['TypeScript Developer']);
  console.log(`RPC 'greet' returned: ${greeting}`);

  // 3. Batch: fire multiple calls concurrently
  const [r1, r2, r3] = await client.batch([
    ['add', [100, 200]],
    ['add', [1, 1]],
    ['greet', ['Batch Client']],
  ]);
  console.log(`\nBatch results:`);
  console.log(`  add(100, 200) = ${r1}`);
  console.log(`  add(1, 1) = ${r2}`);
  console.log(`  greet('Batch Client') = ${r3}`);

  client.close();
}

main().catch(console.error);
