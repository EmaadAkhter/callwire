//! callwire orchestration — zero-config multi-language process management.
//!
//! # Two modes, selected automatically
//!
//! **Orchestrator mode** (default, `CALLWIRE_SPAWNED` is unset):
//! - Reads `callwire.toml` from the current directory.
//! - Starts a dynamic registry on a random OS-assigned port.
//! - Spawns declared worker services as child processes with:
//!   - `CALLWIRE_SPAWNED=1`
//!   - `CALLWIRE_REGISTRY=127.0.0.1:<port>`
//! - Returns an [`OrchestratorGuard`]; dropping it kills all children.
//!
//! **Worker mode** (`CALLWIRE_SPAWNED=1`):
//! - Skips spawning.
//! - Starts the local RPC server on a random OS port.
//! - Connects to `CALLWIRE_REGISTRY`, registers all locally-exported functions.
//! - Spawns an orphan-detection task that calls `std::process::exit(0)` if
//!   the parent process dies unexpectedly.

use std::collections::HashMap;
use std::io::Write;
use std::path::Path;
use std::process::{Child, Command};
use std::time::Duration;
use tokio::net::TcpListener;

// ── TOML parsing ─────────────────────────────────────────────────────────────

#[derive(Debug, serde::Deserialize)]
struct CallwireToml {
    services: Option<HashMap<String, ServiceConfig>>,
}

#[derive(Debug, serde::Deserialize)]
struct ServiceConfig {
    dev_cmd: Option<String>,
    prod_cmd: Option<String>,
}

fn load_toml() -> anyhow::Result<CallwireToml> {
    let path = Path::new("callwire.toml");
    if !path.exists() {
        return Ok(CallwireToml { services: None });
    }
    let content = std::fs::read_to_string(path)?;
    let config: CallwireToml = toml::from_str(&content)?;
    Ok(config)
}

// ── PID file management ──────────────────────────────────────────────────────

fn kill_stale_pids() {
    let pid_file = Path::new(".callwire/pids");
    if !pid_file.exists() {
        return;
    }
    if let Ok(content) = std::fs::read_to_string(pid_file) {
        for line in content.lines() {
            if let Ok(pid) = line.trim().parse::<u32>() {
                // Best-effort: send SIGTERM
                #[cfg(unix)]
                unsafe {
                    libc::kill(pid as libc::pid_t, libc::SIGTERM);
                }
            }
        }
    }
    let _ = std::fs::remove_file(pid_file);
}

fn write_pid_file(children: &[Child]) {
    let _ = std::fs::create_dir_all(".callwire");
    if let Ok(mut f) = std::fs::File::create(".callwire/pids") {
        for child in children {
            let _ = writeln!(f, "{}", child.id());
        }
    }
}

// ── free port helper ─────────────────────────────────────────────────────────

async fn free_port() -> std::io::Result<u16> {
    let listener = TcpListener::bind("127.0.0.1:0").await?;
    Ok(listener.local_addr()?.port())
}

// ── OrchestratorGuard ────────────────────────────────────────────────────────

/// Returned by [`init`] in orchestrator mode.  
/// Dropping this value terminates all spawned worker processes.
pub struct OrchestratorGuard {
    children: Vec<Child>,
}

impl OrchestratorGuard {
    /// Explicitly shut down all workers. Equivalent to dropping the guard.
    pub fn shutdown(mut self) {
        self.terminate_children();
    }

    fn terminate_children(&mut self) {
        for child in &mut self.children {
            let _ = child.kill();
        }
        for child in &mut self.children {
            let _ = child.wait();
        }
        let _ = std::fs::remove_file(".callwire/pids");
    }
}

impl Drop for OrchestratorGuard {
    fn drop(&mut self) {
        self.terminate_children();
    }
}

// ── public API ────────────────────────────────────────────────────────────────

/// Initialise Callwire orchestration.
///
/// Call once at application startup (e.g. in `#[tokio::main]`, an Axum
/// lifespan, or an Actix `on_start`).
///
/// Returns `Some(OrchestratorGuard)` in orchestrator mode (drop to shut down
/// workers), or `None` in worker mode.
///
/// # Example
/// ```no_run
/// #[tokio::main]
/// async fn main() {
///     let _guard = callwire::init().await.expect("callwire init failed");
///     // your server logic
/// }
/// ```
pub async fn init() -> anyhow::Result<Option<OrchestratorGuard>> {
    if std::env::var("CALLWIRE_SPAWNED").as_deref() == Ok("1") {
        init_as_worker().await?;
        return Ok(None);
    }
    let guard = init_as_orchestrator().await?;
    Ok(Some(guard))
}

// ── worker mode ──────────────────────────────────────────────────────────────

async fn init_as_worker() -> anyhow::Result<()> {
    let registry_addr = std::env::var("CALLWIRE_REGISTRY")
        .map_err(|_| anyhow::anyhow!("CALLWIRE_REGISTRY not set in worker mode"))?;

    // Bind to a random OS port
    let listener = TcpListener::bind("127.0.0.1:0").await?;
    let worker_addr = listener.local_addr()?.to_string();
    eprintln!("[callwire] Worker serving on {}", worker_addr);

    // Start the RPC accept loop in the background
    let (_tx, rx) = tokio::sync::watch::channel(false);
    tokio::spawn(crate::server::run_accept_loop(listener, rx));

    // Brief grace period
    tokio::time::sleep(Duration::from_millis(150)).await;

    // Collect exported function names from the global REGISTRY
    let func_names: Vec<String> = {
        let reg = crate::server::REGISTRY.lock().unwrap();
        reg.keys()
            .filter(|k| !k.starts_with("callwire."))
            .cloned()
            .collect()
    };

    // Register each with the parent registry
    let client = crate::client::Client::connect(&registry_addr).await?;
    for name in &func_names {
        if let Err(e) = client
            .import::<(), _>("callwire.register", &(name.as_str(), worker_addr.as_str()))
            .await
        {
            eprintln!("[callwire] Warning: could not register '{}': {}", name, e);
        }
    }
    eprintln!("[callwire] Worker registered {:?} → {}", func_names, worker_addr);

    // Orphan detection: exit if parent dies
    let parent_pid = std::os::unix::process::parent_id();
    tokio::spawn(async move {
        loop {
            tokio::time::sleep(Duration::from_secs(2)).await;
            #[cfg(unix)]
            {
                let current = std::os::unix::process::parent_id();
                if current == 1 && current != parent_pid {
                    eprintln!("[callwire] Parent process gone — worker exiting");
                    std::process::exit(0);
                }
            }
        }
    });

    Ok(())
}

// ── orchestrator mode ─────────────────────────────────────────────────────────

async fn init_as_orchestrator() -> anyhow::Result<OrchestratorGuard> {
    let config = load_toml()?;
    let services = config.services.unwrap_or_default();

    if services.is_empty() {
        return Ok(OrchestratorGuard { children: vec![] });
    }

    // Kill PIDs from a previous run
    kill_stale_pids();

    // Start the dynamic registry on a random port
    let reg_port = free_port().await?;
    let registry_addr = format!("127.0.0.1:{}", reg_port);

    // Register callwire.register and callwire.discover on this process
    crate::registry::start_embedded_registry().await;
    crate::server::serve_on(&registry_addr).await?;
    eprintln!("[callwire] Registry listening on {}", registry_addr);
    unsafe {
        std::env::set_var("CALLWIRE_REGISTRY", &registry_addr);
    }

    let is_prod = std::env::var("CALLWIRE_ENV")
        .map(|v| v.to_lowercase() == "prod")
        .unwrap_or(false);

    let mut children = Vec::new();

    for (name, svc) in &services {
        let cmd = if is_prod {
            svc.prod_cmd.as_deref().or(svc.dev_cmd.as_deref())
        } else {
            svc.dev_cmd.as_deref().or(svc.prod_cmd.as_deref())
        };

        let Some(cmd) = cmd else {
            eprintln!("[callwire] Warning: service '{}' has no command — skipping", name);
            continue;
        };

        let child = Command::new("sh")
            .arg("-c")
            .arg(cmd)
            .env("CALLWIRE_SPAWNED", "1")
            .env("CALLWIRE_REGISTRY", &registry_addr)
            .spawn();

        match child {
            Ok(c) => {
                eprintln!("[callwire] Spawned '{}' (PID {}): {}", name, c.id(), cmd);
                children.push(c);
            }
            Err(e) => {
                eprintln!("[callwire] Failed to spawn '{}': {}", name, e);
            }
        }
    }

    write_pid_file(&children);

    // Wait for workers to come up
    let wait_ms = std::cmp::min(1500 * children.len().max(1) as u64, 5000);
    tokio::time::sleep(Duration::from_millis(wait_ms)).await;

    eprintln!("[callwire] Orchestrator ready — registry at {}", registry_addr);

    Ok(OrchestratorGuard { children })
}
