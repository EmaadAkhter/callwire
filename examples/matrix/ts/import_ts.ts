// TypeScript import script: calls "add"(10,20) on every OTHER language's
// matrix export server (best-effort — SKIP if a port isn't reachable).
import { Client } from '../../../ts/src/client';

const TARGETS: Record<string, number> = {
  go: 9101,
  python: 9102,
  rust: 9103,
  java: 9105,
  c: 9106,
  cpp: 9107,
  swift: 9108,
  cobol: 9109,
};

async function init() {
  for (const [name, port] of Object.entries(TARGETS)) {
    const client = new Client({ timeout: 2000 });
    try {
      await client.connect('127.0.0.1', port);
    } catch (e) {
      console.log(`${name.padEnd(8)} SKIP (not running: ${e})`);
      continue;
    }
    try {
      const result = await client.call<number>('add', [10, 20]);
      console.log(`${name.padEnd(8)} OK  add(10,20) = ${result}`);
    } catch (e) {
      console.log(`${name.padEnd(8)} SKIP (call failed: ${e})`);
    } finally {
      client.close();
    }
  }
}

init();
