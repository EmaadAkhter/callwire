# 2 — Orchestrated Examples

These examples show Callwire's **v2 orchestration** — the orchestrator spawns and manages workers across Go, Python, Rust, and TypeScript automatically. You run one file; Callwire handles ports, discovery, and routing.

---

## Quick Start

From the **project root** (where `callwire.toml` lives):

```bash
python examples/2_orchestrated/demo.py
```

That's it. No separate terminals. No manual port config.

---

## How It Works

```
demo.py
  └── callwire.init()
        ├── Reads callwire.toml
        ├── Starts registry on random port (e.g. :49200)
        ├── Spawns go-worker  → binds :49201, registers add/multiply/greet
        └── Spawns rust-worker → binds :49202, registers predict/embed

demo.py client connects to :49200
  → calls add()     → routed to Go worker   ✓
  → calls greet()   → routed to Go worker   ✓
  → calls predict() → routed to Rust worker ✓
  → calls embed()   → routed to Rust worker ✓
```

Workers find the registry via `CALLWIRE_REGISTRY` env var — set automatically by the orchestrator.

---

## Files

| File      | Description                                                         |
|-----------|---------------------------------------------------------------------|
| `demo.py` | Main entry point — spawns workers, calls cross-language functions   |

Worker source files live alongside the SDK examples:
- Go worker: `examples/1_standalone/go_server.go` (also used standalone)
- Rust worker: configured in `callwire.toml` → `[workers.rust-worker]`

---

## callwire.toml reference

Workers are defined in `callwire.toml` at the project root:

```toml
[workers.go-worker]
cmd = "go run examples/1_standalone/go_server.go"
lang = "go"

[workers.rust-worker]
cmd = "cargo run --example cross_lang_client"
lang = "rust"
```

---

## Integrating with FastAPI

```python
from contextlib import asynccontextmanager
from fastapi import FastAPI
import callwire

@asynccontextmanager
async def lifespan(app: FastAPI):
    await callwire.async_init()   # spawns workers, starts registry
    yield
    await callwire.async_shutdown()

app = FastAPI(lifespan=lifespan)
```
