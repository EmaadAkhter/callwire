package callwire

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"reflect"
	"strconv"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

var errorType = reflect.TypeOf((*error)(nil)).Elem()

var (
	registry      = map[string]interface{}{}
	registryMu    sync.RWMutex
	autoServeMu   sync.Mutex
	autoServeDone bool
	autoListener  net.Listener
	autoServeStop chan struct{}
)

func validateFunc(name string, fn interface{}) error {
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return fmt.Errorf("callwire: Export(%q) requires a function, got %T", name, fn)
	}
	switch n := fnType.NumOut(); {
	case n > 2:
		return fmt.Errorf("callwire: exported %q has %d return values; at most 2 (value, error) supported", name, n)
	case n == 2:
		if !fnType.Out(1).Implements(errorType) {
			return fmt.Errorf("callwire: exported %q has 2 returns but second is %s, not error", name, fnType.Out(1))
		}
	}
	return nil
}

func Export(name string, fn interface{}) error {
	if err := validateFunc(name, fn); err != nil {
		return err
	}
	registryMu.Lock()
	registry[name] = fn
	registryMu.Unlock()
	autoServe()
	return nil
}

func MustExport(name string, fn interface{}) {
	if err := Export(name, fn); err != nil {
		panic(err)
	}
}

func autoServe() {
	autoServeMu.Lock()
	defer autoServeMu.Unlock()
	if autoServeDone {
		return
	}
	if os.Getenv("CALLWIRE_AUTO") == "0" {
		autoServeDone = true
		return
	}
	addr := net.JoinHostPort(defaultHost, strconv.Itoa(defaultPort))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("callwire: auto-serve on %s failed: %v", addr, err)
		return
	}
	autoListener = listener
	autoServeStop = make(chan struct{})
	autoServeDone = true
	log.Printf("callwire: auto-serving on %s", addr)
	go func(stop chan struct{}) {
		defer close(stop)
		for {
			conn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			go handleConnection(conn)
		}
	}(autoServeStop)
}

func Close() {
	autoServeMu.Lock()
	l := autoListener
	stop := autoServeStop
	autoListener = nil
	autoServeStop = nil
	autoServeDone = false
	autoServeMu.Unlock()
	if l != nil {
		l.Close()
	}
	if stop != nil {
		<-stop
	}
}

func Serve(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("callwire serving on %s", addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
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
	defer func() {
		if r := recover(); r != nil {
			payload, _ := encodeError(msg.ID, "PanicError", "function panicked")
			writeFrame(conn, payload)
		}
	}()

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
		payload, _ := encodeError(msg.ID, "TypeError", "expected "+strconv.Itoa(fnType.NumIn())+" args, got "+strconv.Itoa(len(args)))
		writeFrame(conn, payload)
		return
	}

	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		paramType := fnType.In(i)
		argValue := reflect.ValueOf(arg)
		if !argValue.IsValid() {
			in[i] = reflect.Zero(paramType)
			continue
		}
		if argValue.Type().AssignableTo(paramType) {
			in[i] = argValue
			continue
		}
		if argValue.Type().ConvertibleTo(paramType) {
			in[i] = argValue.Convert(paramType)
			continue
		}
		converted, err := decodeIntoParamType(arg, paramType)
		if err != nil {
			payload, _ := encodeError(msg.ID, "TypeError", "arg "+strconv.Itoa(i+1)+" conversion failed: "+err.Error())
			writeFrame(conn, payload)
			return
		}
		in[i] = converted
	}

	results := fnValue.Call(in)

	if results == nil {
		payload, _ := encodeResponse(msg.ID, nil)
		writeFrame(conn, payload)
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

func decodeIntoParamType(arg interface{}, paramType reflect.Type) (reflect.Value, error) {
	raw, err := msgpack.Marshal(arg)
	if err != nil {
		return reflect.Value{}, err
	}
	dst := reflect.New(paramType)
	if err := msgpack.Unmarshal(raw, dst.Interface()); err != nil {
		return reflect.Value{}, err
	}
	return dst.Elem(), nil
}
