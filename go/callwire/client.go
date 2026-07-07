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
		c.mu.Unlock()

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
		c.mu.Unlock()

		payload, err := readFrame(conn)
		if err != nil {
			c.drainPending()

			// Attempt reconnect with back-off.
			for {
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
	c.mu.Unlock()

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
	c.mu.Unlock()

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
