package callwire

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	payload, err := encodeRequest(1, "predict", []interface{}{1.5, 2.0})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := decodeMessage(payload)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != 1 {
		t.Fatalf("expected id=1, got %d", msg.ID)
	}
	if msg.Type != "request" {
		t.Fatalf("expected type=request, got %s", msg.Type)
	}
	if msg.Func != "predict" {
		t.Fatalf("expected func=predict, got %s", msg.Func)
	}
	if len(msg.Args) != 2 || msg.Args[0] != 1.5 || msg.Args[1] != 2.0 {
		t.Fatalf("unexpected args: %v", msg.Args)
	}
	t.Log("encodeRequest / decodeMessage roundtrip OK")
}

func TestFramingRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	conn := &mockConn{&buf}
	payload := []byte("hello world")
	if err := writeFrame(conn, payload); err != nil {
		t.Fatal(err)
	}

	length := binary.BigEndian.Uint32(buf.Bytes()[:4])
	if length != uint32(len(payload)) {
		t.Fatalf("expected length %d, got %d", len(payload), length)
	}
	if !bytes.Equal(buf.Bytes()[4:], payload) {
		t.Fatal("payload mismatch")
	}
	t.Log("writeFrame produces correct framing")
}

type mockConn struct {
	buf *bytes.Buffer
}

func (m *mockConn) Read(b []byte) (int, error)       { return m.buf.Read(b) }
func (m *mockConn) Write(b []byte) (int, error)      { return m.buf.Write(b) }
func (m *mockConn) Close() error                      { return nil }
func (m *mockConn) LocalAddr() net.Addr               { return nil }
func (m *mockConn) RemoteAddr() net.Addr              { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }
