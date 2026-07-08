# Callwire Java SDK

Zero-schema RPC for Java/JVM. Call any exported function in Go/Python/Rust/TypeScript/Java from any other language.

## Installation

Add to `pom.xml`:

```xml
<dependency>
    <groupId>dev.callwire</groupId>
    <artifactId>callwire</artifactId>
    <version>2.0.4</version>
</dependency>
```

## Quick Start

### Export a function (server)

```java
import dev.callwire.core.*;

Server server = new Server();
server.export("add", args -> {
    long a = ((Number) args.get(0)).longValue();
    long b = ((Number) args.get(1)).longValue();
    return a + b;
});
server.serve("localhost", 9090);
```

### Call a remote function (client)

```java
import dev.callwire.core.*;
import java.util.*;

Client client = new Client();
client.connect("localhost", 9090);
Object result = client.call("add", Arrays.asList(10L, 20L));
System.out.println(result); // 30
client.close();
```

## API

### Client

```java
// Unary call
Object result = client.call("func_name", List.of(arg1, arg2));

// Server-streaming
Iterator<Object> chunks = client.callStream("stream_func", List.of(arg1));
for (Object chunk : (Iterable<Object>) () -> chunks) {
    System.out.println(chunk);
}

// Client-streaming (TODO: ExportStream)
// Bidi-streaming (TODO: ImportBidi)
```

### Server

```java
Server server = new Server();
server.export("func_name", args -> {
    // Handle args, return result
    return result;
});
server.serve("0.0.0.0", 9090);
```

## Features

- **Unary RPC** — single request, single response
- **Server-streaming** — single request, stream of responses
- **Client-streaming** (stub) — stream of requests, single response
- **Bidirectional streaming** (stub) — concurrent streams both directions
- **TLS/mTLS** (TODO) — OpenSSL via JSSE
- **CLI** — `mvn exec:java@cli -- init` generates `callwire.toml`

## Testing

```bash
mvn test
```

## Publishing to Maven Central

Requires GPG key and Sonatype account. See `.github/workflows/publish.yml` for CI setup.

```bash
mvn deploy -P release
```

## Architecture

- `dev.callwire.core.Framing` — TCP framing (4-byte length + payload)
- `dev.callwire.core.Codec` — msgpack encode/decode via msgpack-core
- `dev.callwire.core.Client` — bidirectional RPC client
- `dev.callwire.core.Server` — handler dispatch + thread pool
- `dev.callwire.cli.Main` — CLI for `callwire init`

## TODO

- Full client-streaming support (ExportStream)
- Full bidirectional streaming support (ImportBidi)
- TLS/mTLS support
- Registry + discovery (dynamic routing)
- Orchestration (callwire.toml integration)
