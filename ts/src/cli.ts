#!/usr/bin/env node

import * as fs from 'fs';
import * as path from 'path';

const EXCLUDED_DIRS = new Set([
  'node_modules', '.git', 'target', '__pycache__', '.venv',
  'dist', 'build', '.callwire',
]);

function shouldSkip(p: string): boolean {
  return p.split(path.sep).some(part => EXCLUDED_DIRS.has(part));
}

function isSdkDir(abs: string): boolean {
  const pwd = process.cwd();
  const sdkRoots = [
    path.join(pwd, 'go', 'callwire'),
    path.join(pwd, 'rust'),
    path.join(pwd, 'ts'),
    path.join(pwd, 'python', 'callwire'),
  ];
  const goPath = path.join(pwd, 'go');
  const pyPath = path.join(pwd, 'python');
  for (const sdk of sdkRoots) {
    const canonical = path.resolve(sdk);
    if (abs.startsWith(canonical) && abs !== goPath && abs !== pyPath) {
      return true;
    }
  }
  return false;
}

function walkDir(dir: string): string[] {
  const results: string[] = [];
  const stack = [dir];
  while (stack.length > 0) {
    const current = stack.pop()!;
    if (shouldSkip(current)) continue;
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const entry of entries) {
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) {
        stack.push(full);
      } else {
        results.push(full);
      }
    }
  }
  return results;
}

function findGoModRoot(): string | null {
  const files = walkDir('.');
  for (const f of files) {
    if (path.basename(f) !== 'go.mod') continue;
    const content = fs.readFileSync(f, 'utf-8');
    for (const line of content.split('\n')) {
      if (line.startsWith('module ') && line.includes('callwire')) {
        return path.dirname(path.resolve(f));
      }
    }
  }
  return null;
}

function serviceName(stem: string): string {
  const name = stem.replace(/_/g, '-');
  if (name.endsWith('-worker')) return name;
  return `${name}-worker`;
}

function detectGoWorkers(): [string, string][] {
  const results: [string, string][] = [];
  const goModRoot = findGoModRoot();
  if (!goModRoot) return results;

  const files = walkDir('.');
  for (const f of files) {
    if (!f.endsWith('.go') || shouldSkip(f)) continue;
    const abs = path.resolve(f);
    if (isSdkDir(abs)) continue;
    const content = fs.readFileSync(f, 'utf-8');
    if (!content.includes('package main')) continue;
    if (!content.includes('callwire.')) continue;
    const hasExport = content.includes('callwire.Export') || content.includes('callwire.MustExport');
    const hasInit = content.includes('callwire.Init') || content.includes('callwire.Serve');
    if (!(hasExport && hasInit)) continue;

    const rel = path.relative(goModRoot, abs);
    const fname = path.basename(f, '.go');
    const name = fname === 'main'
      ? path.basename(path.dirname(f)).replace(/_/g, '-')
      : fname.replace(/_/g, '-');
    const modRel = path.relative('.', goModRoot) || '.';
    const cmd = `cd ${modRel} && go run ${rel}`;
    results.push([serviceName(name), cmd]);
  }
  return results;
}

function detectRustWorkers(): [string, string][] {
  const results: [string, string][] = [];
  const entries = fs.readdirSync('.', { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isDirectory() || shouldSkip(entry.name)) continue;
    const cargoToml = path.join(entry.name, 'Cargo.toml');
    if (!fs.existsSync(cargoToml)) continue;
    const content = fs.readFileSync(cargoToml, 'utf-8');
    if (!content.includes('callwire')) continue;

    const isSdk = content.includes('name = "callwire"');
    const cargoRoot = entry.name;
    const modRel = cargoRoot;

    // Check examples/
    const exampleDir = path.join(cargoRoot, 'examples');
    if (fs.existsSync(exampleDir)) {
      for (const e of fs.readdirSync(exampleDir)) {
        if (!e.endsWith('.rs')) continue;
        const ep = path.join(exampleDir, e);
        const ec = fs.readFileSync(ep, 'utf-8');
        if (!ec.includes('callwire::') && !ec.includes('use callwire')) continue;
        const hasMain = ec.includes('fn main') || ec.includes('#[tokio::main]');
        const hasReg = ec.includes('callwire::register_unary') || ec.includes('callwire::export!');
        const hasInit = ec.includes('callwire::init()');
        if (!(hasMain && hasReg && hasInit)) continue;
        const name = path.basename(e, '.rs').replace(/_/g, '-');
        const cmd = `cd ${modRel} && cargo run --quiet --example ${name}`;
        results.push([serviceName(name), cmd]);
      }
    }

    // Check src/bin/ (skip SDK's own binary targets)
    if (!isSdk) {
      const binDir = path.join(cargoRoot, 'src', 'bin');
      if (fs.existsSync(binDir)) {
        for (const e of fs.readdirSync(binDir)) {
          if (!e.endsWith('.rs')) continue;
          const bp = path.join(binDir, e);
          const bc = fs.readFileSync(bp, 'utf-8');
          if (!bc.includes('callwire::') && !bc.includes('use callwire')) continue;
          const hasMain = bc.includes('fn main') || bc.includes('#[tokio::main]');
          const hasReg = bc.includes('callwire::register_unary') || bc.includes('callwire::export!');
          const hasInit = bc.includes('callwire::init()');
          if (!(hasMain && hasReg && hasInit)) continue;
          const name = path.basename(e, '.rs').replace(/_/g, '-');
          const cmd = `cd ${modRel} && cargo run --bin ${name}`;
          results.push([serviceName(name), cmd]);
        }
      }
    }
  }
  return results;
}

function detectTSWorkers(): [string, string][] {
  const results: [string, string][] = [];
  const files = walkDir('.');
  for (const f of files) {
    if (!f.endsWith('.ts') && !f.endsWith('.js')) continue;
    if (f.endsWith('.d.ts')) continue;
    if (shouldSkip(f)) continue;
    const abs = path.resolve(f);
    if (isSdkDir(abs)) continue;
    const content = fs.readFileSync(f, 'utf-8');
    if (!content.includes("'callwire'") && !content.includes('"callwire"')) continue;
    if (!content.includes('new Server(') && !content.includes('.serve(')) continue;
    const name = path.basename(f).replace(/\.(ts|js)$/, '').replace(/_/g, '-');
    const cmd = `npx tsx ${f}`;
    results.push([serviceName(name), cmd]);
  }
  return results;
}

function detectPyWorkers(): [string, string][] {
  const results: [string, string][] = [];
  const files = walkDir('.');
  for (const f of files) {
    if (!f.endsWith('.py')) continue;
    if (shouldSkip(f)) continue;
    const abs = path.resolve(f);
    if (isSdkDir(abs)) continue;
    const fname = path.basename(f);
    if (fname.startsWith('test_') || fname === '__main__.py') continue;
    const content = fs.readFileSync(f, 'utf-8');
    const hasExport = content.includes('@export') || content.includes('callwire.export');
    const hasServe = content.includes('callwire.serve(') || content.includes('callwire.init(');
    if (!(hasExport && hasServe)) continue;
    const name = path.basename(f, '.py').replace(/_/g, '-');
    const cmd = `python ${f}`;
    results.push([serviceName(name), cmd]);
  }
  return results;
}

function formatToml(services: [string, string][]): string {
  const pwd = path.basename(process.cwd());
  const lines: string[] = [];
  lines.push('# callwire.toml');
  lines.push('# ────────────────────────────────────────────────────────────');
  lines.push('# Generated by `callwire init` (TypeScript CLI)');
  lines.push('# ────────────────────────────────────────────────────────────');
  lines.push('');
  lines.push('[project]');
  lines.push(`name = "${pwd}-project"`);
  lines.push('version = "1.0.0"');
  lines.push('');
  lines.push('# ── Worker services ─────────────────────────────────────────');
  lines.push('');

  for (const [name, cmd] of services) {
    lines.push(`[services.${name}]`);
    lines.push(`dev_cmd  = "${cmd}"`);
    lines.push(`prod_cmd = "./bin/${name}"`);
    lines.push('');
  }

  return lines.join('\n');
}

function main(): void {
  const args = process.argv.slice(2);
  if (args.length < 1 || args[0] !== 'init') {
    console.error('Usage: npx tsx src/cli.ts init');
    process.exit(1);
  }

  const tomlPath = 'callwire.toml';
  if (fs.existsSync(tomlPath)) {
    console.error(`callwire: '${tomlPath}' already exists — skipping.`);
    console.error('callwire: delete it first, or edit it manually.');
    process.exit(1);
  }

  const services: [string, string][] = [
    ...detectGoWorkers(),
    ...detectRustWorkers(),
    ...detectTSWorkers(),
    ...detectPyWorkers(),
  ];
  const content = formatToml(services);
  fs.writeFileSync(tomlPath, content, 'utf-8');
  const count = (content.match(/\[services\./g) || []).length;
  console.log(`Created callwire.toml with ${count} service(s)`);
  console.log(content);
}

main();
