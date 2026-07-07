package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/emaad/callwire"
)

func main() {
	pyAddr := "localhost:9099"
	httpAddr := ":8089"
	if len(os.Args) >= 2 {
		pyAddr = os.Args[1]
	}
	if len(os.Args) >= 3 {
		httpAddr = os.Args[2]
	}

	callwire.Configure("", 9098)
	callwire.Export("double", func(x int) int { return x * 2 })
	callwire.Export("upper", func(s string) string { return strings.ToUpper(s) })

	goClient, err := callwire.Connect("localhost:9098")
	if err != nil {
		log.Fatalf("[Go] self-connect: %v", err)
	}
	defer goClient.Close()

	pyClient, err := callwire.Connect(pyAddr)
	if err != nil {
		log.Printf("[Go] python not reachable at %s: %v", pyAddr, err)
	}

	http.HandleFunc("/go-to-python", callHandler(pyClient))
	http.HandleFunc("/go-to-go", callHandler(goClient))
	http.HandleFunc("/python-to-go", info("Python → Go", "localhost:9098", "double(x)", "upper(s)"))
	http.HandleFunc("/", root)

	log.Printf("[Go] HTTP on %s", httpAddr)
	log.Fatal(http.ListenAndServe(httpAddr, nil))
}

func callHandler(client *callwire.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if client == nil {
			jsonError(w, "not connected", 503)
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

func info(direction, addr string, exports ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jsonResult(w, map[string]interface{}{
			"direction": direction,
			"how":       fmt.Sprintf("connect callwire to %s", addr),
			"exports":   exports,
		})
	}
}

func root(w http.ResponseWriter, r *http.Request) {
	jsonResult(w, map[string]interface{}{
		"server":   "Go all-in-one",
		"callwire": ":9098",
		"http":     ":8089",
		"endpoints": []string{
			"/go-to-python?func=greet&s=world",
			"/go-to-go?func=double&i=21",
		},
	})
}

func parseArgs(r *http.Request) (string, []interface{}, error) {
	q := r.URL.Query()
	funcName := q.Get("func")
	if funcName == "" {
		return "", nil, fmt.Errorf("missing ?func=")
	}
	var args []interface{}
	for key, vals := range q {
		if key == "func" || len(vals) == 0 {
			continue
		}
		v := vals[0]
		switch key {
		case "i", "int":
			n, _ := strconv.ParseInt(v, 10, 64)
			args = append(args, n)
		case "f", "float":
			n, _ := strconv.ParseFloat(v, 64)
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
