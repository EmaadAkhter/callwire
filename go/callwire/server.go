package callwire

import (
	"log"
	"net"
	"os"
	"reflect"
	"strconv"
	"sync"
)

var (
	registry      = map[string]interface{}{}
	registryMu    sync.RWMutex
	autoServeOnce sync.Once
)

func Export(name string, fn interface{}) {
	registryMu.Lock()
	registry[name] = fn
	registryMu.Unlock()
	autoServeOnce.Do(autoServe)
}

func autoServe() {
	if os.Getenv("CALLWIRE_AUTO") == "0" {
		return
	}
	go func() {
		addr := net.JoinHostPort(defaultHost, strconv.Itoa(defaultPort))
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Printf("callwire: auto-serve on %s failed: %v", addr, err)
			return
		}
		log.Printf("callwire: auto-serving on %s", addr)
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			go handleConnection(conn)
		}
	}()
}

func Serve(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("callwire serving on %s", addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	for {
		payload, err := readFrame(conn)
		if err != nil {
			return
		}
		msg, err := decodeMessage(payload)
		if err != nil {
			continue
		}
		dispatch(conn, msg)
	}
}

func dispatch(conn net.Conn, msg wireMessage) {
	registryMu.RLock()
	fn, ok := registry[msg.Func]
	registryMu.RUnlock()

	if !ok {
		payload, _ := encodeError(msg.ID, "NotFoundError", "function '"+msg.Func+"' not exported")
		writeFrame(conn, payload)
		return
	}

	fnValue := reflect.ValueOf(fn)
	fnType := fnValue.Type()

	args := msg.Args
	if args == nil {
		args = []interface{}{}
	}

	if len(args) != fnType.NumIn() {
		payload, _ := encodeError(msg.ID, "TypeError", "expected "+itoa(fnType.NumIn())+" args, got "+itoa(len(args)))
		writeFrame(conn, payload)
		return
	}

	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		argValue := reflect.ValueOf(arg)
		paramType := fnType.In(i)
		if argValue.Type() != paramType {
			if argValue.Type().ConvertibleTo(paramType) {
				argValue = argValue.Convert(paramType)
			}
		}
		in[i] = argValue
	}

	var results []reflect.Value
	func() {
		defer func() {
			if r := recover(); r != nil {
				payload, _ := encodeError(msg.ID, "PanicError", "function panicked")
				writeFrame(conn, payload)
			}
		}()
		results = fnValue.Call(in)
	}()

	if results == nil {
		return
	}

	if len(results) >= 1 {
		lastIdx := len(results) - 1
		if err, ok := results[lastIdx].Interface().(error); ok && err != nil {
			errorType := "Error"
			message := err.Error()
			if we, ok := err.(*WireError); ok {
				errorType = we.ErrorType
				message = we.Message
			}
			payload, _ := encodeError(msg.ID, errorType, message)
			writeFrame(conn, payload)
			return
		}
	}

	if len(results) == 0 {
		payload, _ := encodeResponse(msg.ID, nil)
		writeFrame(conn, payload)
		return
	}

	result := results[0].Interface()
	payload, err := encodeResponse(msg.ID, result)
	if err != nil {
		payload, _ := encodeError(msg.ID, "InternalError", "failed to encode response")
		writeFrame(conn, payload)
		return
	}
	writeFrame(conn, payload)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
