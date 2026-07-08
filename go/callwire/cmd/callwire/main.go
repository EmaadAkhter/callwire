package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "init" {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/callwire init")
		os.Exit(1)
	}

	path := "callwire.toml"
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "callwire: '%s' already exists — skipping.\n", path)
		fmt.Fprintf(os.Stderr, "callwire: delete it first, or edit it manually.\n")
		os.Exit(1)
	}

	content, err := scanAndGenerate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "callwire: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "callwire: failed to write callwire.toml: %v\n", err)
		os.Exit(1)
	}

	svcCount := strings.Count(content, "[services.")
	fmt.Printf("Created callwire.toml with %d service(s)\n", svcCount)
	fmt.Println(content)
}

var excludedDirs = map[string]bool{
	"node_modules": true, ".git": true, "target": true,
	"__pycache__": true, ".venv": true, "dist": true,
	"build": true, ".callwire": true,
}

func shouldSkip(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		if excludedDirs[p] {
			return true
		}
	}
	return false
}

func findGoModRoot() string {
	var modRoot string
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "go.mod" || shouldSkip(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "module ") && strings.Contains(line, "callwire") {
				abs, _ := filepath.Abs(filepath.Dir(path))
				modRoot = abs
				return filepath.SkipAll
			}
		}
		return nil
	})
	return modRoot
}

func serviceName(stem string) string {
	name := strings.ReplaceAll(stem, "_", "-")
	if strings.HasSuffix(name, "-worker") {
		return name
	}
	return name + "-worker"
}

var repoRoot string

func findRepoRoot() string {
	goModRoot := findGoModRoot()
	if goModRoot == "" {
		pwd, _ := os.Getwd()
		return pwd
	}
	return filepath.Dir(filepath.Dir(goModRoot))
}

func isSDKDir(abs string) bool {
	sdkRoots := []string{
		filepath.Join(repoRoot, "go", "callwire"),
		filepath.Join(repoRoot, "rust"),
		filepath.Join(repoRoot, "ts"),
		filepath.Join(repoRoot, "python", "callwire"),
	}
	goPath := filepath.Join(repoRoot, "go")
	pyPath := filepath.Join(repoRoot, "python")
	for _, sdk := range sdkRoots {
		if strings.HasPrefix(abs, sdk) && abs != goPath && abs != pyPath {
			return true
		}
	}
	return false
}

func detectGoWorkers() ([]string, []string) {
	var names, cmds []string
	goModRoot := findGoModRoot()
	if goModRoot == "" {
		return names, cmds
	}

	filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".go") || shouldSkip(path) {
			return nil
		}
		// Skip CLI tool directories (cmd/ — these are not workers)
		if strings.Contains(path, "/cmd/") {
			return nil
		}
		abs, _ := filepath.Abs(path)
		if isSDKDir(abs) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		if !strings.Contains(content, "package main") {
			return nil
		}
		if !strings.Contains(content, "callwire.") {
			return nil
		}
		hasExport := strings.Contains(content, "callwire.Export") || strings.Contains(content, "callwire.MustExport")
		hasInit := strings.Contains(content, "callwire.Init") || strings.Contains(content, "callwire.Serve")
		if !(hasExport && hasInit) {
			return nil
		}

		absPath, _ := filepath.Abs(path)
		rel, _ := filepath.Rel(goModRoot, absPath)
		// Skip CLI tool directories
		if strings.HasPrefix(rel, "cmd/") {
			return nil
		}
		name := strings.ReplaceAll(strings.TrimSuffix(info.Name(), ".go"), "_", "-")
		if name == "main" {
			name = strings.ReplaceAll(filepath.Base(filepath.Dir(path)), "_", "-")
		}
		modRel, _ := filepath.Rel(repoRoot, goModRoot)
		cmd := fmt.Sprintf("cd %s && go run %s", modRel, rel)
		names = append(names, serviceName(name))
		cmds = append(cmds, cmd)
		return nil
	})

	return names, cmds
}

func detectRustWorkers() ([]string, []string) {
	var names, cmds []string

	filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.Name() != "Cargo.toml" || shouldSkip(path) {
			return nil
		}
		cargoRoot := filepath.Dir(path)

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		if !strings.Contains(content, "callwire") {
			return nil
		}

		isSdk := strings.Contains(content, `name = "callwire"`)

		// Check examples/
		exampleDir := filepath.Join(cargoRoot, "examples")
		if fi, err := os.Stat(exampleDir); err == nil && fi.IsDir() {
			entries, _ := os.ReadDir(exampleDir)
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".rs") {
					continue
				}
				ep := filepath.Join(exampleDir, entry.Name())
				ed, _ := os.ReadFile(ep)
				if err != nil {
					continue
				}
				ec := string(ed)
				if !strings.Contains(ec, "callwire::") && !strings.Contains(ec, "use callwire") {
					continue
				}
				hasMain := strings.Contains(ec, "fn main") || strings.Contains(ec, "#[tokio::main]")
				hasReg := strings.Contains(ec, "callwire::register_unary") || strings.Contains(ec, "callwire::export!")
				hasInit := strings.Contains(ec, "callwire::init()")
				if !(hasMain && hasReg && hasInit) {
					continue
				}
				name := strings.ReplaceAll(strings.TrimSuffix(entry.Name(), ".rs"), "_", "-")
				modRel, _ := filepath.Rel(repoRoot, cargoRoot)
				cmd := fmt.Sprintf("cd %s && cargo run --quiet --example %s", modRel, name)
				names = append(names, serviceName(name))
				cmds = append(cmds, cmd)
			}
		}

		// Check src/bin/ (skip SDK's own binary targets)
		if !isSdk {
			binDir := filepath.Join(cargoRoot, "src", "bin")
			if fi, err := os.Stat(binDir); err == nil && fi.IsDir() {
				entries, _ := os.ReadDir(binDir)
				for _, entry := range entries {
					if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".rs") {
						continue
					}
					bp := filepath.Join(binDir, entry.Name())
					bd, _ := os.ReadFile(bp)
					if err != nil {
						continue
					}
					bc := string(bd)
					if !strings.Contains(bc, "callwire::") && !strings.Contains(bc, "use callwire") {
						continue
					}
					hasMain := strings.Contains(bc, "fn main") || strings.Contains(bc, "#[tokio::main]")
					hasReg := strings.Contains(bc, "callwire::register_unary") || strings.Contains(bc, "callwire::export!")
					hasInit := strings.Contains(bc, "callwire::init()")
					if !(hasMain && hasReg && hasInit) {
						continue
					}
					name := strings.ReplaceAll(strings.TrimSuffix(entry.Name(), ".rs"), "_", "-")
				modRel, _ := filepath.Rel(repoRoot, cargoRoot)
				cmd := fmt.Sprintf("cd %s && cargo run --bin %s", modRel, name)
					names = append(names, serviceName(name))
					cmds = append(cmds, cmd)
				}
			}
		}

		return nil
	})

	return names, cmds
}

func detectTSWorkers() ([]string, []string) {
	var names, cmds []string

	filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || shouldSkip(path) {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".ts") && !strings.HasSuffix(info.Name(), ".js") {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".d.ts") {
			return nil
		}
		abs, _ := filepath.Abs(path)
		if isSDKDir(abs) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		if !strings.Contains(content, "'callwire'") && !strings.Contains(content, "\"callwire\"") {
			return nil
		}
		if !strings.Contains(content, "new Server(") && !strings.Contains(content, ".serve(") {
			return nil
		}
		name := strings.ReplaceAll(strings.TrimSuffix(info.Name(), ".ts"), "_", "-")
		name = strings.ReplaceAll(name, ".js", "")
		cmd := fmt.Sprintf("npx tsx %s", path)
		names = append(names, serviceName(name))
		cmds = append(cmds, cmd)
		return nil
	})

	return names, cmds
}

func detectPyWorkers() ([]string, []string) {
	var names, cmds []string

	filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || shouldSkip(path) {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".py") {
			return nil
		}
		if strings.HasPrefix(info.Name(), "test_") || info.Name() == "__main__.py" {
			return nil
		}
		abs, _ := filepath.Abs(path)
		if isSDKDir(abs) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		hasExport := strings.Contains(content, "@export") || strings.Contains(content, "callwire.export")
		hasServe := strings.Contains(content, "callwire.serve(") || strings.Contains(content, "callwire.init(")
		if !(hasExport && hasServe) {
			return nil
		}
		name := strings.ReplaceAll(strings.TrimSuffix(info.Name(), ".py"), "_", "-")
		cmd := fmt.Sprintf("python %s", path)
		names = append(names, serviceName(name))
		cmds = append(cmds, cmd)
		return nil
	})

	return names, cmds
}

func scanAndGenerate() (string, error) {
	repoRoot = findRepoRoot()

	var services []struct{ name, cmd string }

	for _, fn := range []func() ([]string, []string){
		detectGoWorkers, detectRustWorkers, detectTSWorkers, detectPyWorkers,
	} {
		names, cmds := fn()
		for i := range names {
			services = append(services, struct{ name, cmd string }{names[i], cmds[i]})
		}
	}

	projName := filepath.Base(repoRoot) + "-project"

	var b strings.Builder
	b.WriteString("# callwire.toml\n")
	b.WriteString("# ────────────────────────────────────────────────────────────\n")
	b.WriteString("# Generated by `callwire init` (Go CLI)\n")
	b.WriteString("# ────────────────────────────────────────────────────────────\n\n")
	fmt.Fprintf(&b, "[project]\n")
	fmt.Fprintf(&b, "name = \"%s\"\n", projName)
	fmt.Fprintf(&b, "version = \"1.0.0\"\n\n")
	b.WriteString("# ── Worker services ─────────────────────────────────────────\n\n")

	for _, svc := range services {
		fmt.Fprintf(&b, "[services.%s]\n", svc.name)
		fmt.Fprintf(&b, "dev_cmd  = \"%s\"\n", svc.cmd)
		fmt.Fprintf(&b, "prod_cmd = \"./bin/%s\"\n\n", svc.name)
	}

	return b.String(), nil
}
