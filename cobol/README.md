# Callwire COBOL SDK

GnuCOBOL bindings for the Callwire C core, via a thin shim. Supports both
directions: COBOL calling out to a modern service (import), and COBOL
hosting a service that any other language can call (export).

## Scope

**Unary calls only, integer and string values only.** Matches COBOL's
typical legacy-integration role — connecting to/from modern services with
simple numeric/string payloads, not consuming a streaming API.

## Why a shim instead of calling the C ABI directly

`callwire_value_t` is a tagged union with C struct padding. Hand-replicating
that layout field-for-field in COBOL's DATA DIVISION would be fragile — a
padding/alignment mismatch reads garbage silently, not a crash. Instead,
`src/cobol_shim.c` exposes flat, COBOL-native parameter shapes (`int64`
arrays via `COMP-5`, null-terminated `PIC X` strings) and builds the real
`callwire_value_t` structs on the C side, where the layout is guaranteed
correct by construction.

## Import (COBOL calls a server)

```cobol
CALL "callwire_cobol_connect" USING BY REFERENCE WS-ADDR BY VALUE WS-PORT
     RETURNING WS-CLIENT-PTR END-CALL.
CALL "callwire_cobol_call_ints" USING
    BY VALUE WS-CLIENT-PTR BY REFERENCE WS-FUNC-ADD
    BY REFERENCE WS-ARGS BY VALUE WS-ARGC
    BY REFERENCE WS-INT-RESULT RETURNING WS-RC END-CALL.
```

`callwire_cobol_call_ints`/`callwire_cobol_call_str` build the
`callwire_value_t` args internally — one `CALL` statement instead of
manually constructing the tagged union.

## Export (COBOL hosts a server)

A handler is a **separate COBOL subprogram**, compiled as its own
dynamically-loadable module, registered by name:

```cobol
*> add_handler.cob — PROGRAM-ID ADD-HANDLER, PROCEDURE DIVISION USING A B RESULT.
CALL "callwire_cobol_server_new" USING BY REFERENCE WS-ADDR BY VALUE WS-PORT
     RETURNING WS-SERVER-PTR END-CALL.
CALL "callwire_cobol_export_int2" USING
    BY VALUE WS-SERVER-PTR BY REFERENCE WS-FUNC-ADD
    BY REFERENCE WS-PROG-ADD RETURNING WS-RC END-CALL.
CALL "callwire_cobol_server_serve" USING BY VALUE WS-SERVER-PTR
     RETURNING WS-RC END-CALL.
```

`WS-PROG-ADD` is the handler subprogram's name (e.g. `"ADD-HANDLER"`) —
dispatch goes through `libcob`'s own `cob_call()`, the same mechanism
GnuCOBOL uses internally for dynamic `CALL`, rather than passing a raw
`ADDRESS OF` function pointer (untested territory for entry-point calling
conventions — this was verified working first, see Status below).

**Naming gotcha** (cost real debugging time): the compiled `.dylib`'s
basename must match the `CALL`ed name, **lowercased, with dashes preserved**.
`PROGRAM-ID ADD-HANDLER` must be compiled to `add-handler.dylib`, not
`add_handler.dylib` — the underscore version fails at runtime with
`libcob: error: module 'ADD-HANDLER' not found` even though the file exists.
Control this via `cobc -m foo.cob -o add-handler.dylib`. The module also
needs to be on `COB_LIBRARY_PATH` (or preloaded via `COB_PRELOAD`) when the
server process runs.

## Files

- `src/cobol_shim.c` — the shim (import: `callwire_cobol_connect/close/call_ints/call_str`;
  export: `callwire_cobol_server_new/export_int2/export_str1/server_serve/server_close`)
- `copybooks/CALLWIRE.cpy` — common WORKING-STORAGE fields, `COPY` into client programs
- `tests/test_loopback.cob` — import-side round-trip test
- `tests/test_server.cob` + `tests/handlers/*.cob` — export-side round-trip test
  (a COBOL-hosted server, called by a client in any other language)

## Build

`./build.sh` compiles everything (C core, shim, both test programs, both
handler modules) and prints exact run commands. Override `COB_INCLUDE_DIR`/
`COB_LIB_DIR` env vars if your GnuCOBOL install isn't at the Homebrew
default (`/opt/homebrew/{include,lib}`).

## String marshalling

C strings must be null-terminated; COBOL `PIC X` fields are space-padded, not
null-terminated. Every string passed to the shim (address, function name,
string args, handler module names) must be explicitly null-terminated first:

```cobol
STRING FUNCTION TRIM(WS-SOURCE) DELIMITED BY SIZE
    LOW-VALUE DELIMITED BY SIZE
    INTO WS-NULL-TERMINATED
END-STRING.
```

String *results* from `callwire_cobol_call_str` come back null-terminated
but the destination buffer isn't cleared first, so trailing bytes past the
null terminator are whatever was previously in the buffer — don't `DISPLAY`
a result buffer directly without trimming at the null, use a known-length
substring compare/copy instead (see `tests/test_loopback.cob` for the
pattern). Handler subprograms (`callwire_cobol_export_str1`) use raw
space-padded `PIC X(256)` buffers on both sides instead (native COBOL string
convention), trimmed by the shim before re-encoding as a Callwire string.

## Status

**Import**: verified end-to-end against a live server — connect,
`add(10, 20)` (int args/result), `greet("World")` (string arg/result), and
the `NotFoundError` path, all passing over real TCP.

**Export**: verified end-to-end — a COBOL server (`tests/test_server.cob`)
hosting `add`/`greet` via separate handler subprograms, called by an
unmodified C++ client (`examples/1_standalone/cpp_client.cpp`) over real
TCP. `libcob`'s dynamic module loading and `cob_call()` dispatch confirmed
working from a C trampoline registered via `callwire_server_export_ctx`.
