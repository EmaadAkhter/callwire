// TypeScript export script: exports "add" on a fixed port. init() performs
// setup and is called unconditionally at the bottom — auto-starts as soon
// as the script runs, no separate manual step.
import { Server } from '../../../ts/src/server';

const MATRIX_PORT = 9104;

async function init() {
  const server = new Server();
  server.export('add', ([a, b]) => (a as number) + (b as number));
  await server.serve('0.0.0.0', MATRIX_PORT);
  console.log(`TypeScript matrix export listening on :${MATRIX_PORT}`);
}

init();
