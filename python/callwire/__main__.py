"""
callwire CLI — `python -m callwire`

Usage:
    python -m callwire init          # Generate a default callwire.toml manifest
"""

import sys
import os

DEFAULT_TOML_CONTENT = """# callwire.toml
# ─────────────────────────────────────────────────────────────────────────────
# This manifest declares all the worker services that make up this project.
#
# Any file that calls callwire.init() will:
#   1. Start a dynamic registry on a random OS-assigned port.
#   2. Spawn each declared service as a child process (with CALLWIRE_SPAWNED=1).
#   3. Each worker auto-registers its exported functions with the registry.
#   4. The parent routes client calls transparently — zero port config needed.
# ─────────────────────────────────────────────────────────────────────────────

[project]
name = "callwire-project"
version = "1.0.0"

# ── Worker services ──────────────────────────────────────────────────────────

# Example Go worker:
# [services.go-worker]
# dev_cmd  = "go run server.go"
# prod_cmd = "./bin/go-worker"

# Example Python worker:
# [services.py-worker]
# dev_cmd  = "python worker.py"
# prod_cmd = "python worker.py"
"""


def scan_and_generate_toml() -> str:
    services = []
    
    # 1. Look for Go files
    for root, _, files in os.walk("."):
        if "node_modules" in root or ".git" in root or "target" in root:
            continue
        for file in files:
            if file.endswith(".go"):
                path = os.path.join(root, file)
                try:
                    with open(path, "r", errors="ignore") as f:
                        content = f.read()
                        if "package main" in content and "callwire.Init" in content:
                            rel_path = os.path.relpath(path, ".")
                            # Extract directory or run the file directly
                            dir_path = os.path.dirname(rel_path)
                            name = os.path.splitext(file)[0].replace("_", "-")
                            if name == "main" and dir_path != ".":
                                name = os.path.basename(dir_path).replace("_", "-")
                            
                            dev_cmd = f"go run {rel_path}"
                            services.append((f"{name}-worker", dev_cmd))
                except Exception:
                    pass

    # 2. Look for Rust packages (Cargo.toml)
    for root, _, files in os.walk("."):
        if "node_modules" in root or ".git" in root or "target" in root:
            continue
        if "Cargo.toml" in files:
            path = os.path.join(root, "Cargo.toml")
            rel_path = os.path.relpath(root, ".")
            try:
                with open(path, "r", errors="ignore") as f:
                    content = f.read()
                    # Verify if it contains callwire dep
                    if 'callwire' in content:
                        name = os.path.basename(os.path.abspath(root)).replace("_", "-")
                        dev_cmd = f"cargo run --manifest-path {os.path.join(rel_path, 'Cargo.toml')}"
                        services.append((f"{name}-worker", dev_cmd))
            except Exception:
                pass

    # 3. Look for TypeScript/Node packages (package.json)
    for root, _, files in os.walk("."):
        if "node_modules" in root or ".git" in root or "target" in root:
            continue
        if "package.json" in files:
            path = os.path.join(root, "package.json")
            rel_path = os.path.relpath(root, ".")
            try:
                with open(path, "r", errors="ignore") as f:
                    content = f.read()
                    if 'callwire' in content:
                        name = os.path.basename(os.path.abspath(root)).replace("_", "-")
                        dev_cmd = f"npm --prefix {rel_path} start"
                        services.append((f"{name}-worker", dev_cmd))
            except Exception:
                pass

    # 4. Look for Python files
    for root, _, files in os.walk("."):
        if "node_modules" in root or ".git" in root or "target" in root or ".venv" in root:
            continue
        for file in files:
            if file.endswith(".py") and file != "__main__.py":
                path = os.path.join(root, file)
                try:
                    with open(path, "r", errors="ignore") as f:
                        content = f.read()
                        if "callwire.init" in content and "CALLWIRE_SPAWNED" not in content:
                            # Skip if this is likely the main orchestrator demo itself
                            if "spawn" in content or "subprocess" in content or "orchestrate" in content:
                                continue
                            rel_path = os.path.relpath(path, ".")
                            name = os.path.splitext(file)[0].replace("_", "-")
                            dev_cmd = f"python {rel_path}"
                            services.append((f"{name}-worker", dev_cmd))
                except Exception:
                    pass

    # Generate config string
    toml = []
    toml.append("# callwire.toml")
    toml.append("# ─────────────────────────────────────────────────────────────────────────────")
    toml.append("# Generated by `python -m callwire init`")
    toml.append("# ─────────────────────────────────────────────────────────────────────────────")
    toml.append("")
    toml.append("[project]")
    toml.append(f'name = "{os.path.basename(os.path.abspath("."))} - project"')
    toml.append('version = "1.0.0"')
    toml.append("")
    toml.append("# ── Worker services ──────────────────────────────────────────────────────────")
    toml.append("")

    for name, cmd in services:
        toml.append(f"[services.{name}]")
        toml.append(f'dev_cmd  = "{cmd}"')
        toml.append(f'prod_cmd = "{cmd}"')
        toml.append("")

    return "\n".join(toml)


def main():
    args = sys.argv[1:]

    if not args or args[0] != "init":
        print("Usage: python -m callwire init", file=sys.stderr)
        sys.exit(1)

    toml_path = "callwire.toml"
    if os.path.exists(toml_path):
        print(f"callwire: '{toml_path}' already exists in current directory — skipping creation.", file=sys.stderr)
        sys.exit(1)

    try:
        content = scan_and_generate_toml()
        with open(toml_path, "w", encoding="utf-8") as f:
            f.write(content)
        print(f"Created dynamic callwire configuration: {os.path.abspath(toml_path)}")
        print("Scanned services details:")
        print(content)
    except OSError as e:
        print(f"callwire: failed to write callwire.toml: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()


