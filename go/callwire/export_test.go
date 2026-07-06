package callwire

import (
	"encoding/binary"
	"os"
	"testing"
)

func TestCrossLanguageVerify(t *testing.T) {
	payload, err := EncodeRequest(1, "predict", []interface{}{1.5, 2.0})
	if err != nil {
		t.Fatal(err)
	}
	lengthPrefix := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthPrefix, uint32(len(payload)))
	frame := append(lengthPrefix, payload...)
	if err := os.WriteFile("/tmp/callwire_cross_verify.bin", frame, 0644); err != nil {
		t.Fatal(err)
	}
	t.Log("wrote cross-verify frame to /tmp/callwire_cross_verify.bin")
}
