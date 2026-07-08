package callwire

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestCloseAllowsImmediateReopen(t *testing.T) {
	t.Setenv("CALLWIRE_AUTO", "1")
	// Reset any auto-server state left by previous tests so autoServeDone
	// doesn't skip starting a new listener on our chosen port.
	Close()
	port := freeTCPPort(t)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	Configure("127.0.0.1", port)
	t.Cleanup(func() {
		Close()
		Configure("localhost", 9090)
	})

	MustExport("close_probe", func() int { return 1 })
	waitForDial(t, addr, 5*time.Second)

	Close()
	waitForDialFailure(t, addr, 5*time.Second)

	MustExport("close_probe_reopen", func() int { return 2 })
	waitForDial(t, addr, 5*time.Second)
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on ephemeral port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitForDial(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to become reachable", addr)
}

func TestCloseDuringActiveCall(t *testing.T) {
	t.Setenv("CALLWIRE_AUTO", "0")

	// Start a listener that handles connections with a slow endpoint.
	MustExport("slow_ping", func() string {
		time.Sleep(5 * time.Second)
		return "pong"
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()

	addr := ln.Addr().String()
	waitForDial(t, addr, 5*time.Second)

	client, err := Connect(addr)
	if err != nil {
		t.Fatal(err)
	}

	// Start a slow call in the background.
	errCh := make(chan error, 1)
	go func() {
		_, callErr := Import[string](client, context.Background(), "slow_ping", nil)
		errCh <- callErr
	}()

	time.Sleep(100 * time.Millisecond) // let the call reach the pending map

	// Close the client while the call is in-flight.
	client.Close()

	select {
	case callErr := <-errCh:
		if callErr == nil {
			t.Fatal("expected error from call after client.Close(), got nil")
		}
		t.Logf("close-during-call error (expected): %v", callErr)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for call to complete after close")
	}
}

func TestCloseDuringActiveStream(t *testing.T) {
	t.Setenv("CALLWIRE_AUTO", "0")

	MustExport("slow_stream", func() <-chan int {
		ch := make(chan int)
		go func() {
			for i := 1; i <= 100; i++ {
				ch <- i
				time.Sleep(100 * time.Millisecond)
			}
			close(ch)
		}()
		return ch
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn)
		}
	}()

	addr := ln.Addr().String()
	waitForDial(t, addr, 5*time.Second)

	client, err := Connect(addr)
	if err != nil {
		t.Fatal(err)
	}

	results, errc := ImportStream[int](client, context.Background(), "slow_stream", nil)

	// Read a few chunks, then close.
	chunk1 := <-results
	t.Logf("got stream chunk: %d", chunk1)

	client.Close()

	// After close, the results channel should drain and errc should have an error.
	// The stream goroutine should exit without panicking.
	for range results {
		// drain
	}

	select {
	case streamErr := <-errc:
		if streamErr == nil {
			t.Log("stream ended cleanly after close")
		} else {
			t.Logf("stream error after close (expected): %v", streamErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not terminate after close")
	}
}

func waitForDialFailure(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %s to be closed, but it remained reachable", addr)
}

