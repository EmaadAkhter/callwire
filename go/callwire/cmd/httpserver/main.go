package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/emaad/callwire"
)

var client *callwire.Client

func main() {
	callwireAddr := "localhost:9090"
	httpAddr := ":8080"
	if len(os.Args) >= 2 {
		callwireAddr = os.Args[1]
	}
	if len(os.Args) >= 3 {
		httpAddr = os.Args[2]
	}

	var err error
	client, err = callwire.Connect(callwireAddr)
	if err != nil {
		log.Fatalf("connect to callwire: %v", err)
	}
	defer client.Close()

	http.HandleFunc("/", handler)
	log.Printf("listening on %s, proxying to callwire at %s", httpAddr, callwireAddr)
	log.Fatal(http.ListenAndServe(httpAddr, nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	funcName := strings.TrimPrefix(r.URL.Path, "/")
	funcName = strings.TrimSuffix(funcName, "/")
	if funcName == "" {
		jsonError(w, "specify a function name in the path, e.g. /predict?inches=1.5", 400)
		return
	}

	args, err := queryArgs(r)
	if err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	result, err := callwire.Import[interface{}](client, context.Background(), funcName, args)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"result": result,
	})
}

func queryArgs(r *http.Request) ([]interface{}, error) {
	q := r.URL.Query()
	var args []interface{}

	for key, vals := range q {
		if len(vals) == 0 {
			continue
		}
		val := vals[0]
		switch key {
		case "i", "int":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, err
			}
			args = append(args, n)
		case "f", "float":
			n, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return nil, err
			}
			args = append(args, n)
		case "s", "str":
			args = append(args, val)
		default:
			args = append(args, val)
		}
	}
	return args, nil
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
