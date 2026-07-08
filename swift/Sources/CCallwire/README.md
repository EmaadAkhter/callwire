# CCallwire

Vendored copy of `../../../c/{src,include,third_party}` (the Callwire C core),
adapted for Swift Package Manager's C target layout.

**This is a copy, not a symlink**, deliberately — SPM package resolution
(and git hosting in general) can silently drop symlinks when a package is
published/fetched, the same pitfall this repo already hit once with the npm
package's `README.md`. If you change the C core, re-sync this copy:

```sh
cd swift
cp ../c/include/callwire.h Sources/CCallwire/include/
cp ../c/src/{codec,framing,client,server,errors,internal}.[ch] Sources/CCallwire/ 2>/dev/null
cp ../c/third_party/mpack_stub.h Sources/CCallwire/third_party/
```

Two files have adjusted `#include` paths relative to the flattened layout
here (`internal.h` and `codec.c`) — re-check those after re-syncing.
