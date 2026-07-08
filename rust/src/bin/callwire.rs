/// Callwire CLI — `cargo run --bin callwire -- init`
///
/// Scans the current directory for callwire workers across Go, Rust, Python,
/// and TypeScript, then generates a complete callwire.toml configuration.

use std::fmt::Write;
use std::fs;
use std::io;
use std::path::Path;

const EXCLUDED_DIRS: &[&str] = &[
    "node_modules", ".git", "target", "__pycache__", ".venv",
    "dist", "build", ".callwire",
];

fn should_skip(path: &Path) -> bool {
    for component in path.components() {
        if let Some(name) = component.as_os_str().to_str() {
            if EXCLUDED_DIRS.contains(&name) {
                return true;
            }
        }
    }
    false
}

fn is_sdk_dir(abs: &Path) -> bool {
    let go_mod_root = match find_go_mod_root() {
        Some(r) => r,
        None => return false,
    };
    let go_mod_path = std::path::PathBuf::from(&go_mod_root);
    // Two levels up: go/callwire -> go -> repo root
    let repo_root = match go_mod_path.parent().and_then(|p| p.parent()) {
        Some(r) => r,
        None => return false,
    };
    let sdk_roots = [
        repo_root.join("go").join("callwire"),
        repo_root.join("rust"),
        repo_root.join("ts"),
        repo_root.join("python").join("callwire"),
    ];
    let go_path = repo_root.join("go");
    let py_path = repo_root.join("python");
    for sdk in &sdk_roots {
        let canonical = if let Ok(c) = fs::canonicalize(sdk) { c } else { continue };
        if abs.starts_with(&canonical) && abs != go_path && abs != py_path {
            return true;
        }
    }
    false
}

/// Compute relative path from `base` to `target`, handling `../` when needed.
fn rel_path(base: &Path, target: &Path) -> String {
    let base_abs = if base.is_relative() {
        std::env::current_dir().ok().map(|c| c.join(base)).unwrap_or_else(|| base.to_path_buf())
    } else {
        base.to_path_buf()
    };
    let target_abs = if target.is_relative() {
        std::env::current_dir().ok().map(|c| c.join(target)).unwrap_or_else(|| target.to_path_buf())
    } else {
        target.to_path_buf()
    };
    let base_components: Vec<_> = base_abs.components().collect();
    let target_components: Vec<_> = target_abs.components().collect();
    let mut i = 0;
    while i < base_components.len() && i < target_components.len()
        && base_components[i] == target_components[i]
    {
        i += 1;
    }
    let mut result = std::path::PathBuf::new();
    for _ in i..base_components.len() {
        result.push("..");
    }
    for j in i..target_components.len() {
        result.push(target_components[j]);
    }
    result.to_string_lossy().to_string()
}

fn find_go_mod_root() -> Option<String> {
    let result = None;
    // Search from CWD and parent directories up to 3 levels
    let mut search_root = std::env::current_dir().ok()?;
    for _ in 0..4 {
        let mut files = vec![];
        let _ = walk_dir_at(&search_root, &mut |path: &Path| {
            if path.file_name().and_then(|n| n.to_str()) == Some("go.mod") {
                files.push(path.to_path_buf());
            }
            Ok(())
        });
        for path in &files {
            if should_skip(path) { continue; }
            if let Ok(content) = fs::read_to_string(path) {
                for line in content.lines() {
                    if line.starts_with("module ") && line.contains("callwire") {
                        if let Some(dir) = path.parent() {
                            if let Ok(abs) = fs::canonicalize(dir) {
                                return Some(abs.to_string_lossy().to_string());
                            }
                        }
                    }
                }
            }
        }
        // Try parent directory
        if let Some(parent) = search_root.parent() {
            search_root = parent.to_path_buf();
        } else {
            break;
        }
    }
    result
}

fn walk_dir_at<F>(root: &std::path::Path, f: &mut F) -> io::Result<()>
where
    F: FnMut(&Path) -> io::Result<()>,
{
    let mut stack = vec![root.to_path_buf()];
    while let Some(dir) = stack.pop() {
        if should_skip(&dir) {
            continue;
        }
        for entry in fs::read_dir(&dir)? {
            let entry = entry?;
            let path = entry.path();
            if entry.file_type()?.is_dir() {
                stack.push(path);
            } else {
                f(&path)?;
            }
        }
    }
    Ok(())
}

fn service_name(stem: &str) -> String {
    let name = stem.replace('_', "-");
    if name.ends_with("-worker") {
        return name;
    }
    format!("{}-worker", name)
}

fn repo_root() -> std::path::PathBuf {
    if let Some(go_mod) = find_go_mod_root() {
        let p = std::path::PathBuf::from(&go_mod);
        if let Some(parent) = p.parent().and_then(|p| p.parent()) {
            return parent.to_path_buf();
        }
    }
    if let Ok(cwd) = std::env::current_dir() {
        return cwd;
    }
    std::path::PathBuf::from(".")
}

fn detect_go_workers() -> Vec<(String, String)> {
    let mut results = vec![];
    let go_mod_root = match find_go_mod_root() {
        Some(r) => r,
        None => return results,
    };
    let root = repo_root();

    let mut files = vec![];
    let _ = walk_dir_at(&root, &mut |path: &Path| {
        if path.extension().and_then(|e| e.to_str()) == Some("go") {
            files.push(path.to_path_buf());
        }
        Ok(())
    });

    for path in files {
        if should_skip(&path) {
            continue;
        }
        if let Ok(abs) = fs::canonicalize(&path) {
            if is_sdk_dir(&abs) { continue; }
        }
        let content = match fs::read_to_string(&path) {
            Ok(c) => c,
            Err(_) => continue,
        };
        if !content.contains("package main") { continue; }
        if !content.contains("callwire.") { continue; }
        let has_export = content.contains("callwire.Export") || content.contains("callwire.MustExport");
        let has_init = content.contains("callwire.Init") || content.contains("callwire.Serve");
        if !(has_export && has_init) { continue; }

        let abs_path = if let Ok(a) = fs::canonicalize(&path) { a } else { continue };
        let rel = rel_path(std::path::Path::new(&go_mod_root), &abs_path);
        // Skip CLI tool directories
        if rel.starts_with("cmd/") { continue; }
        let fname = path.file_stem().and_then(|s| s.to_str()).unwrap_or("worker");
        let name = if fname == "main" {
            path.parent().and_then(|p| p.file_name()).and_then(|s| s.to_str()).unwrap_or("worker").replace('_', "-")
        } else {
            fname.replace('_', "-")
        };
        let mod_rel = rel_path(&root, std::path::Path::new(&go_mod_root));
        let cmd = format!("cd {} && go run {}", mod_rel, rel);
        results.push((service_name(&name), cmd));
    }
    results
}

fn detect_rust_workers() -> Vec<(String, String)> {
    let mut results = vec![];
    let root = repo_root();

    if let Ok(entries) = fs::read_dir(&root) {
        for entry in entries.flatten() {
            let path = entry.path();
            if !path.is_dir() || should_skip(&path) { continue; }
            let cargo_toml = path.join("Cargo.toml");
            if !cargo_toml.exists() { continue; }

            let content = match fs::read_to_string(&cargo_toml) {
                Ok(c) => c,
                Err(_) => continue,
            };
            if !content.contains("callwire") { continue; }

            let is_sdk = content.contains("name = \"callwire\"");
            let cargo_root = path;
            let mod_rel = cargo_root.strip_prefix(&root).unwrap_or(&cargo_root).to_string_lossy().to_string();

            // Check examples/
            let example_dir = cargo_root.join("examples");
            if example_dir.is_dir() {
                if let Ok(entries) = fs::read_dir(&example_dir) {
                    for e in entries.flatten() {
                        let ep = e.path();
                        if ep.extension().and_then(|s| s.to_str()) != Some("rs") { continue; }
                        let ec = match fs::read_to_string(&ep) { Ok(c) => c, Err(_) => continue };
                        if !ec.contains("callwire::") && !ec.contains("use callwire") { continue; }
                        let has_main = ec.contains("fn main") || ec.contains("#[tokio::main]");
                        let has_reg = ec.contains("callwire::register_unary") || ec.contains("callwire::export!");
                        let has_init = ec.contains("callwire::init()");
                        if !(has_main && has_reg && has_init) { continue; }
                        let name = ep.file_stem().and_then(|s| s.to_str()).unwrap_or("worker").replace('_', "-");
                        let cmd = format!("cd {} && cargo run --quiet --example {}", mod_rel, name);
                        results.push((service_name(&name), cmd));
                    }
                }
            }

            // Check src/bin/ (skip SDK's own binary targets)
            if !is_sdk {
                let bin_dir = cargo_root.join("src").join("bin");
                if bin_dir.is_dir() {
                    if let Ok(entries) = fs::read_dir(&bin_dir) {
                        for e in entries.flatten() {
                            let bp = e.path();
                            if bp.extension().and_then(|s| s.to_str()) != Some("rs") { continue; }
                            let bc = match fs::read_to_string(&bp) { Ok(c) => c, Err(_) => continue };
                            if !bc.contains("callwire::") && !bc.contains("use callwire") { continue; }
                            let has_main = bc.contains("fn main") || bc.contains("#[tokio::main]");
                            let has_reg = bc.contains("callwire::register_unary") || bc.contains("callwire::export!");
                            let has_init = bc.contains("callwire::init()");
                            if !(has_main && has_reg && has_init) { continue; }
                            let name = bp.file_stem().and_then(|s| s.to_str()).unwrap_or("worker").replace('_', "-");
                            let cmd = format!("cd {} && cargo run --bin {}", mod_rel, name);
                            results.push((service_name(&name), cmd));
                        }
                    }
                }
            }
        }
    }
    results
}


fn detect_ts_workers() -> Vec<(String, String)> {
    let mut results = vec![];
    let root = repo_root();

    let mut files = vec![];
    let _ = walk_dir_at(&root, &mut |path: &Path| {
        if let Some(ext) = path.extension().and_then(|e| e.to_str()) {
            if ext == "ts" || ext == "js" {
                if path.file_name().and_then(|s| s.to_str()).map_or(false, |s| !s.ends_with(".d.ts")) {
                    files.push(path.to_path_buf());
                }
            }
        }
        Ok(())
    });

    for path in files {
        if should_skip(&path) { continue; }
        if let Ok(abs) = fs::canonicalize(&path) {
            if is_sdk_dir(&abs) { continue; }
        }
        let content = match fs::read_to_string(&path) {
            Ok(c) => c,
            Err(_) => continue,
        };
        if !content.contains("'callwire'") && !content.contains("\"callwire\"") { continue; }
        if !content.contains("new Server(") && !content.contains(".serve(") { continue; }

        let name = path.file_stem().and_then(|s| s.to_str()).unwrap_or("worker").replace('_', "-");
        let cmd = format!("npx tsx {}", path.to_string_lossy());
        results.push((service_name(&name), cmd));
    }
    results
}

fn detect_py_workers() -> Vec<(String, String)> {
    let mut results = vec![];
    let root = repo_root();

    let mut files = vec![];
    let _ = walk_dir_at(&root, &mut |path: &Path| {
        if path.extension().and_then(|e| e.to_str()) == Some("py") {
            files.push(path.to_path_buf());
        }
        Ok(())
    });

    for path in files {
        if should_skip(&path) { continue; }
        if let Ok(abs) = fs::canonicalize(&path) {
            if is_sdk_dir(&abs) { continue; }
        }
        let fname = path.file_name().and_then(|s| s.to_str()).unwrap_or("");
        if fname.starts_with("test_") || fname == "__main__.py" { continue; }

        let content = match fs::read_to_string(&path) {
            Ok(c) => c,
            Err(_) => continue,
        };
        let has_export = content.contains("@export") || content.contains("callwire.export");
        let has_serve = content.contains("callwire.serve(") || content.contains("callwire.init(");
        if !(has_export && has_serve) { continue; }

        let name = path.file_stem().and_then(|s| s.to_str()).unwrap_or("worker").replace('_', "-");
        let cmd = format!("python {}", path.to_string_lossy());
        results.push((service_name(&name), cmd));
    }
    results
}

fn format_toml(services: &[(String, String)]) -> String {
    let pwd = repo_root().file_name().and_then(|s| s.to_str()).unwrap_or("project").to_string();

    let mut out = String::new();
    writeln!(out, "# callwire.toml").ok();
    writeln!(out, "# ────────────────────────────────────────────────────────────").ok();
    writeln!(out, "# Generated by `callwire init` (Rust CLI)").ok();
    writeln!(out, "# ────────────────────────────────────────────────────────────").ok();
    writeln!(out).ok();
    writeln!(out, "[project]").ok();
    writeln!(out, "name = \"{}-project\"", pwd).ok();
    writeln!(out, "version = \"1.0.0\"").ok();
    writeln!(out).ok();
    writeln!(out, "# ── Worker services ─────────────────────────────────────────").ok();
    writeln!(out).ok();

    for (name, cmd) in services {
        writeln!(out, "[services.{}]", name).ok();
        writeln!(out, "dev_cmd  = \"{}\"", cmd).ok();
        writeln!(out, "prod_cmd = \"./bin/{}\"", name).ok();
        writeln!(out).ok();
    }

    out
}

fn scan_and_generate() -> String {
    let mut services: Vec<(String, String)> = Vec::new();
    services.extend(detect_go_workers());
    services.extend(detect_rust_workers());
    services.extend(detect_ts_workers());
    services.extend(detect_py_workers());
    format_toml(&services)
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 || args[1] != "init" {
        eprintln!("Usage: cargo run --bin callwire -- init");
        std::process::exit(1);
    }

    let path = "callwire.toml";
    if std::path::Path::new(path).exists() {
        eprintln!("callwire: '{}' already exists — skipping.", path);
        eprintln!("callwire: delete it first, or edit it manually.");
        std::process::exit(1);
    }

    let content = scan_and_generate();
    match fs::write(path, &content) {
        Ok(_) => {
            let count = content.matches("[services.").count();
            println!("Created callwire.toml with {} service(s)", count);
            println!("{}", content);
        }
        Err(e) => {
            eprintln!("callwire: failed to write callwire.toml: {}", e);
            std::process::exit(1);
        }
    }
}
