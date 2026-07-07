package callwire

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTLSOneWayAndMTLS(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	caFile := filepath.Join(tmpDir, "ca.pem")

	// Generate CA and Leaf cert
	if err := GenSelfSignedCert(certFile, keyFile, caFile); err != nil {
		t.Fatalf("failed to generate certs: %v", err)
	}

	MustExport("tlsAdd", func(a, b int) int { return a + b })

	// ── Test 1: One-way TLS (Client validates Server) ──
	go func() {
		// Server configuration with Cert + Key only (no CAFile = no client cert validation)
		err := ServeWithTLS("127.0.0.1:19900", TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		})
		if err != nil {
			t.Logf("Server exited: %v", err)
		}
	}()
	waitForPort(t, "19900", 5*time.Second)

	// Client validates Server using CAFile
	client, err := ConnectWithTLS("127.0.0.1:19900", TLSConfig{
		CAFile: caFile,
	})
	if err != nil {
		t.Fatalf("one-way client connect: %v", err)
	}

	res, err := Import[int](client, context.Background(), "tlsAdd", []interface{}{10, 20})
	if err != nil {
		t.Fatalf("one-way call: %v", err)
	}
	if res != 30 {
		t.Fatalf("expected 30, got %d", res)
	}
	client.Close()
	t.Log("SUCCESS: One-way TLS verified")

	// ── Test 2: Mutual TLS (mTLS) ──
	go func() {
		// Server configuration with CAFile enables client certificate verification
		err := ServeWithTLS("127.0.0.1:19901", TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
			CAFile:   caFile,
		})
		if err != nil {
			t.Logf("Server exited: %v", err)
		}
	}()
	waitForPort(t, "19901", 5*time.Second)

	// Client connecting without client certs should fail either on dial or on call.
	cNoCert, err := ConnectWithTLS("127.0.0.1:19901", TLSConfig{
		CAFile: caFile,
	})
	if err == nil {
		defer cNoCert.Close()
		_, callErr := Import[int](cNoCert, context.Background(), "tlsAdd", []interface{}{10, 20})
		if callErr == nil {
			t.Fatal("expected client connection or call without certs to fail under mTLS")
		}
	}

	// Client connecting with client certs should succeed
	mClient, err := ConnectWithTLS("127.0.0.1:19901", TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
	})
	if err != nil {
		t.Fatalf("mTLS client connect: %v", err)
	}
	defer mClient.Close()

	res, err = Import[int](mClient, context.Background(), "tlsAdd", []interface{}{50, 50})
	if err != nil {
		t.Fatalf("mTLS call: %v", err)
	}
	if res != 100 {
		t.Fatalf("expected 100, got %d", res)
	}
	t.Log("SUCCESS: mTLS verified")
}

func TestTLSReconnect(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	caFile := filepath.Join(tmpDir, "ca.pem")

	if err := GenSelfSignedCert(certFile, keyFile, caFile); err != nil {
		t.Fatalf("failed to generate certs: %v", err)
	}

	MustExport("tlsReconnectAdd", func(a, b int) int { return a + b })

	// Let's implement helper listener directly to make the server stoppable.
	tlsCfg, _ := TLSConfig{CertFile: certFile, KeyFile: keyFile}.ToGoTLSConfig(true)
	l, err := tls.Listen("tcp", "127.0.0.1:19902", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}

	var activeConns []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			activeConns = append(activeConns, conn)
			mu.Unlock()
			go handleConnection(conn)
		}
	}()

	// Connect auto-reconnect client
	c, err := ConnectWithReconnectTLS("127.0.0.1:19902", TLSConfig{
		CAFile: caFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	res, err := Import[int](c, context.Background(), "tlsReconnectAdd", []interface{}{3, 4})
	if err != nil || res != 7 {
		t.Fatalf("initial call failed: %v", err)
	}

	// Close server 1 listener and actively accepted connections to trigger client disconnect.
	l.Close()
	mu.Lock()
	for _, conn := range activeConns {
		conn.Close()
	}
	mu.Unlock()
	time.Sleep(100 * time.Millisecond)

	// Calls should now fail immediately (drained)
	_, err = Import[int](c, context.Background(), "tlsReconnectAdd", []interface{}{3, 4})
	if err == nil {
		t.Fatal("expected call to fail after server closed")
	}

	// Start server 2 on same port
	l2, err := tls.Listen("tcp", "127.0.0.1:19902", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()

	go func() {
		for {
			conn, err := l2.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			activeConns = append(activeConns, conn)
			mu.Unlock()
			go handleConnection(conn)
		}
	}()

	// Wait for client to reconnect
	time.Sleep(800 * time.Millisecond)

	// Call should now succeed
	var success bool
	var lastErr error
	for i := 0; i < 10; i++ {
		res, err = Import[int](c, context.Background(), "tlsReconnectAdd", []interface{}{10, 20})
		if err == nil && res == 30 {
			success = true
			break
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	if !success {
		t.Fatalf("client failed to reconnect and call server 2: %v", lastErr)
	}
	t.Log("SUCCESS: TLS Reconnection verified")
}

func main() {
	// For manual testing if needed
	fmt.Println("Run with go test")
	os.Exit(1)
}
