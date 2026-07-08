"""
callwire CLI — `python -m callwire`

Scans the current directory for callwire workers across Go, Rust, Python,
and TypeScript, then generates a complete callwire.toml configuration.

Usage:
    python -m callwire init    # Scan & generate callwire.toml
"""

import sys
import os


EXCLUDED_DIRS = frozenset({
    "node_modules", ".git", "target", "__pycache__", ".venv",
    "dist", "build", ".callwire",
})


def _should_skip(root: str) -> bool:
    parts = root.replace(os.sep, "/").split("/")
    return any(p in EXCLUDED_DIRS for p in parts)


def _find_go_mod_root() -> str | None:
    for root, dirs, files in os.walk("."):
        if _should_skip(root):
            continue
        if "go.mod" in files:
            path = os.path.join(root, "go.mod")
            try:
                with open(path) as f:
                    for line in f:
                        if line.startswith("module ") and "callwire" in line:
                            return os.path.abspath(root)
            except Exception:
                pass
    return None


def _is_sdk_dir(abs_root: str) -> bool:
    abspwd = os.path.abspath(".")
    sdk_roots = [
        os.path.join(abspwd, "go", "callwire"),
        os.path.join(abspwd, "rust"),
        os.path.join(abspwd, "ts"),
        os.path.join(abspwd, "python", "callwire"),
    ]
    for sdk in sdk_roots:
        if abs_root.startswith(sdk) and abs_root != os.path.join(abspwd, "go") and abs_root != os.path.join(abspwd, "python"):
            return True
    return False


def detect_go_workers() -> list[tuple[str, str]]:
    results = []
    go_mod_root = _find_go_mod_root()
    if not go_mod_root:
        return results

    for root, dirs, files in os.walk("."):
        if _should_skip(root) or _is_sdk_dir(os.path.abspath(root)):
            continue
        for file in files:
            if not file.endswith(".go"):
                continue
            path = os.path.join(root, file)
            try:
                with open(path, errors="ignore") as f:
                    content = f.read()
            except Exception:
                continue
            if "package main" not in content:
                continue
            if "callwire." not in content:
                continue
            has_export = "callwire.Export" in content or "callwire.MustExport" in content
            has_init = "callwire.Init" in content or "callwire.Serve" in content
            if not (has_export and has_init):
                continue

            abs_path = os.path.abspath(path)
            rel = os.path.relpath(abs_path, go_mod_root)
            name = os.path.splitext(file)[0].replace("_", "-")
            if name == "main":
                name = os.path.basename(os.path.dirname(path)).replace("_", "-")
            mod_rel = os.path.relpath(go_mod_root, ".")
            dev_cmd = f"cd {mod_rel} && go run {rel}"
            results.append((_service_name(name), dev_cmd))
    return results


def detect_rust_workers() -> list[tuple[str, str]]:
    results = []

    for root, dirs, files in os.walk("."):
        if _should_skip(root):
            continue
        if "Cargo.toml" not in files:
            continue
        cargo_root = os.path.abspath(root)

        # Determine if this is a user project using callwire or the SDK itself
        is_sdk = False
        cargo_toml_path = os.path.join(cargo_root, "Cargo.toml")
        has_callwire_dep = False
        try:
            with open(cargo_toml_path) as f:
                for line in f:
                    if 'name = "callwire"' in line:
                        is_sdk = True
                    if 'callwire' in line:
                        has_callwire_dep = True
        except Exception:
            continue

        # Skip if callwire is not involved at all
        if not has_callwire_dep:
            continue

        # If it's the SDK, use the "examples" detection (examples/ dir)
        example_dir = os.path.join(cargo_root, "examples")
        if os.path.isdir(example_dir):
            for file in sorted(os.listdir(example_dir)):
                if not file.endswith(".rs"):
                    continue
                path = os.path.join(example_dir, file)
                try:
                    with open(path, errors="ignore") as f:
                        content = f.read()
                except Exception:
                    continue
                if "callwire::" not in content and "use callwire" not in content:
                    continue
                has_main = "fn main" in content or "#[tokio::main]" in content
                has_register = "callwire::register_unary" in content or "callwire::export!" in content
                has_init = "callwire::init()" in content
                if not (has_main and has_register and has_init):
                    continue

                name = os.path.splitext(file)[0].replace("_", "-")
                mod_rel = os.path.relpath(cargo_root, ".")
                dev_cmd = f"cd {mod_rel} && cargo run --quiet --example {name}"
                results.append((_service_name(name), dev_cmd))

        # Check src/bin/ for binary targets (user project pattern)
        # Skip SDK's own binary targets (CLI tool, not a worker)
        if not is_sdk:
            bin_dir = os.path.join(cargo_root, "src", "bin")
            if os.path.isdir(bin_dir):
                for file in sorted(os.listdir(bin_dir)):
                    if not file.endswith(".rs"):
                        continue
                    path = os.path.join(bin_dir, file)
                    try:
                        with open(path, errors="ignore") as f:
                            content = f.read()
                    except Exception:
                        continue
                    if "callwire::" not in content and "use callwire" not in content:
                        continue
                    has_main = "fn main" in content or "#[tokio::main]" in content
                    has_register = "callwire::register_unary" in content or "callwire::export!" in content
                    has_init = "callwire::init()" in content
                    if not (has_main and has_register and has_init):
                        continue
                    name = os.path.splitext(file)[0].replace("_", "-")
                    mod_rel = os.path.relpath(cargo_root, ".")
                    dev_cmd = f"cd {mod_rel} && cargo run --bin {name}"
                    results.append((_service_name(name), dev_cmd))

    return results


def detect_ts_workers() -> list[tuple[str, str]]:
    results = []

    for root, dirs, files in os.walk("."):
        if _should_skip(root) or _is_sdk_dir(os.path.abspath(root)):
            continue
        for file in files:
            if not (file.endswith(".ts") or file.endswith(".js")):
                continue
            if file.endswith(".d.ts"):
                continue
            path = os.path.join(root, file)
            try:
                with open(path, errors="ignore") as f:
                    content = f.read()
            except Exception:
                continue

            if "'callwire'" not in content and '"callwire"' not in content:
                continue
            has_server = "new Server(" in content or ".serve(" in content
            if not has_server:
                continue

            name = os.path.splitext(file)[0].replace("_", "-")
            rel = os.path.relpath(path, ".")
            dev_cmd = f"npx tsx {rel}"
            results.append((_service_name(name), dev_cmd))
    return results


def detect_py_workers() -> list[tuple[str, str]]:
    results = []
    sdk_abs = os.path.abspath("python/callwire")

    for root, dirs, files in os.walk("."):
        if _should_skip(root) or _is_sdk_dir(os.path.abspath(root)):
            continue
        for file in files:
            if not file.endswith(".py") or file.startswith("test_") or file == "__main__.py":
                continue
            path = os.path.join(root, file)
            try:
                with open(path, errors="ignore") as f:
                    content = f.read()
            except Exception:
                continue

            has_export = "@export" in content or "callwire.export" in content
            has_serve = "callwire.serve(" in content or "callwire.init(" in content
            if not (has_export and has_serve):
                continue

            name = os.path.splitext(file)[0].replace("_", "-")
            rel = os.path.relpath(path, ".")
            dev_cmd = f"python {rel}"
            results.append((_service_name(name), dev_cmd))
    return results


def format_toml(services: list[tuple[str, str]]) -> str:
    lines = []
    lines.append("# callwire.toml")
    lines.append("# ────────────────────────────────────────────────────────────")
    lines.append("# Generated by `python -m callwire init`")
    lines.append("# ────────────────────────────────────────────────────────────")
    lines.append("")
    lines.append("[project]")
    lines.append(f'name = "{os.path.basename(os.path.abspath("."))}-project"')
    lines.append('version = "1.0.0"')
    lines.append("")
    lines.append("# ── Worker services ─────────────────────────────────────────")
    lines.append("")

    for name, cmd in services:
        lines.append(f"[services.{name}]")
        lines.append(f'dev_cmd  = "{cmd}"')
        lines.append(f'prod_cmd = "./bin/{name}"')
        lines.append("")

    return "\n".join(lines)


def _service_name(stem: str) -> str:
    name = stem.replace("_", "-")
    if name.endswith("-worker"):
        return name
    return f"{name}-worker"


def scan_and_generate_toml() -> str:
    services = []
    services.extend(detect_go_workers())
    services.extend(detect_rust_workers())
    services.extend(detect_ts_workers())
    services.extend(detect_py_workers())
    return format_toml(services)


def main():
    args = sys.argv[1:]

    if not args or args[0] != "init":
        print("Usage: python -m callwire init", file=sys.stderr)
        sys.exit(1)

    toml_path = "callwire.toml"
    if os.path.exists(toml_path):
        print(f"callwire: '{toml_path}' already exists — skipping.", file=sys.stderr)
        print(f"callwire: delete it first, or edit it manually.", file=sys.stderr)
        sys.exit(1)

    try:
        content = scan_and_generate_toml()
        with open(toml_path, "w", encoding="utf-8") as f:
            f.write(content)
        svc_count = content.count("[services.")
        print(f"Created callwire.toml with {svc_count} service(s)")
        print(content)
    except OSError as e:
        print(f"callwire: failed to write callwire.toml: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
