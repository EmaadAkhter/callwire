package callwire

import (
	"bytes"
	"testing"
)

func TestServerDispatch(t *testing.T) {
	Export("add", func(a, b int) int { return a + b })
	Export("err", func() error { return &WireError{ErrorType: "ValueError", Message: "boom"} })
	Export("multi", func(a int) (int, error) {
		if a < 0 {
			return 0, &WireError{ErrorType: "ValueError", Message: "negative"}
		}
		return a * 2, nil
	})

	tests := []struct {
		name     string
		msg      wireMessage
		wantType string
		wantID   uint64
		check    func(t *testing.T, msg wireMessage)
	}{
		{
			name:     "success",
			msg:      wireMessage{ID: 1, Type: "request", Func: "add", Args: []interface{}{3, 4}},
			wantType: "response",
			wantID:   1,
			check: func(t *testing.T, msg wireMessage) {
				n := toInt64(msg.Result)
				if n != 7 {
					t.Fatalf("expected 7, got %v", msg.Result)
				}
			},
		},
		{
			name:     "not-found",
			msg:      wireMessage{ID: 2, Type: "request", Func: "nope"},
			wantType: "error",
			wantID:   2,
			check: func(t *testing.T, msg wireMessage) {
				if msg.ErrorType != "NotFoundError" {
					t.Fatalf("expected NotFoundError, got %s", msg.ErrorType)
				}
			},
		},
		{
			name:     "error-return",
			msg:      wireMessage{ID: 3, Type: "request", Func: "err"},
			wantType: "error",
			wantID:   3,
			check: func(t *testing.T, msg wireMessage) {
				if msg.Message != "boom" {
					t.Fatalf("expected 'boom', got %s", msg.Message)
				}
			},
		},
		{
			name:     "multi-success",
			msg:      wireMessage{ID: 4, Type: "request", Func: "multi", Args: []interface{}{5}},
			wantType: "response",
			wantID:   4,
			check: func(t *testing.T, msg wireMessage) {
				n := toInt64(msg.Result)
				if n != 10 {
					t.Fatalf("expected 10, got %v", msg.Result)
				}
			},
		},
		{
			name:     "multi-error",
			msg:      wireMessage{ID: 5, Type: "request", Func: "multi", Args: []interface{}{-1}},
			wantType: "error",
			wantID:   5,
			check: func(t *testing.T, msg wireMessage) {
				if msg.Message != "negative" {
					t.Fatalf("expected 'negative', got %s", msg.Message)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			conn := &mockConn{&buf}
			dispatch(conn, tt.msg)

			payload, err := readFrame(conn)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := decodeMessage(payload)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Type != tt.wantType {
				t.Fatalf("expected type %s, got %s", tt.wantType, resp.Type)
			}
			if resp.ID != tt.wantID {
				t.Fatalf("expected id %d, got %d", tt.wantID, resp.ID)
			}
			tt.check(t, resp)
		})
	}
}

func toInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case uint:
		return int64(x)
	case uint8:
		return int64(x)
	case uint16:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		return int64(x)
	case float64:
		return int64(x)
	default:
		return 0
	}
}
