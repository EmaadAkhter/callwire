package callwire

import (
	"encoding/binary"
	"io"
	"net"
)

func writeFrame(conn net.Conn, payload []byte) error {
	lengthPrefix := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthPrefix, uint32(len(payload)))
	_, err := conn.Write(append(lengthPrefix, payload...))
	return err
}

func readFrame(conn net.Conn) ([]byte, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(conn, lengthBytes); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBytes)
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
