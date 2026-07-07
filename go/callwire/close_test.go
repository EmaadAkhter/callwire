package callwire

import (
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

