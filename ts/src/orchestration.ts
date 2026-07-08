import * as fs from 'fs';
import * as net from 'net';
import * as path from 'path';
import * as child_process from 'child_process';
import { Server } from './server';
import { Client } from './client';

// ── TOML mini-parser (no external dep for the subset we need) ─────────────────

interface ServiceConfig {
  dev_cmd?: string;
  prod_cmd?: string;
}

interface CallwireToml {
  services: Record<string, ServiceConfig>;
}

function parseToml(): CallwireToml {
  const tomlPath = path.resolve('callwire.toml');
  if (!fs.existsSync(tomlPath)) return { services: {} };

  const content = fs.readFileSync(tomlPath, 'utf-8');
  const result: CallwireToml = { services: {} };
  let currentService: string | null = null;

  for (const rawLine of content.split('\n')) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;

    // Section header [services.<name>]
    const sectionMatch = line.match(/^\[services\.(.+)\]$/);
    if (sectionMatch) {
      currentService = sectionMatch[1];
      result.services[currentService] = {};
      continue;
    }
    // Skip other headers
    if (line.startsWith('[')) { currentService = null; continue; }

    if (!currentService) continue;
    const eqIdx = line.indexOf('=');
    if (eqIdx === -1) continue;
    const key = line.slice(0, eqIdx).trim();
    let val = line.slice(eqIdx + 1).trim();
    if (val.startsWith('"') && val.endsWith('"')) val = val.slice(1, -1);

    if (key === 'dev_cmd') result.services[currentService].dev_cmd = val;
    if (key === 'prod_cmd') result.services[currentService].prod_cmd = val;
  }

  return result;
}

// ── helpers ────────────────────────────────────────────────────────────────

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address() as net.AddressInfo;
      const port = addr.port;
      srv.close(() => resolve(port));
    });
    srv.on('error', reject);
  });
}

function killStalePids() {
  const pidFile = path.resolve('.callwire', 'pids');
  if (!fs.existsSync(pidFile)) return;
  try {
    const pids = fs.readFileSync(pidFile, 'utf-8').split('\n').filter(Boolean);
    for (const pid of pids) {
      // Negative PID targets the process group (see the detached: true
      // comment at spawn time) so a stale hot-reload PID takes its whole
      // worker tree down, not just the shell wrapper.
      try { process.kill(-Number(pid), 'SIGTERM'); } catch {
        try { process.kill(Number(pid), 'SIGTERM'); } catch { /* already gone */ }
      }
    }
    fs.unlinkSync(pidFile);
  } catch { /* ignore */ }
}

function writePidFile(procs: child_process.ChildProcess[]) {
  fs.mkdirSync('.callwire', { recursive: true });
  const pids = procs.map(p => String(p.pid)).join('\n');
  fs.writeFileSync(path.join('.callwire', 'pids'), pids + '\n');
}

// ── OrchestratorHandle ────────────────────────────────────────────────────────

export class OrchestratorHandle {
  private procs: child_process.ChildProcess[];
  private registryServer: Server;

  constructor(procs: child_process.ChildProcess[], registryServer: Server) {
    this.procs = procs;
    this.registryServer = registryServer;
  }

  /** Terminate all spawned workers and close the registry. */
  async shutdown(): Promise<void> {
    for (const proc of this.procs) {
      // Negative PID = signal the whole process group (detached: true at
      // spawn time made each child's PID double as its group ID). Falls
      // back to signalling just the direct child if the group signal fails.
      try {
        if (proc.pid) {
          process.kill(-proc.pid, 'SIGTERM');
        } else {
          proc.kill('SIGTERM');
        }
      } catch {
        try { proc.kill('SIGTERM'); } catch { /* ignore */ }
      }
    }
    await this.registryServer.close();
    try { fs.unlinkSync(path.join('.callwire', 'pids')); } catch { /* ignore */ }
  }
}

// ── public API ─────────────────────────────────────────────────────────────

/**
 * Initialise Callwire orchestration.
 *
 * Call once in your application's bootstrap function (e.g., Express startup,
 * NestJS bootstrap, or a standalone `callwire run` CLI).
 *
 * - If `CALLWIRE_SPAWNED=1` → worker mode: register local exports with parent.
 * - Otherwise → orchestrator mode: spawn workers from `callwire.toml`.
 *
 * Returns an `OrchestratorHandle` (orchestrator) or `null` (worker).
 *
 * @example
 * ```ts
 * const handle = await initCallwire();
 * // ... your server logic ...
 * await handle?.shutdown();
 * ```
 */
export async function initCallwire(server?: Server): Promise<OrchestratorHandle | null> {
  if (process.env.CALLWIRE_SPAWNED === '1') {
    await initAsWorker(server);
    return null;
  }
  return initAsOrchestrator();
}

// ── worker mode ────────────────────────────────────────────────────────────

async function initAsWorker(workerServer?: Server): Promise<void> {
  const registryAddr = process.env.CALLWIRE_REGISTRY;
  if (!registryAddr) {
    console.error('[callwire] Worker mode: CALLWIRE_REGISTRY not set — skipping registration');
    return;
  }

  if (!workerServer) {
    throw new Error('initCallwire() in worker mode requires a Server instance');
  }

  // Start the local RPC server on a random port
  const port = await freePort();

  // Re-export everything that was registered via server.export() before init()
  // The Server instance used by the worker IS the global one — the user sets
  // their handlers on it, then calls initCallwire(). We start serving here.
  await workerServer.serve('127.0.0.1', port);
  const workerAddr = `127.0.0.1:${port}`;
  console.error(`[callwire] Worker serving on ${workerAddr}`);

  // Brief grace period
  await new Promise(r => setTimeout(r, 150));

  // Register with parent registry
  const [regHost, regPortStr] = registryAddr.split(':');
  const regClient = new Client();
  await regClient.connect(regHost, Number(regPortStr));

  // The worker exports are registered on _workerHandlers (passed via closure)
  // For TS, the user passes their Server instance — we expose the handler names
  const handlerNames = (workerServer as unknown as { handlers: Map<string, unknown> }).handlers 
    ? Array.from((workerServer as unknown as { handlers: Map<string, unknown> }).handlers.keys())
    : [];

  for (const name of handlerNames) {
    try {
      await regClient.call('callwire.register', [name, workerAddr]);
    } catch (e) {
      console.error(`[callwire] Warning: could not register '${name}': ${e}`);
    }
  }
  regClient.close();
  console.error(`[callwire] Worker registered [${handlerNames.join(', ')}] → ${workerAddr}`);

  // Orphan detection: exit if parent dies
  setInterval(() => {
    try {
      process.kill(process.ppid, 0); // 0 = check existence, throws if dead
    } catch {
      console.error('[callwire] Parent process gone — worker exiting');
      process.exit(0);
    }
  }, 2000);
}

// ── orchestrator mode ──────────────────────────────────────────────────────

async function initAsOrchestrator(): Promise<OrchestratorHandle> {
  const config = parseToml();
  const services = Object.entries(config.services);

  if (services.length === 0) {
    const emptyServer = new Server();
    return new OrchestratorHandle([], emptyServer);
  }

  killStalePids();

  // Start the dynamic registry on a random port
  const regPort = await freePort();
  const registryAddr = `127.0.0.1:${regPort}`;

  // Build the registry using an in-process Server
  const registryStore: Record<string, string[]> = {};
  const registryServer = new Server();

  registryServer.export('callwire.register', ([svcName, addr]) => {
    const key = svcName as string;
    if (!registryStore[key]) registryStore[key] = [];
    if (!registryStore[key].includes(addr as string)) {
      registryStore[key].push(addr as string);
    }
    return null;
  });

  registryServer.export('callwire.discover', ([svcName]) => {
    const addrs = registryStore[svcName as string];
    if (!addrs || addrs.length === 0) {
      throw new Error(`service not found: ${svcName}`);
    }
    return addrs;
  });

  await registryServer.serve('127.0.0.1', regPort);
  console.log(`[callwire] Registry listening on ${registryAddr}`);

  const isProd = (process.env.CALLWIRE_ENV ?? 'dev').toLowerCase() === 'prod';
  const spawnedProcs: child_process.ChildProcess[] = [];

  for (const [name, svc] of services) {
    const cmd = isProd
      ? (svc.prod_cmd || svc.dev_cmd)
      : (svc.dev_cmd || svc.prod_cmd);

    if (!cmd) {
      console.warn(`[callwire] Warning: service '${name}' has no command — skipping`);
      continue;
    }

    const proc = child_process.spawn('sh', ['-c', cmd], {
      env: { ...process.env, CALLWIRE_SPAWNED: '1', CALLWIRE_REGISTRY: registryAddr },
      stdio: 'inherit',
      // Puts the shell AND everything it execs (e.g. "cd x && ./worker")
      // into its own process group (Unix). Without this, shutdown()'s
      // proc.kill() only reaches the "sh -c" wrapper — the actual worker
      // binary underneath is a separate process that never receives the
      // signal, keeps running, and (having inherited stdout/stderr via
      // stdio: 'inherit') keeps those pipes open, hanging anything reading
      // this process's output after it exits. Same root cause as the
      // Python/Go/Rust orchestrators' shell wrapper bug, fixed the same way
      // — shutdown() below signals -proc.pid (the group) instead of proc.pid.
      detached: true,
    });

    proc.on('error', (e) => console.error(`[callwire] Failed to spawn '${name}': ${e.message}`));
    console.log(`[callwire] Spawned '${name}' (PID ${proc.pid}): ${cmd}`);
    spawnedProcs.push(proc);
  }

  writePidFile(spawnedProcs);

  // Wait for workers to come up
  const waitMs = Math.min(1500 * Math.max(spawnedProcs.length, 1), 5000);
  await new Promise(r => setTimeout(r, waitMs));

  console.log(`[callwire] Orchestrator ready — registry at ${registryAddr}`);

  // Graceful shutdown on SIGINT/SIGTERM
  const handle = new OrchestratorHandle(spawnedProcs, registryServer);
  const graceful = () => handle.shutdown().then(() => process.exit(0));
  process.once('SIGINT', graceful);
  process.once('SIGTERM', graceful);

  return handle;
}
