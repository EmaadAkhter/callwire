package callwire

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// RegistryServer is a simple built-in service registry.
type RegistryServer struct {
	mu       sync.RWMutex
	services map[string][]string
}

// NewRegistryServer creates a new RegistryServer.
func NewRegistryServer() *RegistryServer {
	return &RegistryServer{
		services: make(map[string][]string),
	}
}

// Register adds a service address.
func (r *RegistryServer) Register(serviceName, addr string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	addrs := r.services[serviceName]
	for _, a := range addrs {
		if a == addr {
			return nil
		}
	}
	r.services[serviceName] = append(addrs, addr)
	return nil
}

// Discover returns all addresses for a service.
func (r *RegistryServer) Discover(serviceName string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	addrs, ok := r.services[serviceName]
	if !ok || len(addrs) == 0 {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}
	return addrs, nil
}

// StartRegistryServer starts a callwire server containing the registry endpoints.
func StartRegistryServer(addr string) (net.Listener, error) {
	reg := NewRegistryServer()
	
	Export("callwire.register", reg.Register)
	Export("callwire.discover", reg.Discover)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()
	
	return l, nil
}

// DiscoverPool maintains a dynamic pool of clients resolved from a registry.
type DiscoverPool struct {
	registryAddr string
	serviceName  string
	mu           sync.Mutex
	clients      []*Client
	addrs        []string
}

// NewDiscoverPool creates a new DiscoverPool.
func NewDiscoverPool(registryAddr, serviceName string) (*DiscoverPool, error) {
	pool := &DiscoverPool{
		registryAddr: registryAddr,
		serviceName:  serviceName,
	}
	if err := pool.Refresh(); err != nil {
		return nil, err
	}
	return pool, nil
}

// Refresh queries the registry and updates the active clients.
func (p *DiscoverPool) Refresh() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	regClient, err := Connect(p.registryAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to registry: %w", err)
	}
	defer regClient.Close()

	var addrs []string
	addrs, err = Import[[]string](regClient, context.Background(), "callwire.discover", []interface{}{p.serviceName})
	if err != nil {
		return fmt.Errorf("discover failed: %w", err)
	}

	// Close old clients.
	for _, c := range p.clients {
		c.Close()
	}
	p.clients = nil
	p.addrs = nil

	for _, addr := range addrs {
		c, err := Connect(addr)
		if err == nil {
			p.clients = append(p.clients, c)
			p.addrs = append(p.addrs, addr)
		}
	}

	if len(p.clients) == 0 {
		return fmt.Errorf("no healthy service instances found for %s", p.serviceName)
	}

	return nil
}

// Get returns a client (simple round-robin or first available).
func (p *DiscoverPool) Get() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.clients) == 0 {
		return nil, fmt.Errorf("no clients available in pool")
	}
	return p.clients[0], nil
}

// Close closes all connections in the pool.
func (p *DiscoverPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		c.Close()
	}
	p.clients = nil
}

// ServeRegistry starts the built-in registry server.
func ServeRegistry(addr string) (net.Listener, error) {
	return StartRegistryServer(addr)
}

// RegisterWith registers a service address with a remote registry server.
func RegisterWith(registryAddr, serviceName, addr string) error {
	client, err := Connect(registryAddr)
	if err != nil {
		return err
	}
	defer client.Close()
	_, err = Import[interface{}](client, context.Background(), "callwire.register", []interface{}{serviceName, addr})
	return err
}

// DiscoverRef returns an RPC helper function pointing to a dynamic discovery client pool.
func DiscoverRef[Resp any](pool *DiscoverPool, funcName string) func(args ...interface{}) (Resp, error) {
	return func(args ...interface{}) (Resp, error) {
		var zero Resp
		client, err := pool.Get()
		if err != nil {
			return zero, err
		}
		cleanArgs := args
		if cleanArgs == nil {
			cleanArgs = []interface{}{}
		}
		return Import[Resp](client, context.Background(), funcName, cleanArgs)
	}
}


