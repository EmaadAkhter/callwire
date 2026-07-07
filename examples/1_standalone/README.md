# 1 — Standalone Examples

These examples show the simplest possible usage: **one Go server, one client in any language.**  
No config files. No orchestrator. Just start the server and connect.

---

## Quick Start

### Step 1 — Start the Go server

```bash
go run go_server.go
```

The server starts on **localhost:9090** and exposes two functions:
- `add(a, b int) int`
- `greet(name string) string`

---

### Step 2 — Connect with any client

Pick a language and run the matching client in a **separate terminal**:

| Language   | Command                                          |
|------------|--------------------------------------------------|
| Python     | `python python_client.py`                        |
| Rust       | copy `rust_client.rs` into a Cargo project, then `cargo run` |
| TypeScript | from `ts/` dir: `npx ts-node ../examples/1_standalone/ts_client.ts` |

---

## Files

| File               | Description                                        |
|--------------------|----------------------------------------------------|
| `go_server.go`     | Go RPC server — exports `add` and `greet`          |
| `python_client.py` | Python client — calls `add` and `greet`            |
| `rust_client.rs`   | Rust client — calls `add` and `greet`              |
| `ts_client.ts`     | TypeScript client — calls `add`, `greet`, and batch|

---

## What's next?

Once you're comfortable with standalone mode, check out [2_orchestrated](../2_orchestrated/README.md) to see how Callwire can **automatically spawn and manage workers** across Go, Python, Rust, and TypeScript — all from a single `callwire.toml`.
