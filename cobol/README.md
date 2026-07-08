# Callwire COBOL SDK

Client-only bindings for GnuCOBOL, calling the Callwire C core over a thin shim.

## Scope

**Unary calls only, integer and string values only.** This matches COBOL's
typical legacy-integration role: a batch/CICS program calling out to a modern
service to fetch or send a number/string, not consuming a streaming API.
Server-side (COBOL exporting functions) and streaming patterns are not
implemented — see `c/IMPLEMENTATION_STATUS.md` if that's ever needed; it
would require registering a COBOL entry point as a C function pointer
(`callwire_server_export`), which GnuCOBOL supports via `PROCEDURE-POINTER`
but wasn't built out here.

## Why a shim instead of calling the C ABI directly

`callwire_value_t` is a tagged union with C struct padding. Hand-replicating
that layout field-for-field in COBOL's DATA DIVISION would be fragile — a
padding/alignment mismatch reads garbage silently, not a crash. Instead,
`src/cobol_shim.c` exposes flat, COBOL-native parameter shapes (`int64`
arrays via `COMP-5`, null-terminated `PIC X` strings) and builds the real
`callwire_value_t` structs on the C side, where the layout is guaranteed
correct by construction.

## Files

- `src/cobol_shim.c` — the shim: `callwire_cobol_connect/close/call_ints/call_str/last_error`
- `copybooks/CALLWIRE.cpy` — common WORKING-STORAGE fields, `COPY` into client programs
- `tests/test_loopback.cob` — real TCP round-trip test (verified against a live server)

## Build

```sh
# 1. Compile the C core as C (not C++ — a single cobc/g++ invocation mixing
#    .c and .cob/.cpp inputs will misparse the .c files).
for f in codec framing client server errors; do
  gcc -std=c99 -pthread -Ic/include -c c/src/$f.c -o /tmp/$f.o
done
gcc -std=c99 -Ic/include -c cobol/src/cobol_shim.c -o /tmp/shim.o

# 2. Compile + link your COBOL program against the object files.
cobc -x your_program.cob -o your_program \
  /tmp/shim.o /tmp/codec.o /tmp/framing.o /tmp/client.o /tmp/server.o /tmp/errors.o -lpthread
```

## String marshalling

C strings must be null-terminated; COBOL `PIC X` fields are space-padded, not
null-terminated. Every string passed to the shim (address, function name,
string args) must be explicitly null-terminated first:

```cobol
STRING FUNCTION TRIM(WS-SOURCE) DELIMITED BY SIZE
    LOW-VALUE DELIMITED BY SIZE
    INTO WS-NULL-TERMINATED
END-STRING.
```

String *results* come back null-terminated but the destination buffer isn't
cleared first, so trailing bytes past the null terminator are whatever was
previously in the buffer — don't `DISPLAY` a result buffer directly without
trimming at the null, use a known-length substring compare/copy instead (see
`tests/test_loopback.cob` for the pattern).

## Status

Verified end-to-end against a live server: connect, `add(10, 20)` (int args/result),
and `greet("World")` (string arg/result) tested and passing over real TCP
(127.0.0.1 loopback). The error path (`callwire_cobol_call_ints`/`call_str`
returning -1, `callwire_cobol_last_error()`) is implemented but not yet covered
by an automated test — `test_loopback.cob` only exercises the success path.
