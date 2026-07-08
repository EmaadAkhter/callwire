/* Callwire CLI: scan repo for workers, generate callwire.toml.
 * Mirrors the same tool shipped alongside every other SDK (Go/Python/Rust/
 * TypeScript/Java) — same output format, same detection-by-directory logic. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

static int dir_exists(const char *path) {
    struct stat st;
    return stat(path, &st) == 0 && (st.st_mode & S_IFDIR);
}

static void scan_for_worker(FILE *out, const char *dir, const char *dev_cmd, const char *prod_cmd) {
    if (!dir_exists(dir)) return;

    fprintf(out, "[services.%s-worker]\n", dir);
    fprintf(out, "dev_cmd = \"%s\"\n", dev_cmd);
    fprintf(out, "prod_cmd = \"%s\"\n", prod_cmd);
    fprintf(out, "\n");
}

static int generate_callwire_toml(void) {
    FILE *out = fopen("callwire.toml", "w");
    if (!out) {
        fprintf(stderr, "callwire: failed to write callwire.toml\n");
        return 1;
    }

    fprintf(out, "[project]\n");
    fprintf(out, "name = \"callwire-project\"\n");
    fprintf(out, "version = \"1.0.0\"\n");
    fprintf(out, "\n");

    scan_for_worker(out, "go", "cd go/callwire && go run server.go", "cd go/callwire && go run server.go");
    scan_for_worker(out, "python", "cd python && python3 -m callwire.server", "cd python && python3 -m callwire.server");
    scan_for_worker(out, "rust", "cd rust && cargo run --quiet --release", "cd rust && cargo run --quiet --release");
    scan_for_worker(out, "ts", "cd ts && npx tsx src/server.ts", "cd ts && npx tsx src/server.ts");
    scan_for_worker(out, "java", "cd java && mvn exec:java@server", "cd java && mvn exec:java@server");
    scan_for_worker(out, "c", "cd c/build && ./server", "cd c/build && ./server");

    fclose(out);
    printf("Generated callwire.toml\n");
    return 0;
}

int main(int argc, char **argv) {
    if (argc == 1 || (argc == 2 && strcmp(argv[1], "init") == 0)) {
        return generate_callwire_toml();
    }
    fprintf(stderr, "Usage: callwire [init]\n");
    return 1;
}
