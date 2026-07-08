package callwire

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// Client is a thread-safe callwire client over a single TCP connection.
// If the underlying connection drops it will automatically reconnect with
// exponential back-off and drain any in-flight calls with a clean error.
type Client struct {
	addr   string      // set only for auto-reconnect clients
	tlsCfg *tls.Config // TLS configuration for auto-reconnect (nil for plain TCP)

	mu        sync.Mutex
	conn      net.Conn
	closed    bool
	writeMu   sync.Mutex
	pending   map[uint64]chan wireMessage
	pendingMu sync.Mutex
	nextID    uint64
}

// streamPending tracks channels for in-progress streaming calls.
// The value is a buffered channel that receives wireMessages until stream_end.
type streamEntry struct {
	ch chan wireMessage
}

var (
	defaultClient *Client
	defaultMu     sync.Mutex
	defaultHost   = envDefault("CALLWIRE_HOST", "localhost")
	defaultPort   = envDefaultInt("CALLWIRE_PORT", 9090)
)

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func Configure(host string, port int) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultHost = host
	defaultPort = port
	if defaultClient != nil {
		defaultClient.Close()
		defaultClient = nil
	}
}

func getDefaultClient() (*Client, error) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultClient != nil {
		return defaultClient, nil
	}
	addr := net.JoinHostPort(defaultHost, strconv.Itoa(defaultPort))
	c, err := Connect(addr)
	if err != nil {
		return nil, err
	}
	defaultClient = c
	return defaultClient, nil
}

// Connect dials addr and returns a Client.  The returned client will NOT
// auto-reconnect on disconnect; use ConnectWithReconnect for that.
func Connect(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, pending: make(map[uint64]chan wireMessage)}
	go c.readLoop()
	return c, nil
}

// ConnectWithReconnect dials addr and returns a Client that will transparently
// reconnect (with exponential back-off) whenever the connection drops.
func ConnectWithReconnect(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &Client{
		addr:    addr,
		conn:    conn,
		pending: make(map[uint64]chan wireMessage),
	}
	go c.readLoopWithReconnect()
	return c, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.closed = true
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// drainPending closes all in-flight pending calls with a sentinel error message
// so callers unblock immediately rather than blocking until timeout.
func (c *Client) drainPending() {
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		ch <- wireMessage{ID: id, Type: "error", ErrorType: "TransportError", Message: "connection lost"}
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

func (c *Client) readLoop() {
	for {
		c.mu.Lock()
		conn := c.conn
		closed := c.closed
		c.mu.Unlock()

		if closed || conn == nil {
			return
		}

		payload, err := readFrame(conn)
		if err != nil {
			c.drainPending()
			return
		}
		msg, err := decodeMessage(payload)
		if err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[msg.ID]
		// For streaming messages we leave the channel in pending until stream_end.
		if ok && (msg.Type == "stream_chunk") {
			// Don't delete the pending entry — send chunk and keep waiting.
			c.pendingMu.Unlock()
			ch <- msg
			continue
		}
		if ok {
			delete(c.pending, msg.ID)
		}
		c.pendingMu.Unlock()
		if ok {
			ch <- msg
		}
	}
}

// readLoopWithReconnect is like readLoop but reconnects on error.
func (c *Client) readLoopWithReconnect() {
	backoff := 50 * time.Millisecond
	const maxBackoff = 5 * time.Second

	for {
		c.mu.Lock()
		conn := c.conn
		closed := c.closed
		c.mu.Unlock()

		if closed || conn == nil {
			return
		}

		payload, err := readFrame(conn)
		if err != nil {
			c.drainPending()

			// Attempt reconnect with back-off.
			for {
				c.mu.Lock()
				if c.closed {
					c.mu.Unlock()
					return
				}
				c.mu.Unlock()

				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				var newConn net.Conn
				var err error
				if c.tlsCfg != nil {
					newConn, err = tls.Dial("tcp", c.addr, c.tlsCfg)
				} else {
					newConn, err = net.Dial("tcp", c.addr)
				}
				if err != nil {
					log.Printf("callwire: reconnect to %s failed: %v", c.addr, err)
					continue
				}
				c.mu.Lock()
				if c.closed {
					newConn.Close()
					c.mu.Unlock()
					return
				}
				c.conn = newConn
				c.mu.Unlock()
				backoff = 50 * time.Millisecond // reset on success
				break
			}
			continue
		}
		msg, err := decodeMessage(payload)
		if err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[msg.ID]
		if ok && msg.Type == "stream_chunk" {
			c.pendingMu.Unlock()
			ch <- msg
			continue
		}
		if ok {
			delete(c.pending, msg.ID)
		}
		c.pendingMu.Unlock()
		if ok {
			ch <- msg
		}
	}
}

func Ref[Resp any](funcName string) func(...interface{}) (Resp, error) {
	return func(args ...interface{}) (Resp, error) {
		var zero Resp
		c, err := getDefaultClient()
		if err != nil {
			return zero, err
		}
		return RefWithClient[Resp](c, funcName)(args...)
	}
}

func RefWithClient[Resp any](c *Client, funcName string) func(...interface{}) (Resp, error) {
	return func(args ...interface{}) (Resp, error) {
		var zero Resp
		cleanArgs := args
		if cleanArgs == nil {
			cleanArgs = []interface{}{}
		}
		r, err := Import[Resp](c, context.Background(), funcName, cleanArgs)
		if err != nil {
			return zero, err
		}
		return r, nil
	}
}

func Import[Resp any](c *Client, ctx context.Context, funcName string, args []interface{}) (Resp, error) {
	var zero Resp

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	id := atomic.AddUint64(&c.nextID, 1)
	respChan := make(chan wireMessage, 1)

	c.pendingMu.Lock()
	c.pending[id] = respChan
	c.pendingMu.Unlock()

	cleanArgs := args
	if cleanArgs == nil {
		cleanArgs = []interface{}{}
	}
	payload, err := encodeRequest(id, funcName, cleanArgs)
	if err != nil {
		return zero, err
	}

	c.mu.Lock()
	conn := c.conn
	closed := c.closed
	c.mu.Unlock()

	if closed || conn == nil {
		return zero, &WireError{ErrorType: "ConnectionClosed", Message: "client closed"}
	}

	c.writeMu.Lock()
	err = writeFrame(conn, payload)
	c.writeMu.Unlock()
	if err != nil {
		return zero, err
	}

	select {
	case msg := <-respChan:
		if msg.Type == "error" {
			return zero, &WireError{ErrorType: msg.ErrorType, Message: msg.Message}
		}
		resultBytes, err := msgpack.Marshal(msg.Result)
		if err != nil {
			return zero, err
		}
		var resp Resp
		if err := msgpack.Unmarshal(resultBytes, &resp); err != nil {
			return zero, err
		}
		return resp, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

// ImportStream calls a streaming function on the server and returns two
// channels: one for results and one for a terminal error (nil on clean end).
// The result channel is closed after the last chunk.  Callers should drain
// the results channel and then read from errc.
//
//	results, errc := callwire.ImportStream[int](c, ctx, "count", nil)
//	for v := range results {
//	    fmt.Println(v)
//	}
//	if err := <-errc; err != nil {
//	    log.Fatal(err)
//	}
func ImportStream[Resp any](c *Client, ctx context.Context, funcName string, args []interface{}) (<-chan Resp, <-chan error) {
	results := make(chan Resp, 64)
	errc := make(chan error, 1)

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		_ = cancel // keep alive for the duration of the stream; caller can cancel via their own ctx
	}

	id := atomic.AddUint64(&c.nextID, 1)
	// Use a larger buffer so stream_chunk messages don't stall the read loop.
	respChan := make(chan wireMessage, 256)

	c.pendingMu.Lock()
	c.pending[id] = respChan
	c.pendingMu.Unlock()

	cleanArgs := args
	if cleanArgs == nil {
		cleanArgs = []interface{}{}
	}
	payload, err := encodeRequest(id, funcName, cleanArgs)
	if err != nil {
		close(results)
		errc <- err
		return results, errc
	}

	c.mu.Lock()
	conn := c.conn
	closed := c.closed
	c.mu.Unlock()

	if closed || conn == nil {
		close(results)
		errc <- &WireError{ErrorType: "ConnectionClosed", Message: "client closed"}
		return results, errc
	}

	c.writeMu.Lock()
	err = writeFrame(conn, payload)
	c.writeMu.Unlock()
	if err != nil {
		close(results)
		errc <- err
		return results, errc
	}

	go func() {
		defer close(results)
		defer func() {
			// Ensure pending entry is cleaned up.
			c.pendingMu.Lock()
			delete(c.pending, id)
			c.pendingMu.Unlock()
		}()

		for {
			select {
			case <-ctx.Done():
				errc <- ctx.Err()
				return
			case msg := <-respChan:
				switch msg.Type {
				case "stream_chunk":
					raw, err := msgpack.Marshal(msg.Result)
					if err != nil {
						errc <- err
						return
					}
					var v Resp
					if err := msgpack.Unmarshal(raw, &v); err != nil {
						errc <- err
						return
					}
					select {
					case results <- v:
					case <-ctx.Done():
						errc <- ctx.Err()
						return
					}
				case "stream_end":
					errc <- nil
					return
				case "error":
					errc <- &WireError{ErrorType: msg.ErrorType, Message: msg.Message}
					return
				}
			}
		}
	}()

	return results, errc
}

// ClientStream encapsulates a client-streaming call: client sends multiple chunks, server sends response.
// Use: stream := callwire.ClientStream[int](c, ctx, "process_items", nil)
// stream.Send(item1); stream.Send(item2); resp, err := stream.CloseAndRecv()
type ClientStream[Req, Resp any] struct {
	c       *Client
	ctx     context.Context
	id      uint64
	reqChan chan interface{}
	respCh  chan wireMessage
	done    bool
}

// Send enqueues a chunk to be sent to the server. Blocks if buffer is full.
func (s *ClientStream[Req, Resp]) Send(req Req) error {
	if s.done {
		return &WireError{ErrorType: "StreamClosed", Message: "stream already closed"}
	}
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.reqChan <- req:
		return nil
	}
}

// CloseAndRecv signals end of chunks and waits for the server's single response.
func (s *ClientStream[Req, Resp]) CloseAndRecv() (Resp, error) {
	var zero Resp
	s.done = true
	close(s.reqChan)

	// Send stream_close
	c := s.c
	c.writeMu.Lock()
	payload, _ := encodeStreamClose(s.id)
	err := writeFrame(c.conn, payload)
	c.writeMu.Unlock()
	if err != nil {
		return zero, err
	}

	// Wait for response or error
	select {
	case msg := <-s.respCh:
		if msg.Type == "error" {
			return zero, &WireError{ErrorType: msg.ErrorType, Message: msg.Message}
		}
		if msg.Type == "response" {
			raw, _ := msgpack.Marshal(msg.Result)
			var resp Resp
			if err := msgpack.Unmarshal(raw, &resp); err != nil {
				return zero, err
			}
			return resp, nil
		}
		return zero, &WireError{ErrorType: "ProtocolError", Message: "unexpected message type"}
	case <-s.ctx.Done():
		return zero, s.ctx.Err()
	}
}

// ExportStream initiates a client-streaming RPC call. Caller sends chunks via stream.Send,
// then calls stream.CloseAndRecv to get the final response.
func ExportStream[Req, Resp any](c *Client, ctx context.Context, funcName string) *ClientStream[Req, Resp] {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		_ = cancel
	}

	id := atomic.AddUint64(&c.nextID, 1)
	respCh := make(chan wireMessage, 1)
	reqChan := make(chan interface{}, 64)

	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	payload, _ := encodeRequest(id, funcName, []interface{}{})

	c.mu.Lock()
	conn := c.conn
	closed := c.closed
	c.mu.Unlock()

	if !closed && conn != nil {
		c.writeMu.Lock()
		writeFrame(conn, payload)
		c.writeMu.Unlock()
	}

	stream := &ClientStream[Req, Resp]{
		c:       c,
		ctx:     ctx,
		id:      id,
		reqChan: reqChan,
		respCh:  respCh,
	}

	// Background goroutine sends chunks as they arrive in reqChan.
	go func() {
		for req := range reqChan {
			raw, _ := msgpack.Marshal(req)
			c.writeMu.Lock()
			chunk, _ := encodeStreamChunk(id, raw)
			writeFrame(c.conn, chunk)
			c.writeMu.Unlock()
		}
	}()

	return stream
}

// BidiStream encapsulates a bidirectional-streaming call: both client and server send chunks concurrently.
type BidiStream[Req, Resp any] struct {
	c           *Client
	ctx         context.Context
	id          uint64
	sendChan    chan interface{}
	recvChan    chan wireMessage
	clientDone  bool
	serverDone  bool
	serverEnd   chan struct{}
	mu          sync.Mutex
}

// Send enqueues a chunk to be sent to the server.
func (s *BidiStream[Req, Resp]) Send(req Req) error {
	s.mu.Lock()
	if s.clientDone {
		s.mu.Unlock()
		return &WireError{ErrorType: "StreamClosed", Message: "client side closed"}
	}
	s.mu.Unlock()

	raw, _ := msgpack.Marshal(req)
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.sendChan <- raw:
		return nil
	}
}

// Recv waits for the next chunk from the server. Returns nil for Resp when server is done.
func (s *BidiStream[Req, Resp]) Recv() (Resp, error) {
	var zero Resp
	select {
	case <-s.ctx.Done():
		return zero, s.ctx.Err()
	case msg := <-s.recvChan:
		switch msg.Type {
		case "stream_chunk":
			raw, _ := msgpack.Marshal(msg.Result)
			var resp Resp
			if err := msgpack.Unmarshal(raw, &resp); err != nil {
				return zero, err
			}
			return resp, nil
		case "stream_end":
			s.mu.Lock()
			s.serverDone = true
			s.mu.Unlock()
			close(s.serverEnd)
			return zero, nil // caller checks if Recv returned nil to know stream ended
		case "error":
			return zero, &WireError{ErrorType: msg.ErrorType, Message: msg.Message}
		}
	}
	return zero, &WireError{ErrorType: "ProtocolError", Message: "unexpected message type"}
}

// CloseSend signals the client side is done sending. Must still call Recv in a loop until it returns nil.
func (s *BidiStream[Req, Resp]) CloseSend() error {
	s.mu.Lock()
	if s.clientDone {
		s.mu.Unlock()
		return nil
	}
	s.clientDone = true
	s.mu.Unlock()

	close(s.sendChan)

	c := s.c
	c.writeMu.Lock()
	payload, _ := encodeStreamEnd(s.id)
	err := writeFrame(c.conn, payload)
	c.writeMu.Unlock()
	return err
}

// ImportBidi initiates a bidirectional-streaming RPC call.
func ImportBidi[Req, Resp any](c *Client, ctx context.Context, funcName string) *BidiStream[Req, Resp] {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		_ = cancel
	}

	id := atomic.AddUint64(&c.nextID, 1)
	recvChan := make(chan wireMessage, 256)
	sendChan := make(chan interface{}, 64)

	c.pendingMu.Lock()
	c.pending[id] = recvChan
	c.pendingMu.Unlock()

	payload, _ := encodeBidiRequest(id, funcName, []interface{}{})

	c.mu.Lock()
	conn := c.conn
	closed := c.closed
	c.mu.Unlock()

	if !closed && conn != nil {
		c.writeMu.Lock()
		writeFrame(conn, payload)
		c.writeMu.Unlock()
	}

	stream := &BidiStream[Req, Resp]{
		c:         c,
		ctx:       ctx,
		id:        id,
		sendChan:  sendChan,
		recvChan:  recvChan,
		serverEnd: make(chan struct{}),
	}

	// Background goroutine sends chunks as they arrive.
	go func() {
		for req := range sendChan {
			c.writeMu.Lock()
			chunk, _ := encodeStreamChunk(id, req)
			writeFrame(c.conn, chunk)
			c.writeMu.Unlock()
		}
	}()

	return stream
}
