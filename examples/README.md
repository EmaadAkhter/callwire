# Callwire Examples

Two tiers of examples — start with standalone, graduate to orchestrated.

---

## [1_standalone/](./1_standalone/README.md) — Simple, Direct Connections

> **Best for:** Getting started, understanding the API, debugging.

One Go server, one client. No config files.

```
examples/1_standalone/
  ├── go_server.go       # Go RPC server (exposes add, greet)
  ├── python_client.py   # Python client
  ├── rust_client.rs     # Rust client
  ├── ts_client.ts       # TypeScript client (+ batch calls)
  └── README.md
```

**Quick start:**
```bash
# Terminal 1
go run examples/1_standalone/go_server.go

# Terminal 2
python examples/1_standalone/python_client.py
```

---

## [2_orchestrated/](./2_orchestrated/README.md) — Zero-Config Multi-Language Orchestration

> **Best for:** Production services, microservices, cross-language RPC at scale.

Run **one file**. Callwire reads `callwire.toml`, spawns Go and Rust workers, handles all ports and routing automatically.

```
examples/2_orchestrated/
  ├── demo.py     # Entry point — spawns workers, calls cross-lang functions
  └── README.md
```

**Quick start (from project root):**
```bash
python examples/2_orchestrated/demo.py
```

---

## Supported Languages

| Language   | Server/Worker | Client |
|------------|:---:|:---:|
| Go         | ✅ | ✅ |
| Python     | ✅ | ✅ |
| Rust       | ✅ | ✅ |
| TypeScript | ✅ | ✅ |
