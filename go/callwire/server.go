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
	"time"

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
	if os.Getenv("CALLWIRE_AUTO") == "0" || os.Getenv("CALLWIRE_SPAWNED") == "1" {
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

// streamPending tracks state for in-progress streaming calls (client-streaming or bidi).
type streamPending struct {
	chunksChan chan wireMessage
	end        chan struct{}
}

func handleConnection(conn net.Conn) {
	// Set SO_LINGER=0 so the connection closes with RST instead of FIN/ACK.
	// This prevents both ends from entering TIME_WAIT, which is critical for
	// connection-churn scenarios that would otherwise exhaust ephemeral ports.
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetLinger(0)
	}
	defer conn.Close()

	streamMu := sync.Mutex{}
	streams := make(map[uint64]*streamPending) // ID -> pending stream state

	for {
		payload, err := readFrame(conn)
		if err != nil {
			return
		}
		msg, err := decodeMessage(payload)
		if err != nil {
			continue
		}

		// Route streaming messages to their pending call IDs.
		if msg.Type == "stream_chunk" || msg.Type == "stream_close" || msg.Type == "stream_end" {
			streamMu.Lock()
			pending, ok := streams[msg.ID]
			streamMu.Unlock()
			if ok {
				select {
				case pending.chunksChan <- msg:
				case <-pending.end:
					// Stream already ended, ignore this message
				}
				continue
			}
		}

		// For non-streaming messages, dispatch as usual, but mark bidi/client-streaming calls.
		if msg.Type == "request" {
			if msg.Stream {
				// Bidirectional streaming
				pendingCh := make(chan wireMessage, 64)
				endCh := make(chan struct{})
				streamMu.Lock()
				streams[msg.ID] = &streamPending{chunksChan: pendingCh, end: endCh}
				streamMu.Unlock()
				go dispatchBidiStream(conn, msg, pendingCh, endCh, func() {
					streamMu.Lock()
					delete(streams, msg.ID)
					streamMu.Unlock()
				})
			} else {
				// Check if this will be client-streaming (next messages will be stream_chunk)
				// For now, treat as unary and set up a pending stream in case chunks arrive.
				pendingCh := make(chan wireMessage, 64)
				endCh := make(chan struct{})
				streamMu.Lock()
				streams[msg.ID] = &streamPending{chunksChan: pendingCh, end: endCh}
				streamMu.Unlock()
				go dispatchClientOrUnary(conn, msg, pendingCh, endCh, func() {
					streamMu.Lock()
					delete(streams, msg.ID)
					streamMu.Unlock()
				})
			}
		} else {
			// This shouldn't normally happen (streaming message without pending entry)
			dispatch(conn, msg)
		}
	}
}

// isChannelReturn reports whether the function's first return value is a
// receive-capable channel (chan T or <-chan T).
func isChannelReturn(fnType reflect.Type) bool {
	if fnType.NumOut() == 0 {
		return false
	}
	k := fnType.Out(0).Kind()
	return k == reflect.Chan
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

	// If the first return value is a channel, stream its elements.
	if len(results) >= 1 && isChannelReturn(fnType) {
		dispatchStream(conn, msg.ID, fnType, results)
		return
	}

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

// dispatchStream reads from a channel return value and sends stream_chunk /
// stream_end frames. If there is a second (error) return value it is checked
// after the channel closes.
func dispatchStream(conn net.Conn, id uint64, fnType reflect.Type, results []reflect.Value) {
	chanVal := results[0]
	var errVal reflect.Value
	if len(results) == 2 {
		errVal = results[1]
	}

	// If the function itself returned an error upfront (second return value is
	// non-nil), report it before streaming.
	if errVal.IsValid() {
		if err, ok := errVal.Interface().(error); ok && err != nil {
			errType := "Error"
			msg := err.Error()
			if we, ok := err.(*WireError); ok {
				errType = we.ErrorType
				msg = we.Message
			}
			payload, _ := encodeError(id, errType, msg)
			writeFrame(conn, payload)
			return
		}
	}

	// Stream channel values until it closes.
	for {
		v, open := chanVal.Recv()
		if !open {
			break
		}
		payload, err := encodeStreamChunk(id, v.Interface())
		if err != nil {
			payload, _ = encodeError(id, "InternalError", "failed to encode stream chunk")
			writeFrame(conn, payload)
			return
		}
		if err := writeFrame(conn, payload); err != nil {
			return
		}
	}

	payload, _ := encodeStreamEnd(id)
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

// isChannelInput reports whether a parameter type is a receive-capable channel.
func isChannelInput(paramType reflect.Type) bool {
	k := paramType.Kind()
	return k == reflect.Chan
}

// dispatchClientOrUnary handles both unary calls and client-streaming calls.
// It waits briefly for stream_chunk messages; if none arrive, treats as unary.
// If stream_chunk arrives, waits for stream_close, then invokes the function with all chunks.
func dispatchClientOrUnary(conn net.Conn, msg wireMessage, chunksChan chan wireMessage, endChan chan struct{}, cleanup func()) {
	defer cleanup()
	defer close(endChan)

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

	// Try to receive a stream_chunk with timeout to detect client-streaming.
	hasChunks := false
	chunks := []interface{}{}
	select {
	case firstMsg := <-chunksChan:
		if firstMsg.Type == "stream_chunk" {
			// This is client-streaming.
			hasChunks = true
			chunks = append(chunks, firstMsg.Result)
			// Collect remaining chunks until stream_close.
			for {
				msg := <-chunksChan
				if msg.Type == "stream_close" {
					break
				} else if msg.Type == "stream_chunk" {
					chunks = append(chunks, msg.Result)
				} else if msg.Type == "error" {
					payload, _ := encodeError(msg.ID, msg.ErrorType, msg.Message)
					writeFrame(conn, payload)
					return
				}
			}
		} else if firstMsg.Type == "error" {
			payload, _ := encodeError(msg.ID, firstMsg.ErrorType, firstMsg.Message)
			writeFrame(conn, payload)
			return
		}
	case <-time.After(10 * time.Millisecond):
		// No stream_chunk arrived quickly, treat as unary.
		break
	}

	// Build arguments for function invocation.
	args := msg.Args
	if args == nil {
		args = []interface{}{}
	}

	if hasChunks && fnType.NumIn() > 0 && isChannelInput(fnType.In(0)) {
		// Client-streaming: first param should be a channel to receive chunks.
		inChunkCh := make(chan interface{}, len(chunks))
		for _, c := range chunks {
			inChunkCh <- c
		}
		close(inChunkCh)

		// Prepare arguments: first arg is the chunk channel, rest from msg.Args.
		if len(msg.Args) > fnType.NumIn()-1 {
			payload, _ := encodeError(msg.ID, "TypeError", "expected "+strconv.Itoa(fnType.NumIn()-1)+" args after channel, got "+strconv.Itoa(len(msg.Args)))
			writeFrame(conn, payload)
			return
		}

		in := make([]reflect.Value, fnType.NumIn())
		// First parameter: channel of chunks
		inChChan := reflect.MakeChan(fnType.In(0), len(chunks))
		for _, c := range chunks {
			inChChan.Send(reflect.ValueOf(c))
		}
		inChChan.Close()
		in[0] = inChChan

		// Remaining parameters from msg.Args
		for i, arg := range args {
			paramType := fnType.In(i + 1)
			argValue := reflect.ValueOf(arg)
			if !argValue.IsValid() {
				in[i+1] = reflect.Zero(paramType)
				continue
			}
			if argValue.Type().AssignableTo(paramType) {
				in[i+1] = argValue
				continue
			}
			if argValue.Type().ConvertibleTo(paramType) {
				in[i+1] = argValue.Convert(paramType)
				continue
			}
			converted, err := decodeIntoParamType(arg, paramType)
			if err != nil {
				payload, _ := encodeError(msg.ID, "TypeError", "arg "+strconv.Itoa(i+2)+" conversion failed: "+err.Error())
				writeFrame(conn, payload)
				return
			}
			in[i+1] = converted
		}

		results := fnValue.Call(in)
		// Handle response as unary call.
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
		return
	}

	// Unary call (or fallback if first param isn't a channel).
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

	if len(results) >= 1 && isChannelReturn(fnType) {
		dispatchStream(conn, msg.ID, fnType, results)
		return
	}

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

// dispatchBidiStream handles bidirectional-streaming calls.
// Both client and server send stream_chunk frames; each sends stream_end when done.
func dispatchBidiStream(conn net.Conn, msg wireMessage, chunksChan chan wireMessage, endChan chan struct{}, cleanup func()) {
	defer cleanup()
	defer close(endChan)

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

	// Bidirectional streaming requires a function that accepts and returns channels.
	if fnType.NumIn() != 1 || fnType.NumOut() < 1 {
		payload, _ := encodeError(msg.ID, "TypeError", "bidi-stream function must accept 1 param (input channel) and return (output channel[, error])")
		writeFrame(conn, payload)
		return
	}

	if !isChannelInput(fnType.In(0)) {
		payload, _ := encodeError(msg.ID, "TypeError", "bidi-stream function's first parameter must be a channel")
		writeFrame(conn, payload)
		return
	}

	if !isChannelReturn(fnType) {
		payload, _ := encodeError(msg.ID, "TypeError", "bidi-stream function must return a channel as first return value")
		writeFrame(conn, payload)
		return
	}

	// Create input channel for receiving client chunks.
	inChType := fnType.In(0)
	inCh := reflect.MakeChan(inChType, 64)

	// Start a goroutine to feed incoming chunks into the input channel.
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		for {
			select {
			case msg := <-chunksChan:
				if msg.Type == "stream_chunk" {
					inCh.Send(reflect.ValueOf(msg.Result))
				} else if msg.Type == "stream_end" {
					inCh.Close()
					return
				} else if msg.Type == "error" {
					inCh.Close()
					return
				}
			case <-endChan:
				inCh.Close()
				return
			}
		}
	}()

	// Invoke the function with the input channel.
	results := fnValue.Call([]reflect.Value{inCh})

	// Extract the output channel and error (if present).
	outCh := results[0]
	var errVal reflect.Value
	if len(results) == 2 {
		errVal = results[1]
	}

	// If the function returned an error upfront, report it.
	if errVal.IsValid() {
		if err, ok := errVal.Interface().(error); ok && err != nil {
			errType := "Error"
			errMsg := err.Error()
			if we, ok := err.(*WireError); ok {
				errType = we.ErrorType
				errMsg = we.Message
			}
			payload, _ := encodeError(msg.ID, errType, errMsg)
			writeFrame(conn, payload)
			return
		}
	}

	// Stream output channel values until it closes, then send stream_end.
	for {
		v, open := outCh.Recv()
		if !open {
			break
		}
		payload, err := encodeStreamChunk(msg.ID, v.Interface())
		if err != nil {
			payload, _ := encodeError(msg.ID, "InternalError", "failed to encode stream chunk")
			writeFrame(conn, payload)
			return
		}
		if err := writeFrame(conn, payload); err != nil {
			return
		}
	}

	payload, _ := encodeStreamEnd(msg.ID)
	writeFrame(conn, payload)
}
