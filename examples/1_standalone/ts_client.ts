/**
 * Standalone TypeScript Client Example
 * ======================================
 * Connects directly to a running Go server at localhost:9090.
 *
 * Prerequisites:
 *   1. Start the Go server:
 *        cd examples/1_standalone
 *        go run go_server.go
 *
 *   2. From the ts/ directory, run this example:
 *        npx ts-node ../examples/1_standalone/ts_client.ts
 */

import { remote } from '../../ts/src';

async function main() {
  console.log('── Calling Go functions via remote Proxy ──────────────────');

  try {
    // Call 'add' dynamically
    const sum = await remote.add(15, 27);
    console.log(`  add(15, 27)        = ${sum}`);

    // Call 'greet'
    const greeting = await remote.greet('TypeScript Developer');
    console.log(`  greet('Developer') = ${JSON.stringify(greeting)}`);
  } catch (err) {
    console.error(`[error] Call failed:`, err);
    console.error('Is the Go server running? (go run go_server.go)');
    process.exit(1);
  }
}

main().catch(console.error);
