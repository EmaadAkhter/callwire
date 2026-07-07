package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/emaad/callwire"
)

type lazyClient struct {
	addr string
	mu   sync.Mutex
	c    *callwire.Client
}

func (l *lazyClient) get() (*callwire.Client, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.c != nil {
		return l.c, nil
	}
	client, err := callwire.Connect(l.addr)
	if err != nil {
		return nil, err
	}
	l.c = client
	return l.c, nil
}

func (l *lazyClient) status() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.c != nil {
		return "connected"
	}
	return "disconnected"
}

func (l *lazyClient) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.c != nil {
		_ = l.c.Close()
		l.c = nil
	}
}

func main() {
	pyAddr := envOr("PY_CALLWIRE_ADDR", "localhost:9099")
	httpAddr := envOr("GO_HTTP_ADDR", ":8089")
	callwirePort := envIntOr("GO_CALLWIRE_PORT", 9098)
	if len(os.Args) >= 2 {
		pyAddr = os.Args[1]
	}
	if len(os.Args) >= 3 {
		httpAddr = os.Args[2]
	}
	if len(os.Args) >= 4 {
		parsed, err := strconv.Atoi(os.Args[3])
		if err != nil {
			log.Fatalf("invalid callwire port: %v", err)
		}
		callwirePort = parsed
	}

	callwire.Configure("", callwirePort)
	callwire.MustExport("double", func(x int) int { return x * 2 })
	callwire.MustExport("upper", func(s string) string { return strings.ToUpper(s) })

	goAddr := net.JoinHostPort("localhost", strconv.Itoa(callwirePort))
	goClient, err := callwire.Connect(goAddr)
	if err != nil {
		log.Fatalf("[Go] self-connect: %v", err)
	}
	defer goClient.Close()

	pyClient := &lazyClient{addr: pyAddr}
	defer pyClient.close()

	http.HandleFunc("/go-to-python", callHandler(func() (*callwire.Client, error) {
		return pyClient.get()
	}))
	http.HandleFunc("/go-to-go", callHandler(func() (*callwire.Client, error) {
		return goClient, nil
	}))
	http.HandleFunc("/health", health(goAddr, httpAddr, pyAddr, pyClient))
	http.HandleFunc("/demo", demo(pyAddr))
	http.HandleFunc("/python-to-go", info("Python → Go", goAddr, "double(x)", "upper(s)"))
	http.HandleFunc("/", root(goAddr, httpAddr))

	log.Printf("[Go] HTTP on %s", httpAddr)
	log.Fatal(http.ListenAndServe(httpAddr, nil))
}

func callHandler(getClient func() (*callwire.Client, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, err := getClient()
		if err != nil {
			jsonError(w, "not connected: "+err.Error(), 503)
			return
		}
		funcName, args, err := parseArgs(r)
		if err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		result, err := callwire.Import[interface{}](client, context.Background(), funcName, args)
		if err != nil {
			jsonError(w, err.Error(), 500)
			return
		}
		jsonResult(w, map[string]interface{}{"result": result})
	}
}

func health(goAddr, httpAddr, pyAddr string, pyClient *lazyClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResult(w, map[string]interface{}{
			"status": "ok",
			"services": map[string]string{
				"go_callwire":     goAddr,
				"go_http":         httpAddr,
				"python_callwire": pyAddr,
			},
			"python_link": pyClient.status(),
		})
	}
}

func demo(pyAddr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResult(w, map[string]interface{}{
			"overview": "Go all-in-one demo",
			"calls": []map[string]string{
				{"title": "Go -> Go (double)", "url": "/go-to-go?func=double&i=21"},
				{"title": "Go -> Go (upper)", "url": "/go-to-go?func=upper&s=hello"},
				{"title": "Go -> Python (greet)", "url": "/go-to-python?func=greet&s=world"},
				{"title": "Go -> Python (reverse)", "url": "/go-to-python?func=reverse&s=hello"},
			},
			"also_available": fmt.Sprintf("Python-side demo routes are served at http://localhost:8088/demo (Python callwire: %s)", pyAddr),
		})
	}
}

func envOr(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func info(direction, addr string, exports ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResult(w, map[string]interface{}{
			"direction": direction,
			"how":       fmt.Sprintf("connect callwire to %s", addr),
			"exports":   exports,
		})
	}
}

func root(goAddr, httpAddr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResult(w, map[string]interface{}{
			"server":   "Go all-in-one",
			"callwire": goAddr,
			"http":     httpAddr,
			"endpoints": []string{
				"/health",
				"/demo",
				"/go-to-python?func=greet&s=world",
				"/go-to-go?func=double&i=21",
			},
		})
	}
}

func parseArgs(r *http.Request) (string, []interface{}, error) {
	q := r.URL.Query()
	funcName := q.Get("func")
	if funcName == "" {
		return "", nil, fmt.Errorf("missing ?func=")
	}

	keys := make([]string, 0, len(q))
	for key, vals := range q {
		if key == "func" || len(vals) == 0 {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var args []interface{}
	for _, key := range keys {
		v := q.Get(key)
		switch key {
		case "i", "int":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return "", nil, fmt.Errorf("invalid int for %q: %q", key, v)
			}
			args = append(args, n)
		case "f", "float":
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return "", nil, fmt.Errorf("invalid float for %q: %q", key, v)
			}
			args = append(args, n)
		default:
			args = append(args, v)
		}
	}
	return funcName, args, nil
}

func jsonResult(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
