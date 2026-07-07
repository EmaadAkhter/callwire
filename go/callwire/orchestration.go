package callwire

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── TOML mini-parser ─────────────────────────────────────────────────────────
// We parse only the subset of callwire.toml that we need:
//   [services.<name>]
//   dev_cmd = "..."
//   prod_cmd = "..."

type serviceConfig struct {
	Name    string
	DevCmd  string
	ProdCmd string
}

func parseCallwireToml() ([]serviceConfig, error) {
	data, err := os.ReadFile("callwire.toml")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no manifest — nothing to orchestrate
		}
		return nil, err
	}

	var services []serviceConfig
	var current *serviceConfig

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section header: [services.<name>]
		if strings.HasPrefix(line, "[services.") && strings.HasSuffix(line, "]") {
			if current != nil {
				services = append(services, *current)
			}
			name := line[len("[services."):]
			name = name[:len(name)-1]
			current = &serviceConfig{Name: name}
			continue
		}

		// Skip non-service sections
		if strings.HasPrefix(line, "[") {
			if current != nil {
				services = append(services, *current)
				current = nil
			}
			continue
		}

		if current == nil {
			continue
		}

		// Key = "value"
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding quotes
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}

		switch key {
		case "dev_cmd":
			current.DevCmd = val
		case "prod_cmd":
			current.ProdCmd = val
		}
	}
	if current != nil {
		services = append(services, *current)
	}

	return services, scanner.Err()
}

// ── PID file management ──────────────────────────────────────────────────────

func killStalePids() {
	pidFile := filepath.Join(".callwire", "pids")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err != nil {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err == nil {
			_ = proc.Signal(os.Interrupt)
		}
	}
	_ = os.Remove(pidFile)
}

func writePidFile(procs []*exec.Cmd) {
	_ = os.MkdirAll(".callwire", 0755)
	f, err := os.Create(filepath.Join(".callwire", "pids"))
	if err != nil {
		return
	}
	defer f.Close()
	for _, p := range procs {
		if p.Process != nil {
			fmt.Fprintf(f, "%d\n", p.Process.Pid)
		}
	}
}

// ── OrchestratorHandle ────────────────────────────────────────────────────────

// OrchestratorHandle is returned by Init in orchestrator mode.
// Calling Shutdown (or cancelling the provided Context) terminates
// all spawned worker processes.
type OrchestratorHandle struct {
	procs  []*exec.Cmd
	mu     sync.Mutex
	done   chan struct{}
	once   sync.Once
}

func (h *OrchestratorHandle) Shutdown() {
	h.once.Do(func() {
		h.mu.Lock()
		procs := h.procs
		h.mu.Unlock()
		for _, p := range procs {
			if p.Process != nil {
				_ = p.Process.Signal(os.Interrupt)
			}
		}
		for _, p := range procs {
			_ = p.Wait()
		}
		_ = os.Remove(filepath.Join(".callwire", "pids"))
		close(h.done)
	})
}

// Wait blocks until Shutdown is called.
func (h *OrchestratorHandle) Wait() { <-h.done }

// ── Init — the main entrypoint ────────────────────────────────────────────────

// Init initialises Callwire orchestration.
//
// Two modes, selected automatically from environment variables:
//
//   - Orchestrator mode (default): reads callwire.toml, starts a dynamic
//     registry on a random port, spawns declared service workers as child
//     processes, and waits for them to register.
//
//   - Worker mode (CALLWIRE_SPAWNED=1): skips spawning. Starts the local
//     RPC server on a random OS-assigned port, connects to the parent
//     registry (CALLWIRE_REGISTRY), and registers all locally exported
//     functions.  Starts an orphan-detection goroutine.
//
// The returned *OrchestratorHandle is nil in worker mode.
// The provided ctx is used to trigger shutdown when cancelled.
func Init(ctx context.Context) (*OrchestratorHandle, error) {
	if os.Getenv("CALLWIRE_SPAWNED") == "1" {
		if err := initAsWorker(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return initAsOrchestrator(ctx)
}

// ── worker mode ──────────────────────────────────────────────────────────────

func initAsWorker() error {
	registryAddr := os.Getenv("CALLWIRE_REGISTRY")
	if registryAddr == "" {
		return fmt.Errorf("callwire: CALLWIRE_REGISTRY not set in worker mode")
	}

	// Bind the RPC server on a random OS-assigned port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("callwire: worker failed to bind: %w", err)
	}

	workerAddr := listener.Addr().String()
	log.Printf("[callwire] Worker serving on %s", workerAddr)

	// Serve in a goroutine (non-blocking)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()

	// Give the server a moment to be ready
	time.Sleep(100 * time.Millisecond)

	// Register all exported function names with the parent registry
	registryMu.RLock()
	var funcNames []string
	for name := range registry {
		funcNames = append(funcNames, name)
	}
	registryMu.RUnlock()

	conn, err := Connect(registryAddr)
	if err != nil {
		return fmt.Errorf("callwire: worker could not connect to registry at %s: %w", registryAddr, err)
	}
	defer conn.Close()

	for _, name := range funcNames {
		if _, err := Import[interface{}](conn, context.Background(), "callwire.register", []interface{}{name, workerAddr}); err != nil {
			log.Printf("[callwire] Warning: could not register '%s': %v", name, err)
		}
	}
	log.Printf("[callwire] Worker registered %v → %s", funcNames, workerAddr)

	// Orphan detection: exit if parent dies
	go func() {
		ppid := os.Getppid()
		for {
			time.Sleep(2 * time.Second)
			current := os.Getppid()
			if current == 1 && current != ppid {
				log.Println("[callwire] Parent process gone — worker exiting")
				os.Exit(0)
			}
		}
	}()

	return nil
}

// ── orchestrator mode ─────────────────────────────────────────────────────────

func initAsOrchestrator(ctx context.Context) (*OrchestratorHandle, error) {
	services, err := parseCallwireToml()
	if err != nil {
		return nil, fmt.Errorf("callwire: failed to parse callwire.toml: %w", err)
	}
	if len(services) == 0 {
		return &OrchestratorHandle{done: make(chan struct{})}, nil
	}

	// Kill PIDs from a previous hot-reload cycle
	killStalePids()

	// Start the dynamic registry on a random port
	regPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("callwire: could not find free port: %w", err)
	}
	registryAddr := fmt.Sprintf("127.0.0.1:%d", regPort)

	if _, err := ServeRegistry(registryAddr); err != nil {
		return nil, fmt.Errorf("callwire: failed to start registry: %w", err)
	}
	log.Printf("[callwire] Registry listening on %s", registryAddr)
	os.Setenv("CALLWIRE_REGISTRY", registryAddr)

	isProd := strings.ToLower(os.Getenv("CALLWIRE_ENV")) == "prod"

	handle := &OrchestratorHandle{done: make(chan struct{})}

	for _, svc := range services {
		cmd := svc.DevCmd
		if isProd && svc.ProdCmd != "" {
			cmd = svc.ProdCmd
		}
		if cmd == "" {
			log.Printf("[callwire] Warning: service '%s' has no command — skipping", svc.Name)
			continue
		}

		proc := exec.CommandContext(ctx, "sh", "-c", cmd)
		proc.Env = append(os.Environ(),
			"CALLWIRE_SPAWNED=1",
			"CALLWIRE_REGISTRY="+registryAddr,
		)
		proc.Stdout = os.Stdout
		proc.Stderr = os.Stderr

		if err := proc.Start(); err != nil {
			log.Printf("[callwire] Failed to spawn '%s': %v", svc.Name, err)
			continue
		}
		log.Printf("[callwire] Spawned '%s' (PID %d): %s", svc.Name, proc.Process.Pid, cmd)

		handle.mu.Lock()
		handle.procs = append(handle.procs, proc)
		handle.mu.Unlock()
	}

	writePidFile(handle.procs)

	// Wait for workers to come up (proportional to number of workers)
	waitMs := time.Duration(1500*max(len(handle.procs), 1)) * time.Millisecond
	if waitMs > 5*time.Second {
		waitMs = 5 * time.Second
	}
	time.Sleep(waitMs)

	log.Printf("[callwire] Orchestrator ready — registry at %s", registryAddr)

	// Watch for context cancellation
	go func() {
		<-ctx.Done()
		handle.Shutdown()
	}()

	return handle, nil
}

// ── utility ───────────────────────────────────────────────────────────────────

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
