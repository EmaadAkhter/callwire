package callwire

import (
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/vmihailenco/msgpack/v5"
)

type Client struct {
	conn      net.Conn
	writeMu   sync.Mutex
	pending   map[uint64]chan wireMessage
	pendingMu sync.Mutex
	nextID    uint64
}

var (
	defaultClient   *Client
	defaultMu       sync.Mutex
	defaultHost     = envDefault("CALLWIRE_HOST", "localhost")
	defaultPort     = envDefaultInt("CALLWIRE_PORT", 9090)
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

func Connect(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, pending: make(map[uint64]chan wireMessage)}
	go c.readLoop()
	return c, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) readLoop() {
	for {
		payload, err := readFrame(c.conn)
		if err != nil {
			return
		}
		msg, err := decodeMessage(payload)
		if err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[msg.ID]
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
		r, err := Import[interface{}, Resp](c, funcName, cleanArgs)
		if err != nil {
			return zero, err
		}
		return r, nil
	}
}

func Import[Req any, Resp any](c *Client, funcName string, args []interface{}) (Resp, error) {
	var zero Resp
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

	c.writeMu.Lock()
	err = writeFrame(c.conn, payload)
	c.writeMu.Unlock()
	if err != nil {
		return zero, err
	}

	msg := <-respChan

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
}
